package presence

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/store"
)

// Directive reminders (#214, #586).
//
// A directive that its recipient has read and not finished (an open
// directive, see openDirectives, with status read and a non-boss recipient;
// replies leave it open since #269) gets a reminder from the system sender
// each time it has been quiet for the mail kind's reminder time (package
// remind): counted from the latest of the directive, its newest reply, the
// last reminder and since, when the time was set, so switching on or
// changing it sends no burst. It repeats at that fixed gap until the
// directive ends (reply --done, complete, close). 0, the start value, is off.
// An open directive whose recipient sits idle is the idle check's (idle.go).
//
// Every reminder carries an idempotency key (remind:<id>:<n>) on the
// messages table's UNIQUE(sender,idem), so repeated or concurrent sweeps and
// restarts send each exactly once. The sent mail is also the schedule's
// state: the next one is due a gap after the last one stored.

const (
	// defaultRemindIdleMax is the idle notices per directive.
	defaultRemindIdleMax = 1
)

// RemindConfig holds the idle check's knobs, code constants unless a test
// sets them.
type RemindConfig struct {
	// Idle is the idle threshold (idle.go): 0 means defaultRemindIdle,
	// negative means off.
	Idle time.Duration
	// IdleMax is the idle notices per directive; 0 means
	// defaultRemindIdleMax.
	IdleMax int
}

func (c RemindConfig) idleMax() int {
	if c.IdleMax > 0 {
		return c.IdleMax
	}
	return defaultRemindIdleMax
}

// idle is the idle threshold; 0 or less is off.
func (c RemindConfig) idle() time.Duration {
	if c.Idle == 0 {
		return defaultRemindIdle
	}
	return max(0, c.Idle)
}

// RemindStatus is the mail reminder setting in force, as the start-up line
// and GET /api/admin/settings report it (#223, #586). Since is when the time
// was set.
type RemindStatus struct {
	On           bool       `json:"on"`
	After        string     `json:"after"`
	AfterSeconds int64      `json:"after_seconds"`
	Since        *time.Time `json:"since"`
	Idle         string     `json:"idle"`         // the idle threshold, "off" when 0
	IdleSeconds  int64      `json:"idle_seconds"` // 0 when off
	IdleMax      int        `json:"idle_max"`
}

// RemindStatus reads the mail reminder setting.
func (s *Service) RemindStatus(ctx context.Context) (RemindStatus, error) {
	c := s.cfg.Remind
	st := RemindStatus{Idle: "off", IdleMax: c.idleMax()}
	if d := c.idle(); d > 0 {
		st.Idle, st.IdleSeconds = shortDuration(d), int64(d/time.Second)
	}
	set, err := remind.Load(ctx, s.db, remind.Mail, s.now().UTC())
	if err != nil {
		return st, err
	}
	st.On, st.AfterSeconds, st.Since = set.On(), int64(set.After/time.Second), &set.Since
	if set.On() {
		st.After = shortDuration(set.After)
	}
	return st, nil
}

// StartLine is the one start-up log line for directive reminders:
// "directive reminders: on (after 2h, since <RFC 3339 time>, idle 1h, idle
// max 1)" or "directive reminders: off (idle 1h, idle max 1)"; "idle off"
// when the idle check is off.
func (st RemindStatus) StartLine() string {
	idle := "idle off"
	if st.Idle != "off" {
		idle = fmt.Sprintf("idle %s, idle max %d", st.Idle, st.IdleMax)
	}
	if !st.On {
		return fmt.Sprintf("directive reminders: off (%s)", idle)
	}
	return fmt.Sprintf("directive reminders: on (after %s, since %s, %s)", st.After, st.Since.UTC().Format(time.RFC3339), idle)
}

// shortDuration drops the zero minutes and seconds of a duration: 2h, 1h30m,
// 45s.
func shortDuration(d time.Duration) string { return mail.ShortDuration(d) }

// RemindSubject and RemindBody are a reminder's text.
func RemindSubject(id int64, subject string) string {
	return fmt.Sprintf("[다시 알림] #%d %s", id, subject)
}

func RemindBody(id int64) string {
	return fmt.Sprintf("#%d 가 끝났으면 reply --done 으로 보고하고, 아직이면 reply 로 진행을 알려라", id)
}

func remindKey(id int64, n int) string {
	return remindPrefix(id) + strconv.Itoa(n)
}

func remindPrefix(id int64) string { return "remind:" + strconv.FormatInt(id, 10) + ":" }

// idleAfterKey is the key of the n-th idle notice: idle-after:<id> for the
// first (as #268 stored it), idle-after:<id>:<n> after that.
func idleAfterKey(id int64, n int) string {
	k := "idle-after:" + strconv.FormatInt(id, 10)
	if n > 1 {
		k += ":" + strconv.Itoa(n)
	}
	return k
}

