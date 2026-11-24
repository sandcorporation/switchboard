package requests

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Request reminders (#586), the request kind of package remind. An open
// request (in progress) quiet for the reminder time gets its boss role (the
// author, the role that took the owner's words) one mail from the system
// sender, and again each reminder time until it is done. Any movement under
// it is news and starts the count again: a note, a report, mail on its
// chain (sent with its number, or below such mail, replies too), a linked
// question or its answer, the waiting mark set or cleared.
//
// A request waiting on the owner (an open linked question, or the boss
// role's 「대표 확인 대기」 mark) gets no mail; past the time it is overdue,
// which the dashboard's 지시 tab marks. Keys are request-remind:<id>:<n>, so
// repeated or concurrent sweeps and restarts send each once.

// SendSystem stores system mail to role to once per key and wakes it,
// reporting whether this call stored it. The server passes mail's in, so
// this package does not import mail.
type SendSystem func(ctx context.Context, to, subject, body, key string, now time.Time) (bool, error)

func remindPrefix(id int64) string { return "request-remind:" + strconv.FormatInt(id, 10) + ":" }

func (s *Service) setting(ctx context.Context) (remind.Setting, error) {
	return remind.Load(ctx, s.db, remind.Request, s.now().UTC())
}

// overdue reports whether open request r has been quiet for the time.
func (s *Service) overdue(r wire.Request, set remind.Setting) bool {
	open := r.Status == wire.RequestProgress || r.Status == wire.RequestWaiting
	return open && set.Due(s.now().UTC(), r.QuietSince)
}

// quietSince is the request's latest news of any kind.
func quietSince(rw *row, it *items) time.Time {
	t := rw.CreatedAt
	at := func(v time.Time) {
		if v.After(t) {
			t = v
		}
	}
	at(rw.waitChanged)
	for _, n := range it.notes {
		at(n.CreatedAt)
	}
	for _, rp := range it.reports {
		at(rp.CreatedAt)
	}
	for _, m := range it.mail {
		at(m.CreatedAt)
	}
	for _, q := range it.questions {
		at(q.CreatedAt)
		if q.AnsweredAt != nil {
			at(*q.AnsweredAt)
		}
	}
	return t
}

// RemindMail is a request reminder's subject and body: the request's number
// and title and when its last news came.
func RemindMail(r wire.Request, now time.Time) (subject, body string) {
	subject = fmt.Sprintf("[지시 소식 없음] #%d %s", r.ID, r.Title)
	quiet := now.Sub(r.QuietSince).Truncate(time.Minute)
	body = fmt.Sprintf("지시 #%d %s\n마지막 소식: %s (%s 전)\n\n끝났으면 request done %d --result …, 대표 답을 기다리면 request wait %d",
		r.ID, r.Title, r.QuietSince.UTC().Format("2006-01-02 15:04 UTC"), shortDuration(quiet), r.ID, r.ID)
	return subject, body
}

func shortDuration(d time.Duration) string {
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// SweepRemind sends the due request reminders through send and returns how
// many it sent.
func (s *Service) SweepRemind(ctx context.Context, send SendSystem) (int, error) {
	now := s.now().UTC()
	set, err := s.setting(ctx)
	if err != nil || !set.Due(now) {
		return 0, err
	}
	list, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, r := range list.Requests {
		if r.Status != wire.RequestProgress || !r.Overdue {
			continue
		}
		n, last, err := remind.Sent(ctx, s.db, identity.SystemSender, remindPrefix(r.ID))
		if err != nil {
			return sent, err
		}
		if !set.Due(now, r.QuietSince, last) {
			continue
		}
		subject, body := RemindMail(r, now)
		ok, err := send(ctx, r.Author, subject, body, remindPrefix(r.ID)+strconv.Itoa(n+1), now)
		if err != nil {
			return sent, err
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// RemindCounts is the request row of the reminder settings: the open
// requests in progress quiet for the time, and those waiting on the owner.
func (s *Service) RemindCounts(ctx context.Context) (overdue, waiting int, err error) {
	list, err := s.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range list.Requests {
		switch {
		case r.Status == wire.RequestWaiting:
			waiting++
		case r.Overdue:
			overdue++
		}
	}
	return overdue, waiting, nil
}
