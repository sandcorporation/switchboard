package jobs

// Pausing a schedule (#404). A paused schedule does not fire: the sweep
// skips it, the fire times that pass are dropped (never counted as missed)
// and its expiry clock stands still — resuming moves expires_at on by the
// time it was paused. A once schedule whose time went by while paused stays
// paused and Passed until a resume says what to do with that fire: deliver
// it now (PassedFire) or drop it, which ends the schedule (PassedDrop).
// Every pause and resume is kept in schedule_pauses; one by anyone but the
// owner is told to the owner by Notify.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/sandcorporation/switchboard/internal/cron"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Notify tells role something that needs no answer: the server stores a
// normal system mail (no open directive) once per idem key and wakes the
// role's waiters.
type Notify func(ctx context.Context, role, subject, body, idem string) error

// WithNotify sets the notice a pause or resume by anyone but the owner
// sends the owner.
func (s *Service) WithNotify(n Notify) *Service {
	s.notify = n
	return s
}

var (
	// errPassedChoice: a once schedule's time went by while it was paused
	// and the resume did not say fire or drop.
	errPassedChoice = errors.New("its time passed while it was paused: resume with passed=fire (send it now) or passed=drop")
	// errNotPausable: the schedule has ended or expired.
	errNotPausable = errors.New("only an active or paused schedule pauses and resumes")
)

// pauseAPI serves POST ID/pause, ID/resume and ID/run (runAPI) for a View (the role and admin
// paths): 404 for a schedule not in view, 409 for one that ended or a
// passed once fire without a choice.
func (s *Service) pauseAPI(w http.ResponseWriter, r *http.Request, v View, id int64, action string) {
	if action == "run" && r.Method == http.MethodPost {
		s.runAPI(w, r, v, id)
		return
	}
	if r.Method != http.MethodPost || action != "pause" && action != "resume" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var sc wire.Schedule
	var err error
	body := http.MaxBytesReader(w, r.Body, 8<<10)
	if action == "pause" {
		var q wire.SchedulePause
		if derr := json.NewDecoder(body).Decode(&q); derr != nil && !errors.Is(derr, io.EOF) || q.Check() != nil {
			http.Error(w, "invalid body: {\"reason\": TEXT} (at most 500 bytes)", http.StatusBadRequest)
			return
		}
		sc, err = s.PauseSchedule(r.Context(), v, id, q)
	} else {
		var q wire.ScheduleResume
		if derr := json.NewDecoder(body).Decode(&q); derr != nil && !errors.Is(derr, io.EOF) || q.Check() != nil {
			http.Error(w, "invalid body: {\"passed\": \"fire\" | \"drop\"}", http.StatusBadRequest)
			return
		}
		sc, err = s.ResumeSchedule(r.Context(), v, id, q)
	}
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "unknown schedule", http.StatusNotFound)
	case errors.Is(err, errPassedChoice), errors.Is(err, errNotPausable):
		http.Error(w, err.Error(), http.StatusConflict)
	case err != nil:
		http.Error(w, "could not record the change", http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, sc)
	}
}

// actor is v as a schedule's pauser.
func (v View) actor() wire.ScheduleActor {
	if v.Admin != "" {
		return wire.ScheduleActor{Name: v.Admin, Admin: true}
	}
	return wire.ScheduleActor{Name: v.Role}
}

// PauseSchedule pauses an active schedule in view with an optional reason.
// Pausing a paused one changes nothing.
func (s *Service) PauseSchedule(ctx context.Context, v View, id int64, q wire.SchedulePause) (wire.Schedule, error) {
	now := s.now().UTC()
	by := v.actor()
	var owner, label string
	var hist int64
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		hist = 0
		sc, err := s.scheduleInView(tx, v, id)
		if err != nil {
			return err
		}
		owner, label = sc.Role, sc.Label
		switch sc.State {
		case wire.SchedulePaused:
			return nil
		case wire.ScheduleActive:
		default:
			return errNotPausable
		}
		if _, err = tx.Exec(`UPDATE schedules SET state=?, paused_at=?, paused_by=?, paused_by_admin=?, pause_reason=? WHERE id=? AND state=?`,
			wire.SchedulePaused, stamp(now), by.Name, by.Admin, q.Reason, id, wire.ScheduleActive); err != nil {
			return err
		}
		hist, err = recordPause(tx, id, "pause", by, q.Reason, "", now)
		return err
	})
	if err != nil {
		return wire.Schedule{}, err
	}
	if hist > 0 {
		text := fmt.Sprintf("예약 #%d %s 을(를) %s 이(가) 멈췄습니다.", id, label, by.Label())
		if q.Reason != "" {
			text += "\n까닭: " + q.Reason
		}
		s.tell(ctx, v, owner, "[예약 멈춤] "+label, text, hist)
	}
	return s.GetSchedule(id)
}

