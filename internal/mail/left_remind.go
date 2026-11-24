package mail

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// The reminder of open left items (#553). An item still open one reminder
// time after it was recorded gets its directive's sender one mail from the
// system sender, all of that sender's due items in one, and again each
// reminder time after that until the item ends (issue, pass, done, drop).
// The mail completes only with a verdict on each open item it lists
// (left_verdict.go, #565).
//
// The time is the left kind of package remind (#586): counted from the
// later of the item's recorded_at, the last reminder (reminded_at) and
// since, when the reminder time was last set, so neither a deploy nor a
// change sends the items left before at once. 0 is off.
//
// A sweep marks each item it reminds of with a conditional update in the
// transaction that stores the mail, so repeated or concurrent sweeps remind
// of an item once per reminder time.

const (
	// leftRemindLines is the most items one reminder lists; the rest are
	// counted on one line.
	leftRemindLines = 20
	// leftRemindText is the most characters of an item's text one line
	// shows.
	leftRemindText = 200
)

// LeftRemind is the reminder setting in force.
func (s *Service) LeftRemind(ctx context.Context) (wire.LeftRemind, error) {
	st, err := remind.Load(ctx, s.db, remind.Left, s.now().UTC())
	if err != nil {
		return wire.LeftRemind{}, err
	}
	return leftRemindView(st), nil
}

func leftRemindView(st remind.Setting) wire.LeftRemind {
	since := st.Since
	v := wire.LeftRemind{On: st.On(), AfterSeconds: int64(st.After / time.Second), Since: &since}
	if st.On() {
		v.After = ShortDuration(st.After)
	}
	return v
}

// SetLeftRemind sets the reminder time (0 off). A new value counts from now;
// the same value keeps its time.
func (s *Service) SetLeftRemind(ctx context.Context, after time.Duration) (wire.LeftRemind, error) {
	st, err := remind.Save(ctx, s.db, remind.Left, after, s.now().UTC())
	if err != nil {
		return wire.LeftRemind{}, err
	}
	return leftRemindView(st), nil
}

// ShortDuration drops the zero minutes and seconds of a duration: 24h,
// 1h30m, 45s.
func ShortDuration(d time.Duration) string {
	t := d.String()
	if strings.HasSuffix(t, "m0s") {
		t = strings.TrimSuffix(t, "0s")
	}
	if strings.HasSuffix(t, "h0m") {
		t = strings.TrimSuffix(t, "0m")
	}
	return t
}

// dueLeft is one open item due a reminder.
type dueLeft struct {
	id, directive    int64
	text, subject    string
	sender, reminded string
}

