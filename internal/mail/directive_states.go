package mail

import (
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// DirectiveStates is the state (wire.DirectiveOpen or wire.DirectiveDone,
// under the answered rule) of each of ids that is directive mail; mail that
// is not a directive (a reply, a report, no-reply or system mail) is left
// out. openOnly leaves out the ended ones too, reading only the mail that
// may be open. The requests' remaining work reads it (#564).
func (s *Service) DirectiveStates(ids []int64, openOnly bool) (map[int64]string, error) {
	out := map[int64]string{}
	const chunk = 500
	for len(ids) > 0 {
		n := min(chunk, len(ids))
		part := ids[:n]
		ids = ids[n:]
		marks := make([]string, len(part))
		args := make([]any, len(part))
		for i, id := range part {
			marks[i], args[i] = "?", id
		}
		q := `SELECT m.id FROM messages m ` + StatusJoin + ` WHERE m.id IN (` + strings.Join(marks, ",") + `) AND (m.reply_to IS NULL OR m.directive=1)`
		if openOnly {
			q += ` AND m.no_reply=0 AND ` + StatusExpr + ` NOT IN ('complete','completed')`
		}
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		var cand []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			cand = append(cand, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		for _, id := range cand {
			m, err := s.Get(id)
			if err != nil || !s.isDirective(m) {
				continue
			}
			st, ok := s.directiveStateOf(id)
			if !ok {
				continue
			}
			if name := stateName(st); !openOnly || name == wire.DirectiveOpen {
				out[id] = name
			}
		}
	}
	return out, nil
}
