package mail

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// The reminder of open left items (#553). An item still open one reminder
// time after it was recorded gets its directive's sender one mail from the
// system sender, all of that sender's due items in one, and again each
// reminder time after that until the item ends (issue, pass, done, drop).
//
// The time is counted from the later of the item's recorded_at, the last
// reminder (reminded_at) and since, when the reminder time was last set: the
// server's first use writes the default (24h) with that time, and every
// change writes the new value with the time of the change, so neither a
// deploy nor a change sends the items left before at once. 0 is off.
//
// A sweep marks each item it reminds of with a conditional update in the
// transaction that stores the mail, so repeated or concurrent sweeps remind
// of an item once per reminder time.

const (
	// DefaultLeftRemind is the reminder time the server starts with.
	DefaultLeftRemind = 24 * time.Hour
	// leftRemindLines is the most items one reminder lists; the rest are
	// counted on one line.
	leftRemindLines = 20
	// leftRemindText is the most characters of an item's text one line
	// shows.
	leftRemindText = 200
	// LeftRemindMin and LeftRemindMax bound a reminder time that is on.
	LeftRemindMin = time.Minute
	LeftRemindMax = 365 * 24 * time.Hour
)

// leftRemindRow reads the setting, writing the default with now first when
// there is none.
func (s *Service) leftRemindRow(ctx context.Context, now time.Time) (after time.Duration, since time.Time, err error) {
	if _, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO left_remind(id,after_s,since) VALUES(1,?,?)`, int64(DefaultLeftRemind/time.Second), now.UnixMilli()); err != nil {
		return 0, since, err
	}
	var afterS, sinceMS int64
	if err = s.db.QueryRowContext(ctx, `SELECT after_s,since FROM left_remind WHERE id=1`).Scan(&afterS, &sinceMS); err != nil {
		return 0, since, err
	}
	return time.Duration(afterS) * time.Second, time.UnixMilli(sinceMS).UTC(), nil
}

// LeftRemind is the reminder setting in force.
func (s *Service) LeftRemind(ctx context.Context) (wire.LeftRemind, error) {
	after, since, err := s.leftRemindRow(ctx, s.now().UTC())
	if err != nil {
		return wire.LeftRemind{}, err
	}
	return leftRemindView(after, since), nil
}

func leftRemindView(after time.Duration, since time.Time) wire.LeftRemind {
	v := wire.LeftRemind{On: after > 0, AfterSeconds: int64(after / time.Second), Since: &since}
	if after > 0 {
		v.After = ShortDuration(after)
	}
	return v
}

// ParseLeftRemind reads a reminder time: "" or "0" is off, else a duration
// such as 24h or 90m between LeftRemindMin and LeftRemindMax.
func ParseLeftRemind(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < LeftRemindMin || d > LeftRemindMax {
		return 0, errors.New("after: empty (off) or a duration from 1m to 8760h such as 24h")
	}
	return d.Truncate(time.Second), nil
}

// SetLeftRemind sets the reminder time (0 off). A new value counts from now;
// the same value keeps its time.
func (s *Service) SetLeftRemind(ctx context.Context, after time.Duration) (wire.LeftRemind, error) {
	now := s.now().UTC()
	if _, _, err := s.leftRemindRow(ctx, now); err != nil {
		return wire.LeftRemind{}, err
	}
	afterS := int64(after / time.Second)
	if _, err := s.db.ExecContext(ctx, `UPDATE left_remind SET after_s=?,since=? WHERE id=1 AND after_s<>?`, afterS, now.UnixMilli(), afterS); err != nil {
		return wire.LeftRemind{}, err
	}
	return s.LeftRemind(ctx)
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
	after, since, err := s.leftRemindRow(ctx, now)
	if err != nil || after <= 0 || now.Before(since.Add(after)) {
		return 0, err
	}
	due, err := s.dueLeft(ctx, now, after, since)
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
			subject, body := leftRemindMail(marked, after)
			key := "left-remind:" + to + ":" + strconv.FormatInt(marked[0].id, 10) + ":" + strconv.FormatInt(now.UnixMilli(), 10)
			var err error
			stored, err = InsertSystemMailOnce(tx, to, subject, body, "", "normal", key, now)
			return err
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

// dueLeft reads the open items due a reminder at now, oldest first.
func (s *Service) dueLeft(ctx context.Context, now time.Time, after time.Duration, since time.Time) ([]dueLeft, error) {
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
		from := since
		for _, v := range []string{recorded, it.reminded} {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil && t.After(from) {
				from = t
			}
		}
		if !now.Before(from.Add(after)) {
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

// leftRemindMail is one reminder's subject and body: a line per item (its
// ID, text and directive), at most leftRemindLines, then how many more, then
// the commands that end an item.
func leftRemindMail(items []dueLeft, after time.Duration) (subject, body string) {
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
	b.WriteString("\n처리: left done ID · left drop ID --reason \"까닭\" · left issue ID URL · left pass ID --directive M")
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
