package mail

import (
	"errors"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// BossFilter narrows a boss role's mail list (#397).
type BossFilter struct {
	Open  bool      // only mail not complete (pending, read, acknowledged, held)
	From  string    // sender mailbox
	To    string    // recipient mailbox
	Since time.Time // created at or after; zero for no bound
	Limit int       // 1-500; 0 is 100
}

// BossList lists every mailbox's mail newest first, as the dashboard's mail
// tab does, for a boss role whose view see allows: a message is in view when
// its sender or its recipient is. Nothing is marked read and no view is
// recorded (the list carries subjects and bodies the dashboard shows too).
func (s *Service) BossList(f BossFilter, see func(string) bool) ([]wire.Message, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	query := `SELECT ` + messageCols(StatusExpr) + ` FROM messages m ` + StatusJoin + ` WHERE 1=1`
	args := []any{}
	if f.Open {
		query += ` AND ` + StatusExpr + ` NOT IN ('complete','completed')`
	}
	if f.From != "" {
		from := f.From
		if mb, ok := identity.Mailbox(s.db, from); ok {
			from = mb
		}
		query += ` AND m.sender=?`
		args = append(args, from)
	}
	if f.To != "" {
		to := f.To
		if mb, ok := identity.Mailbox(s.db, to); ok {
			to = mb
		}
		query += ` AND m.recipient=?`
		args = append(args, to)
	}
	if !f.Since.IsZero() {
		query += ` AND m.created_at>=?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	rows, err := s.db.Query(query+` ORDER BY m.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.Message{}
	for rows.Next() && len(out) < limit {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		if see(m.Sender) || see(m.Recipient) {
			m.ViewOnly = true
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// ErrNotInView is BossShow's answer for a message the boss role may not see
// (the same 404 as an unknown ID, so its existence does not leak).
var ErrNotInView = errors.New("not found")

// BossShow is one message for a boss role (#397): its state is left as it is,
// and when it is neither sent nor received by viewer the view is recorded,
// the same record a boss role's `read` of another role's mail leaves (#355).
func (s *Service) BossShow(viewer identity.Principal, id int64, see func(string) bool) (wire.Message, error) {
	m, err := s.View(id)
	if err != nil {
		return wire.Message{}, ErrNotInView
	}
	own := m.Sender == viewer.Role || m.Recipient == viewer.Role
	if !own && !see(m.Sender) && !see(m.Recipient) {
		return wire.Message{}, ErrNotInView
	}
	if !own {
		if err := s.recordView(viewer.Role, m.Recipient, id); err != nil {
			return wire.Message{}, err
		}
	}
	m.AnswersDirective = s.AnswersDirective(m.Recipient, id)
	m.ViewOnly = true
	return m, nil
}
