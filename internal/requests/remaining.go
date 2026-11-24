package requests

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// MailStates is the state of each of ids that is directive mail
// (wire.DirectiveOpen or wire.DirectiveDone); openOnly keeps the open ones.
// The server passes mail's rule in (mail.Service.DirectiveStates), so this
// package does not import mail.
type MailStates func(ids []int64, openOnly bool) (map[int64]string, error)

// leftRow is a left item under a request.
type leftRow struct {
	id                    int64
	kind, text, state, by string
	at                    time.Time
}

func (l leftRow) open() bool { return l.state == wire.LeftOpen }

// gatherLeft adds the left items (not none rows) of the requests to out:
// the request they were attached to, else their directive's on the chain.
func (s *Service) gatherLeft(ctx context.Context, out map[int64]*items, keep func(int64) bool) error {
	rows, err := s.db.QueryContext(ctx, ChainSQL+`SELECT `+LeftRequestExpr+`,l.id,l.kind,l.text,l.state,l.recorded_by,l.recorded_at FROM left_items l LEFT JOIN chain c ON c.id=l.directive_id WHERE l.kind<>? ORDER BY l.id`, wire.LeftKindNone)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rid int64
		var l leftRow
		var at string
		if err = rows.Scan(&rid, &l.id, &l.kind, &l.text, &l.state, &l.by, &at); err != nil {
			return err
		}
		if rid == 0 || !keep(rid) {
			continue
		}
		l.at = parse(at)
		it := out[rid]
		if it == nil {
			it = &items{}
			out[rid] = it
		}
		it.left = append(it.left, l)
	}
	return rows.Err()
}

// mailIDs is every mail ID of its.
func mailIDs(its map[int64]*items) []int64 {
	var ids []int64
	for _, it := range its {
		for _, m := range it.mail {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// states reads the directive states of ids; with no MailStates every mail
// is taken as not a directive.
func (s *Service) states(ids []int64, openOnly bool) (map[int64]string, error) {
	if s.mailStates == nil || len(ids) == 0 {
		return map[int64]string{}, nil
	}
	return s.mailStates(ids, openOnly)
}

// remaining is the work left under a request, oldest first, and what has
// ended. states holds the request's directive mail.
func remaining(it *items, states map[int64]string) ([]wire.RequestOpenItem, wire.RequestFinished) {
	open := []wire.RequestOpenItem{}
	fin := wire.RequestFinished{Reports: len(it.reports)}
	for _, m := range it.mail {
		switch states[m.ID] {
		case wire.DirectiveOpen:
			open = append(open, wire.RequestOpenItem{Kind: wire.RequestOpenMail, ID: strconv.FormatInt(m.ID, 10), Title: m.Subject, Who: m.Recipient, At: m.CreatedAt,
				Close: fmt.Sprintf("complete %d --left-none (its recipient) or close %d --left-none (its sender)", m.ID, m.ID)})
		case wire.DirectiveDone:
			fin.Mail++
		}
	}
	for _, q := range it.questions {
		if q.Status != "pending" {
			continue
		}
		title := q.Header
		if title == "" {
			title = q.Question
		}
		open = append(open, wire.RequestOpenItem{Kind: wire.RequestOpenQuestion, ID: q.ID, Title: title, Who: q.Asker, At: q.CreatedAt,
			Close: "answer it on the panel, or question withdraw " + q.ID})
	}
	for _, l := range it.left {
		if !l.open() {
			fin.Left++
			continue
		}
		open = append(open, wire.RequestOpenItem{Kind: wire.RequestOpenLeft, ID: strconv.FormatInt(l.id, 10), Title: l.text, Who: l.by, At: l.at,
			Close: fmt.Sprintf("left done %d, left drop %d --reason TEXT or left pass %d --directive M", l.id, l.id, l.id)})
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].At.Before(open[j].At) })
	return open, fin
}

// openCount is how many of it are left, given the open directive mail.
func openCount(it *items, open map[int64]string) int {
	if it == nil {
		return 0
	}
	n := 0
	for _, m := range it.mail {
		if open[m.ID] == wire.DirectiveOpen {
			n++
		}
	}
	for _, q := range it.questions {
		if q.Status == "pending" {
			n++
		}
	}
	for _, l := range it.left {
		if l.open() {
			n++
		}
	}
	return n
}
