package jobs

// Running a schedule now (#588): one mail of the prompt to the owner, apart
// from the fire times. The next fire, the expiry, the missed count and
// last_fire_at stay; last_run_at and a run line in schedule_pauses record
// it. A paused schedule runs too; an ended or expired one does not. A run
// again within wire.ScheduleRunWindow of the last is that run (one mail).
// A run by anyone but the owner is told to the owner, like a pause.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

var (
	// errNotRunnable: the schedule has ended or expired.
	errNotRunnable = errors.New("only an active or paused schedule runs now")
	// errNoMailbox: the owner role has no mailbox (retired).
	errNoMailbox = errors.New("the schedule's role has no mailbox")
)

// runAPI serves POST ID/run for a View: 404 for a schedule not in view, 409
// for one that ended or expired.
func (s *Service) runAPI(w http.ResponseWriter, r *http.Request, v View, id int64) {
	out, err := s.RunSchedule(r.Context(), v, id)
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "unknown schedule", http.StatusNotFound)
	case errors.Is(err, errNotRunnable), errors.Is(err, errNoMailbox):
		http.Error(w, err.Error(), http.StatusConflict)
	case err != nil:
		http.Error(w, "could not send", http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, out)
	}
}

// runMail is the mail of a run now: fireMail's subject marked 지금 실행.
func runMail(sc wire.Schedule) (subject, body string) {
	return "[주기] " + sc.Label + " · 지금 실행", sc.Prompt
}

// RunSchedule mails the prompt of schedule id in v to its owner now.
func (s *Service) RunSchedule(ctx context.Context, v View, id int64) (wire.ScheduleRun, error) {
	now := s.now().UTC()
	by := v.actor()
	var out wire.ScheduleRun
	var owner, label, box string
	var hist int64
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		out, box, hist = wire.ScheduleRun{}, "", 0
		sc, err := s.scheduleInView(tx, v, id)
		if err != nil {
			return err
		}
		owner, label = sc.Role, sc.Label
		if sc.State != wire.ScheduleActive && sc.State != wire.SchedulePaused {
			return errNotRunnable
		}
		mailbox, ok := identity.Mailbox(tx, sc.Role)
		if !ok {
			return errNoMailbox
		}
		at := now
		if sc.LastRunAt != nil && now.Sub(*sc.LastRunAt) < wire.ScheduleRunWindow && !now.Before(*sc.LastRunAt) {
			at = *sc.LastRunAt // the same run: its key, its mail
		}
		key := fmt.Sprintf("schedule-run:%d:%s", id, stamp(at.UTC()))
		subject, body := runMail(sc)
		stored, err := mail.InsertSystemMailOnce(tx, mailbox, subject, body, "", "normal", key, now)
		if err != nil {
			return err
		}
		if err = tx.QueryRow(`SELECT id FROM messages WHERE sender=? AND idem=?`, identity.SystemSender, key).Scan(&out.MailID); err != nil {
			return err
		}
		if !stored {
			out.Repeated = true
			return nil
		}
		box = mailbox
		if _, err = tx.Exec(`UPDATE schedules SET last_run_at=? WHERE id=?`, stamp(now), id); err != nil {
			return err
		}
		hist, err = recordPause(tx, id, "run", by, "", "", now)
		return err
	})
	if err != nil {
		return wire.ScheduleRun{}, err
	}
	if box != "" && s.wake != nil {
		s.wake(box)
	}
	if hist > 0 {
		s.tell(ctx, v, owner, "[예약 지금 실행] "+label,
			fmt.Sprintf("예약 #%d %s 을(를) %s 이(가) 지금 실행했습니다(메일 #%s). 다음 회차는 그대로입니다.", id, label, by.Label(), strconv.FormatInt(out.MailID, 10)), hist)
	}
	if out.Schedule, err = s.GetSchedule(id); err != nil {
		return wire.ScheduleRun{}, err
	}
	return out, nil
}
