// Package remind is the one reminder rule every kind shares (#586): mail
// (open directives, to the role they were sent to), left (open left items,
// to their directive's sender) and request (the owner's open requests, to
// the boss role that took them).
//
// Each kind has one reminder time in remind_settings, 0 off, and since, when
// that value was set. An item is due once it has been quiet for the time:
// counted from the latest of since, the item's own news and its last
// reminder. So a reminder repeats once per time until the item ends, news
// starts the count again, and neither a deploy nor a change of the time
// sends the items quiet before at once.
//
// The package reads and writes only its table and, for Sent, the system
// mail's idempotency keys; each kind's sweep lives with its data (presence,
// mail, requests) and asks Due here.
package remind

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Kind names one kind of reminder.
type Kind string

const (
	Mail    Kind = "mail"    // open directives, to their recipient
	Left    Kind = "left"    // open left items, to their directive's sender
	Request Kind = "request" // open owner's requests, to their boss role
)

// Kinds lists the kinds in the order the settings show them.
var Kinds = []Kind{Mail, Left, Request}

// Defaults is the time each kind starts with: mail off (the operator sets
// it on the settings screen), left items and requests a day.
var Defaults = map[Kind]time.Duration{Mail: 0, Left: 24 * time.Hour, Request: 24 * time.Hour}

// Min and Max bound a reminder time that is on.
const (
	Min = time.Minute
	Max = 365 * 24 * time.Hour
)

// Setting is one kind's reminder time and since, when it was set.
type Setting struct {
	Kind  Kind
	After time.Duration // 0 is off
	Since time.Time
}

// On reports whether the kind reminds at all.
func (s Setting) On() bool { return s.After > 0 }

// Due reports whether an item is due a reminder at now: the setting is on
// and the latest of since and from (the item's news and last reminder; zero
// times are skipped) is at least After ago.
func (s Setting) Due(now time.Time, from ...time.Time) bool {
	return s.On() && !now.Before(s.Start(from...).Add(s.After))
}

// Start is the latest of since and from: where the item's quiet time
// counts from.
func (s Setting) Start(from ...time.Time) time.Time {
	start := s.Since
	for _, t := range from {
		if t.After(start) {
			start = t
		}
	}
	return start
}

// Valid reports whether k is a kind.
func Valid(k Kind) bool {
	_, ok := Defaults[k]
	return ok
}

// Load reads kind k's setting, writing its default with now first when there
// is none.
func Load(ctx context.Context, db *sql.DB, k Kind, now time.Time) (Setting, error) {
	if !Valid(k) {
		return Setting{}, errors.New("unknown reminder kind " + string(k))
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO remind_settings(kind,after_s,since) VALUES(?,?,?)`, string(k), int64(Defaults[k]/time.Second), now.UnixMilli()); err != nil {
		return Setting{}, err
	}
	var afterS, sinceMS int64
	if err := db.QueryRowContext(ctx, `SELECT after_s,since FROM remind_settings WHERE kind=?`, string(k)).Scan(&afterS, &sinceMS); err != nil {
		return Setting{}, err
	}
	return Setting{Kind: k, After: time.Duration(afterS) * time.Second, Since: time.UnixMilli(sinceMS).UTC()}, nil
}

// Save sets kind k's time (0 off). A new value counts from now; the same
// value keeps its since.
func Save(ctx context.Context, db *sql.DB, k Kind, after time.Duration, now time.Time) (Setting, error) {
	if _, err := Load(ctx, db, k, now); err != nil {
		return Setting{}, err
	}
	afterS := int64(after / time.Second)
	if _, err := db.ExecContext(ctx, `UPDATE remind_settings SET after_s=?,since=? WHERE kind=? AND after_s<>?`, afterS, now.UnixMilli(), string(k), afterS); err != nil {
		return Setting{}, err
	}
	return Load(ctx, db, k, now)
}

// Parse reads a reminder time: "" or "0" is off, else a duration such as
// 24h or 90m between Min and Max.
func Parse(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < Min || d > Max {
		return 0, errors.New("after: empty (off) or a duration from 1m to 8760h such as 24h")
	}
	return d.Truncate(time.Second), nil
}

// Sent reads the reminders already stored under prefix (keys prefix + n,
// n from 1) by sender: how many, the highest n, and when that one was
// stored (zero when none). The stored mail is the schedule, so repeated or
// concurrent sweeps and restarts send each n once.
func Sent(ctx context.Context, db *sql.DB, sender, prefix string) (n int, last time.Time, err error) {
	// A range on idem reads the UNIQUE(sender,idem) index; prefix ends in
	// ':' and ':' + 1 is ';'.
	rows, err := db.QueryContext(ctx, `SELECT idem,created_at FROM messages WHERE sender=? AND idem>=? AND idem<?`,
		sender, prefix, prefix[:len(prefix)-1]+";")
	if err != nil {
		return 0, time.Time{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var idem, created string
		if err = rows.Scan(&idem, &created); err != nil {
			return 0, time.Time{}, err
		}
		k, e := strconv.Atoi(idem[len(prefix):])
		if e != nil {
			continue
		}
		if k > n {
			n = k
			last, _ = time.Parse(time.RFC3339Nano, created)
		}
	}
	return n, last, rows.Err()
}
