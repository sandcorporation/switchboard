package mail

import (
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Verdicts on a [남긴 것] reminder (#565). A reminder (mail with rows in
// left_remind_items) is completed only with a verdict for each item it
// lists that is still open: done, keep (it stays open and comes again in
// the next reminder), drop with why, or issue with the issue's URL. An item
// ended since (left done, drop, issue, pass) needs none. The complete and
// the verdicts commit together; a missing verdict refuses the complete and
// names the items and the command. Completing a reminder already complete
// asks nothing again.

// verdictUsage is the shape of one --verdict.
const verdictUsage = "--verdict ITEM=done|keep|drop:REASON|issue:URL"

// remindItems is the items reminder id lists, nil for mail that is not a
// reminder. Held mail handed to a session has a new ID (#392): the IDs it
// had before are looked up as well.
func remindItems(db *sql.DB, id int64) ([]int64, error) {
	for i := 0; i < 16 && id > 0; i++ {
		rows, err := db.Query(`SELECT item_id FROM left_remind_items WHERE message_id=? ORDER BY item_id`, id)
		if err != nil {
			return nil, err
		}
		var items []int64
		for rows.Next() {
			var it int64
			if err := rows.Scan(&it); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(items) > 0 {
			return items, nil
		}
		var held sql.NullInt64
		if db.QueryRow(`SELECT held_id FROM messages WHERE id=?`, id).Scan(&held) != nil || !held.Valid {
			return nil, nil
		}
		id = held.Int64
	}
	return nil, nil
}

// openRemindItems is the items reminder id lists that are still open
// (empty, not nil, when none is), nil for mail that is not a reminder: what a
// session must judge to complete it, sent on its event (#571).
func openRemindItems(db *sql.DB, id int64) []int64 {
	items, err := remindItems(db, id)
	if err != nil || len(items) == 0 {
		return nil
	}
	open := []int64{}
	for _, it := range items {
		if itemOpen(db, it) {
			open = append(open, it)
		}
	}
	return open
}

// checkVerdict is why v cannot be taken, "" when it can; it trims v.Value.
func checkVerdict(v *wire.LeftVerdict) string {
	v.Value = strings.TrimSpace(v.Value)
	switch v.Verdict {
	case wire.LeftDone, wire.LeftKeep:
		if v.Value != "" {
			return fmt.Sprintf("--verdict %d=%s takes no value", v.Item, v.Verdict)
		}
	case "drop":
		if v.Value == "" {
			return fmt.Sprintf("--verdict %d=drop:REASON: why it is dropped", v.Item)
		}
		if len([]rune(v.Value)) > leftItemMax {
			return fmt.Sprintf("--verdict %d=drop: the reason is at most %d characters", v.Item, leftItemMax)
		}
	case "issue":
		u, e := url.Parse(v.Value)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Sprintf("--verdict %d=issue:URL: an http(s) issue URL", v.Item)
		}
		v.Value = u.String()
	default:
		return fmt.Sprintf("--verdict %d=%s: the verdict is done, keep, drop:REASON or issue:URL", v.Item, v.Verdict)
	}
	return ""
}

// itemOpen reports whether left item id is open.
func itemOpen(db *sql.DB, id int64) bool {
	var n int
	return db.QueryRow(`SELECT 1 FROM left_items WHERE id=? AND state=? AND kind<>?`, id, wire.LeftOpen, wire.LeftKindNone).Scan(&n) == nil
}

// verdicts checks the verdicts of a request that completes mails (none when
// it completes nothing) for role. It answers the function that applies them
// in the complete's transaction (nil when there is nothing to apply), or
// why the request is refused.
func (s *Service) verdicts(role string, mails []int64, vs []wire.LeftVerdict) (apply func(*sql.Tx, time.Time) error, problem string) {
	if len(vs) > leftItemsMax {
		return nil, fmt.Sprintf("at most %d --verdict", leftItemsMax)
	}
	given := map[int64]wire.LeftVerdict{}
	for _, v := range vs {
		if v.Item <= 0 {
			return nil, verdictUsage + ": ITEM is an item ID"
		}
		if _, dup := given[v.Item]; dup {
			return nil, fmt.Sprintf("--verdict %d: one verdict per item", v.Item)
		}
		if msg := checkVerdict(&v); msg != "" {
			return nil, msg
		}
		given[v.Item] = v
	}
	listed := map[int64]bool{}
	for _, id := range mails {
		items, err := remindItems(s.db, id)
		if err != nil {
			return nil, "could not read the reminder"
		}
		if len(items) == 0 {
			continue
		}
		done, err := Completed(s.db, id)
		if err != nil {
			return nil, "could not read the reminder"
		}
		var missing []int64
		for _, it := range items {
			listed[it] = true
			if _, ok := given[it]; !ok && !done && itemOpen(s.db, it) {
				missing = append(missing, it)
			}
		}
		if len(missing) > 0 {
			var b strings.Builder
			fmt.Fprintf(&b, "reminder #%d lists open items without a verdict:", id)
			for _, it := range missing {
				fmt.Fprintf(&b, " #%d", it)
			}
			fmt.Fprintf(&b, "; give each one (%s), e.g. complete %d", verdictUsage, id)
			for _, it := range missing {
				fmt.Fprintf(&b, " --verdict %d=keep", it)
			}
			return nil, b.String()
		}
	}
	if len(given) == 0 {
		return nil, ""
	}
	if len(listed) == 0 {
		return nil, "--verdict goes with completing a [남긴 것] reminder"
	}
	order := make([]int64, 0, len(given))
	for it := range given {
		if !listed[it] {
			return nil, fmt.Sprintf("--verdict %d: the item is not in the reminder", it)
		}
		order = append(order, it)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	return func(tx *sql.Tx, now time.Time) error {
		at := now.UTC().Format(time.RFC3339Nano)
		for _, it := range order {
			v := given[it]
			state := map[string]string{wire.LeftDone: wire.LeftDone, "drop": wire.LeftDropped, "issue": wire.LeftIssue}[v.Verdict]
			if state == "" { // keep: it stays open
				continue
			}
			// An item ended since keeps how it ended.
			if _, err := tx.Exec(`UPDATE left_items SET state=?,ended_by=?,ended_at=?,value=? WHERE id=? AND state=? AND kind<>?`, state, role, at, v.Value, it, wire.LeftOpen, wire.LeftKindNone); err != nil {
				return err
			}
		}
		return nil
	}, ""
}

// Unjudged is the open items reminder id lists while it is not complete:
// the items its complete needs a verdict for (#565). Nil for other mail.
// Other ways of completing mail (report --mail) refuse a reminder with any.
func Unjudged(db *sql.DB, id int64) ([]int64, error) {
	items, err := remindItems(db, id)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	if done, err := Completed(db, id); err != nil || done {
		return nil, err
	}
	var open []int64
	for _, it := range items {
		if itemOpen(db, it) {
			open = append(open, it)
		}
	}
	return open, nil
}
