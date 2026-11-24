package jobs

// Editing a schedule in place (#516): its time, prompt and label change; its
// ID, fire record (last fire, missed count, the unread fire mail) and expiry
// stay, so a fix is not a remove and a new schedule.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// errNotEditable: the schedule has ended or expired.
var errNotEditable = errors.New("only an active or paused schedule is edited")

// badEdit is an edit the schedule refuses (a time in the past, a cron that
// never fires): 400.
type badEdit struct{ error }

// editAPI serves PUT ID for a View that may edit the schedule: 400 for a bad
// body or time, 404 for one not in view, 409 for one that ended.
func (s *Service) editAPI(w http.ResponseWriter, r *http.Request, v View, id int64) {
	var q wire.ScheduleEdit
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&q) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := q.Check(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sc, err := s.EditSchedule(r.Context(), v, id, q)
	var bad badEdit
	switch {
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), http.StatusBadRequest)
	case errors.Is(err, errNotFound):
		http.Error(w, "unknown schedule", http.StatusNotFound)
	case errors.Is(err, errNotEditable):
		http.Error(w, err.Error(), http.StatusConflict)
	case err != nil:
		http.Error(w, "could not edit schedule", http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, sc)
	}
}

// EditSchedule changes an active or paused schedule in view by q (checked
// already). A new time is read as create reads it, against the schedule's
// expiry, and the next fire moves to its first time after now. A paused
// schedule stays paused. The owner is told when anyone else edits it.
func (s *Service) EditSchedule(ctx context.Context, v View, id int64, q wire.ScheduleEdit) (wire.Schedule, error) {
	now := s.now().UTC()
	var owner, label string
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.scheduleInView(tx, v, id)
		if err != nil {
			return err
		}
		owner, label = sc.Role, sc.Label
		if sc.State != wire.ScheduleActive && sc.State != wire.SchedulePaused {
			return errNotEditable
		}
		if q.Kind != "" {
			tz := q.TZ
			if tz == "" {
				tz = sc.TZ
			}
			next, tz, err := firstFire(q.Kind, q.Cron, q.At, tz, now, sc.ExpiresAt)
			if err != nil {
				return badEdit{err}
			}
			expr := q.Cron
			if q.Kind == wire.ScheduleOnce {
				expr = ""
			}
			if _, err = tx.Exec(`UPDATE schedules SET kind=?, cron=?, tz=?, next_fire_at=? WHERE id=?`,
				q.Kind, expr, tz, stamp(next), id); err != nil {
				return err
			}
		}
		if q.Prompt != nil {
			if _, err = tx.Exec(`UPDATE schedules SET prompt=? WHERE id=?`, *q.Prompt, id); err != nil {
				return err
			}
		}
		if q.Label != nil {
			l := *q.Label
			if l == "" {
				l = fmt.Sprintf("예약 #%d", id)
			}
			if _, err = tx.Exec(`UPDATE schedules SET label=? WHERE id=?`, l, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return wire.Schedule{}, err
	}
	if s.notify != nil && (v.Admin != "" || v.Role != owner) {
		by := v.actor()
		text := fmt.Sprintf("예약 #%d %s 을(를) %s 이(가) 고쳤습니다.", id, label, by.Label())
		idem := "schedule-edit:" + strconv.FormatInt(id, 10) + ":" + strconv.FormatInt(now.UnixNano(), 10)
		if err := s.notify(ctx, owner, "[예약 고침] "+label, text, idem); err != nil {
			log.Printf("schedule edit %d: notice to %s: %v", id, owner, err)
		}
	}
	return s.GetSchedule(id)
}