// SweepReminders sends the due directive reminders and idle notices and
// returns how many it sent. base maps a mailbox name to its base role. With
// the mail reminder time 0 it sends nothing.
func (s *Service) SweepReminders(ctx context.Context, base func(string) string) (int, error) {
	now := s.now().UTC()
	set, err := remind.Load(ctx, s.db, remind.Mail, now)
	if err != nil || !set.On() {
		return 0, err
	}
	isBoss := s.ids.IsBossName(base)
	due, err := s.dueDirectives(ctx, now, set, base, isBoss, false)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, d := range due {
		var stored bool
		err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
			var e error
			stored, e = mail.InsertSystemMailOnce(tx, d.Recipient, RemindSubject(d.ID, d.Subject), RemindBody(d.ID), d.TaskID, "normal", remindKey(d.ID, d.n+1), now)
			return e
		})
		if err != nil {
			return sent, err
		}
		if stored {
			sent++
			s.wake(d.Recipient)
		}
	}
	n, err := s.sweepIdle(ctx, now, set.Since, base, isBoss)
	return sent + n, err
}

// dueDirective is an open directive due a reminder and how many it had.
type dueDirective struct {
	progressMailRow
	n int
}

// dueDirectives lists the open directives that are read, to a non-boss
// role, and quiet for the mail reminder time at now. With quiet the last
// reminder does not count: the directives gone quiet for the time (the
// settings' 넘긴 것).
func (s *Service) dueDirectives(ctx context.Context, now time.Time, set remind.Setting, base func(string) string, isBoss func(string) bool, quiet bool) ([]dueDirective, error) {
	open, err := s.openDirectives(base, isBoss)
	if err != nil {
		return nil, err
	}
	var out []dueDirective
	for _, d := range open {
		if d.Status != "read" || isBoss(d.Recipient) {
			continue
		}
		n, last, err := remind.Sent(ctx, s.db, identity.SystemSender, remindPrefix(d.ID))
		if err != nil {
			return nil, err
		}
		if quiet {
			last = time.Time{}
		}
		if set.Due(now, d.At, d.LastAnswer, last) {
			out = append(out, dueDirective{d, n})
		}
	}
	return out, nil
}

// MailOverdue is how many open directives have been quiet for the mail
// reminder time (0 when it is off).
func (s *Service) MailOverdue(ctx context.Context, base func(string) string) (int, error) {
	now := s.now().UTC()
	set, err := remind.Load(ctx, s.db, remind.Mail, now)
	if err != nil || !set.On() {
		return 0, err
	}
	due, err := s.dueDirectives(ctx, now, set, base, s.ids.IsBossName(base), true)
	return len(due), err
}

// sweepIdle tells the sender of each idle open directive (idle.go) up to
// the idle max times, one threshold apart, the first not before the
// threshold after the mail reminder time was set, so switching on sends no
// burst.
func (s *Service) sweepIdle(ctx context.Context, now, enabled time.Time, base func(string) string, isBoss func(string) bool) (int, error) {
	idle := s.cfg.Remind.idle()
	if idle <= 0 || now.Before(enabled.Add(idle)) {
		return 0, nil
	}
	live, err := s.liveSessions(now)
	if err != nil {
		return 0, err
	}
	stalls, err := s.idleOpen(now, live, base, isBoss)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, st := range stalls {
		n, last, err := s.idleNoticesSent(ctx, st.ID)
		if err != nil {
			return sent, err
		}
		if n >= s.cfg.Remind.idleMax() || (n > 0 && now.Before(last.Add(idle))) {
			continue
		}
		subject := fmt.Sprintf("[멈춤] #%d %s", st.ID, st.Subject)
		body := fmt.Sprintf("#%d (%s) 는 끝나지 않았고(reply --done·complete 없음), 세션 %s 이(가) %s째 idle 이다. 끝났는지 확인하라", st.ID, st.Recipient, strings.Join(st.Sessions, ", "), shortDuration(now.Sub(st.Since).Truncate(time.Minute)))
		var stored bool
		err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
			var e error
			stored, e = mail.InsertSystemMailOnce(tx, st.Sender, subject, body, st.TaskID, "normal", idleAfterKey(st.ID, n+1), now)
			return e
		})
		if err != nil {
			return sent, err
		}
		if stored {
			sent++
			s.wake(st.Sender)
		}
	}
	return sent, nil
}

// idleNoticesSent reads how many idle notices directive id got and when the
// last one was sent.
func (s *Service) idleNoticesSent(ctx context.Context, id int64) (n int, last time.Time, err error) {
	first := idleAfterKey(id, 1)
	rows, err := s.db.QueryContext(ctx, `SELECT idem,created_at FROM messages WHERE sender=? AND (idem=? OR (idem>=? AND idem<?))`,
		identity.SystemSender, first, first+":", first+";")
	if err != nil {
		return 0, time.Time{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var idem, created string
		if err = rows.Scan(&idem, &created); err != nil {
			return 0, time.Time{}, err
		}
		k := 1
		if idem != first {
			if k, err = strconv.Atoi(idem[len(first)+1:]); err != nil {
				err = nil
				continue
			}
		}
		if k > n {
			n = k
			last, _ = time.Parse(time.RFC3339Nano, created)
		}
	}
	return n, last, rows.Err()
}