// ResumeSchedule resumes a paused schedule in view: the expiry moves on by
// the paused time, a cron schedule fires next at its first time after now,
// and a once schedule whose time passed needs q.Passed. Resuming an active
// one changes nothing.
func (s *Service) ResumeSchedule(ctx context.Context, v View, id int64, q wire.ScheduleResume) (wire.Schedule, error) {
	now := s.now().UTC()
	by := v.actor()
	var owner, label, choice string
	var hist int64
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		hist, choice = 0, ""
		sc, err := s.scheduleInView(tx, v, id)
		if err != nil {
			return err
		}
		owner, label = sc.Role, sc.Label
		switch sc.State {
		case wire.ScheduleActive:
			return nil
		case wire.SchedulePaused:
		default:
			return errNotPausable
		}
		expires := sc.ExpiresAt
		if sc.PausedAt != nil && now.After(*sc.PausedAt) {
			expires = expires.Add(now.Sub(*sc.PausedAt))
		}
		state, next := wire.ScheduleActive, sc.NextFireAt
		switch {
		case sc.Kind == wire.ScheduleCron:
			// The times that passed while paused are dropped.
			if next, err = cronNext(sc, now); err != nil {
				return err
			}
		case next != nil && !next.After(now):
			switch q.Passed {
			case wire.PassedFire:
				next = &now // the next sweep delivers it
			case wire.PassedDrop:
				state, next = wire.ScheduleEnded, nil
			default:
				return errPassedChoice
			}
			choice = q.Passed
		}
		if _, err = tx.Exec(`UPDATE schedules SET state=?, expires_at=?, next_fire_at=?, paused_at=NULL, paused_by='', paused_by_admin=0, pause_reason='' WHERE id=? AND state=?`,
			state, stamp(expires), stampNull(next), id, wire.SchedulePaused); err != nil {
			return err
		}
		hist, err = recordPause(tx, id, "resume", by, "", choice, now)
		return err
	})
	if err != nil {
		return wire.Schedule{}, err
	}
	if hist > 0 {
		text := fmt.Sprintf("예약 #%d %s 을(를) %s 이(가) 다시 켰습니다.", id, label, by.Label())
		switch choice {
		case wire.PassedFire:
			text += "\n멈춘 동안 지난 시각의 프롬프트를 지금 보냅니다."
		case wire.PassedDrop:
			text += "\n멈춘 동안 지난 시각은 버렸고 예약은 끝났습니다."
		}
		s.tell(ctx, v, owner, "[예약 켬] "+label, text, hist)
	}
	return s.GetSchedule(id)
}

// scheduleInView reads schedule id inside tx; errNotFound when it is
// missing or not in v.
func (s *Service) scheduleInView(tx *sql.Tx, v View, id int64) (wire.Schedule, error) {
	sc, err := scanSchedule(tx.QueryRow(`SELECT `+scheduleColumns+` FROM schedules WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) || err == nil && !v.Sees(sc.Role) {
		return wire.Schedule{}, errNotFound
	}
	return sc.Schedule, err
}

// cronNext is a cron schedule's first fire after now (nil: never again).
func cronNext(sc wire.Schedule, now time.Time) (*time.Time, error) {
	spec, err := cron.Parse(sc.Cron)
	if err != nil {
		return nil, err
	}
	loc, err := cron.Zone(sc.TZ)
	if err != nil {
		return nil, err
	}
	next := spec.Next(now, loc)
	if next.IsZero() {
		return nil, nil
	}
	next = next.UTC()
	return &next, nil
}

// recordPause appends a pause or resume to schedule_pauses.
func recordPause(tx *sql.Tx, id int64, action string, by wire.ScheduleActor, reason, passed string, now time.Time) (int64, error) {
	res, err := tx.Exec(`INSERT INTO schedule_pauses(schedule_id,action,actor,admin,reason,passed,at) VALUES(?,?,?,?,?,?,?)`,
		id, action, by.Name, by.Admin, reason, passed, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// tell sends the owner a pause or resume made by anyone else. The change is
// stored already; a notice that fails is logged, not retried.
func (s *Service) tell(ctx context.Context, v View, owner, subject, body string, hist int64) {
	if s.notify == nil || v.Admin == "" && v.Role == owner {
		return
	}
	if err := s.notify(ctx, owner, subject, body, "schedule-pause:"+strconv.FormatInt(hist, 10)); err != nil {
		log.Printf("schedule pause %d: notice to %s: %v", hist, owner, err)
	}
}