// SweepLeftRemind sends the due reminders, one mail per directive sender,
// and returns how many it sent.
func (s *Service) SweepLeftRemind(ctx context.Context) (int, error) {
	now := s.now().UTC()
	st, err := remind.Load(ctx, s.db, remind.Left, now)
	if err != nil || !st.Due(now) {
		return 0, err
	}
	after := st.After
	due, err := s.dueLeft(ctx, now, st, false)
	if err != nil {
		return 0, err
	}
	var order []string
	bySender := map[string][]dueLeft{}
	for _, it := range due {
		if _, ok := bySender[it.sender]; !ok {
			order = append(order, it.sender)
		}
		bySender[it.sender] = append(bySender[it.sender], it)
	}
	sent := 0
	at := now.Format(time.RFC3339Nano)
	for _, to := range order {
		var stored bool
		err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
			stored = false
			var marked []dueLeft
			for _, it := range bySender[to] {
				res, err := tx.Exec(`UPDATE left_items SET reminded_at=? WHERE id=? AND state=? AND reminded_at=?`, at, it.id, wire.LeftOpen, it.reminded)
				if err != nil {
					return err
				}
				if n, _ := res.RowsAffected(); n == 1 {
					marked = append(marked, it)
				}
			}
			if len(marked) == 0 {
				return nil
			}
			key := "left-remind:" + to + ":" + strconv.FormatInt(marked[0].id, 10) + ":" + strconv.FormatInt(now.UnixMilli(), 10)
			subject, body := leftRemindMail(0, marked, after)
			var err error
			stored, err = InsertSystemMailOnce(tx, to, subject, body, "", "normal", key, now)
			if err != nil || !stored {
				return err
			}
			// The body ends with the complete of this very mail (#565), so
			// it is written again once the mail has its ID.
			var id int64
			if err = tx.QueryRow(`SELECT id FROM messages WHERE sender=? AND idem=?`, identity.SystemSender, key).Scan(&id); err != nil {
				return err
			}
			_, body = leftRemindMail(id, marked, after)
			if _, err = tx.Exec(`UPDATE messages SET body=? WHERE id=?`, body, id); err != nil {
				return err
			}
			for i, it := range marked {
				if i == leftRemindLines {
					break
				}
				if _, err = tx.Exec(`INSERT OR IGNORE INTO left_remind_items(message_id,item_id) VALUES(?,?)`, id, it.id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return sent, err
		}
		if stored {
			sent++
			s.hub.Publish(to)
		}
	}
	return sent, nil
}

// dueLeft reads the open items due a reminder at now, oldest first. With
// quiet it counts from the item and since only, not its last reminder: the
// items gone quiet for the time (the settings' 넘긴 것).
func (s *Service) dueLeft(ctx context.Context, now time.Time, st remind.Setting, quiet bool) ([]dueLeft, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.id,l.directive_id,l.text,l.recorded_at,l.reminded_at,COALESCE(m.sender,''),COALESCE(m.subject,'')
FROM left_items l LEFT JOIN messages m ON m.id=l.directive_id WHERE l.state=? AND l.kind=? ORDER BY l.id`, wire.LeftOpen, wire.LeftKindItem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dueLeft
	for rows.Next() {
		var it dueLeft
		var recorded string
		if err := rows.Scan(&it.id, &it.directive, &it.text, &recorded, &it.reminded, &it.sender, &it.subject); err != nil {
			return nil, err
		}
		if it.sender == "" {
			continue
		}
		from := []time.Time{parse(recorded)}
		if !quiet {
			from = append(from, parse(it.reminded))
		}
		if st.Due(now, from...) {
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

// LeftOverdue is how many open left items have been quiet for the reminder
// time (0 when it is off).
func (s *Service) LeftOverdue(ctx context.Context) (int, error) {
	now := s.now().UTC()
	st, err := remind.Load(ctx, s.db, remind.Left, now)
	if err != nil || !st.On() {
		return 0, err
	}
	due, err := s.dueLeft(ctx, now, st, true)
	return len(due), err
}

func parse(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

// leftRemindMail is reminder id's subject and body: a line per item (its
// ID, text and directive), at most leftRemindLines, then how many more, then
// the verdicts and the complete that takes one for each listed item (#565).
func leftRemindMail(id int64, items []dueLeft, after time.Duration) (subject, body string) {
	subject = fmt.Sprintf("[남긴 것] %d건 처리 안 됨", len(items))
	var b strings.Builder
	fmt.Fprintf(&b, "남긴 것이 %s 넘게 처리되지 않았다.\n\n", ShortDuration(after))
	for i, it := range items {
		if i == leftRemindLines {
			fmt.Fprintf(&b, "외 %d건\n", len(items)-leftRemindLines)
			break
		}
		fmt.Fprintf(&b, "#%d %s — 지시 #%d %s\n", it.id, oneLine(it.text, leftRemindText), it.directive, oneLine(it.subject, leftRemindText))
	}
	b.WriteString("\n판정(항목마다): done 끝남 · keep 그대로 둠(다음 알림에 다시) · drop:까닭 버림 · issue:URL 이슈로 넘김\n")
	fmt.Fprintf(&b, "끝내기: complete %d", id)
	for i, it := range items {
		if i == leftRemindLines {
			break
		}
		fmt.Fprintf(&b, " --verdict %d=판정", it.id)
	}
	return subject, b.String()
}

// oneLine is v's first line, at most n characters.
func oneLine(v string, n int) string {
	v, _, cut := strings.Cut(strings.TrimSpace(v), "\n")
	r := []rune(strings.TrimSpace(v))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	if cut {
		return string(r) + " …"
	}
	return string(r)
}
