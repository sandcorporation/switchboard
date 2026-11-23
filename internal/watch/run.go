package watch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Fetcher does one HTTP round trip.
type Fetcher func(req *http.Request) (*http.Response, error)

func httpFetcher(timeout time.Duration) Fetcher {
	c := &http.Client{
		Timeout: timeout,
		// A redirect is answered as it is (3xx counts as a failure): the
		// headers, secrets among them, go to the written address only.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c.Do
}

// fetched is one fetch's outcome. err is already safe to show: no query
// string, no header value.
type fetched struct {
	status int
	body   JSON
	err    string
}

// redact is the address as mail and errors show it: scheme, host and path.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(주소)"
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

// fetch GETs the rule's address with its headers. values are the header
// values to send by lower-case name (secrets already read).
func (s *Service) fetch(ctx context.Context, rawURL string, headers []storedHeader, values map[string]string) fetched {
	ctx, cancel := context.WithTimeout(ctx, s.limits.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetched{err: "주소를 읽을 수 없음"}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "switchboard-watch")
	var hidden []string
	for _, h := range headers {
		v := values[strings.ToLower(h.Name)]
		if h.Secret && v != "" {
			hidden = append(hidden, v)
		}
		req.Header.Set(h.Name, v)
	}
	clean := func(msg string) string {
		for _, v := range hidden {
			msg = strings.ReplaceAll(msg, v, "***")
		}
		if q := req.URL.RawQuery; q != "" {
			msg = strings.ReplaceAll(msg, q, "…")
		}
		return msg
	}
	resp, err := s.hooks.Fetch(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return fetched{err: "시간 초과"}
		}
		return fetched{err: clean("연결 실패: " + err.Error())}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, s.limits.MaxBody+1))
	out := fetched{status: resp.StatusCode}
	switch {
	case err != nil:
		out.err = "응답 읽기 실패"
		if ctx.Err() != nil {
			out.err = "시간 초과"
		}
	case int64(len(b)) > s.limits.MaxBody:
		out.err = fmt.Sprintf("응답이 %dKB를 넘음", s.limits.MaxBody>>10)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		out.err = fmt.Sprintf("HTTP %d", resp.StatusCode)
	default:
		out.body, err = decode(b)
		if err != nil {
			out.err = err.Error()
		}
	}
	return out
}

// secretValues reads a stored rule's secret header values; a lost one is
// a failure the login must fix.
func (s *Service) secretValues(ctx context.Context, id int64, hs []storedHeader) (map[string]string, string) {
	out := map[string]string{}
	for _, h := range hs {
		k := strings.ToLower(h.Name)
		if !h.Secret {
			out[k] = h.Value
			continue
		}
		if s.hooks.Secrets == nil || h.SetAt == "" {
			return nil, "머리글 " + h.Name + " 키 다시 등록 필요"
		}
		v, err := s.hooks.Secrets.Get(ctx, SecretRef(id, h.Name))
		if errors.Is(err, ErrSecretLost) {
			return nil, "머리글 " + h.Name + " 키 다시 등록 필요"
		}
		if err != nil {
			return nil, "머리글 " + h.Name + " 키를 읽지 못함"
		}
		out[k] = v
	}
	return out, ""
}

// RunDue runs every enabled rule whose time has come, at most
// Limits.Parallel at once, and returns how many ran. A rule is claimed
// (its next time moved a period ahead) before its fetch, so a slow fetch
// never runs twice.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now().UTC()
	stamp := now.Format(time.RFC3339)
	var due []row
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT `+cols+` FROM watches WHERE enabled=1 AND (next_at='' OR next_at<=?) ORDER BY next_at,id LIMIT 100`, stamp)
		if err != nil {
			return err
		}
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				rows.Close()
				return err
			}
			due = append(due, r)
		}
		rows.Close()
		for _, r := range due {
			next := now.Add(time.Duration(r.Every) * time.Minute).Format(time.RFC3339)
			if _, err := tx.Exec(`UPDATE watches SET next_at=? WHERE id=?`, next, r.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || len(due) == 0 {
		return 0, err
	}
	par := s.limits.Parallel
	if par < 1 {
		par = 1
	}
	sem := make(chan struct{}, par)
	var wg sync.WaitGroup
	for _, r := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(r row) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.run(ctx, r); err != nil {
				log.Printf("watch %d: %v", r.ID, err)
			}
		}(r)
	}
	wg.Wait()
	return len(due), nil
}

// outcome is a run's evaluation, before anything is stored.
type outcome struct {
	f        fetched
	matched  bool   // value: holds now
	value    string // value: what it looked at
	items    []item // new: every item with a key
	newItems []item // new: keys not seen before (empty on a baseline)
}

// evaluate runs the condition on a fetched body; a list path that is not
// an array is a failure like a bad fetch.
func evaluate(c wire.WatchCond, f fetched, seen func(key string) bool) outcome {
	o := outcome{f: f}
	if f.err != "" {
		return o
	}
	if c.Kind == wire.WatchValue {
		o.matched, o.value = holds(c, f.body)
		return o
	}
	items, err := listItems(c, f.body)
	if err != nil {
		o.f.err = err.Error()
		return o
	}
	o.items = items
	for _, it := range items {
		if seen != nil && !seen(it.key) {
			o.newItems = append(o.newItems, it)
		}
	}
	return o
}

func (s *Service) seenSet(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, id int64) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT key FROM watch_seen WHERE watch_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// run fetches one rule, stores its state and seen keys, then sends its
// mail. The state is stored first: a mail that fails to send is logged and
// not retried, so no change is told twice.
func (s *Service) run(ctx context.Context, r row) error {
	values, bad := s.secretValues(ctx, r.ID, r.headers)
	f := fetched{err: bad}
	if bad == "" {
		f = s.fetch(ctx, r.URL, r.headers, values)
	}
	var seen map[string]bool
	if r.Cond.Kind == wire.WatchNew && r.seeded {
		var err error
		if seen, err = s.seenSet(ctx, s.db, r.ID); err != nil {
			return err
		}
	}
	o := evaluate(r.Cond, f, func(k string) bool { return seen == nil || seen[k] })
	if !r.seeded {
		o.newItems = nil
	}
	now := s.now().UTC()
	stamp := now.Format(time.RFC3339)
	var notes []Notice
	note := func(subject, body string, urgent bool) {
		notes = append(notes, Notice{To: r.To, Source: "watch:" + r.Name, Subject: subject, Body: body, Urgent: urgent, Window: time.Duration(r.Window) * time.Minute})
	}
	head := fmt.Sprintf("규칙: %s\n주소: %s\n", r.Name, redact(r.URL))
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		// The rule may have changed or gone while it was fetched: store
		// nothing then (the next pass runs the new one).
		var upd string
		if err := tx.QueryRow(`SELECT updated_at FROM watches WHERE id=?`, r.ID).Scan(&upd); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if upd != r.UpdatedAt {
			return nil
		}
		if o.f.err != "" {
			fails := r.Fails + 1
			failing := r.Failing
			if !failing && fails >= r.FailAfter {
				failing = true
				note(fmt.Sprintf("[지켜보기] %s 조회 실패", r.Name), head+fmt.Sprintf("%d번 연달아 실패\n마지막 오류: %s\n", fails, o.f.err), false)
			}
			_, err := tx.Exec(`UPDATE watches SET last_at=?,last_status=?,last_ok=0,last_result=?,fails=?,failing=? WHERE id=?`,
				stamp, o.f.status, "실패: "+o.f.err, fails, b2i(failing), r.ID)
			return err
		}
		if r.Failing {
			note(fmt.Sprintf("[지켜보기] %s 다시 됨", r.Name), head+fmt.Sprintf("%d번 실패 뒤 다시 조회됨\n", r.Fails), false)
		}
		result := ""
		matched := r.Matched
		seeded := r.seeded
		switch r.Cond.Kind {
		case wire.WatchValue:
			matched = o.matched
			result = "안 맞음"
			if o.matched {
				result = "맞음"
			}
			if o.matched && !r.Matched {
				subject := s.subject(r.WatchRule, f.body)
				note(subject, head+fmt.Sprintf("조건: %s\n값: %s\n", describe(r.Cond), orDash(o.value)), r.Urgent)
			}
			seeded = true
		case wire.WatchNew:
			add := o.items
			if seeded {
				add = o.newItems
				result = fmt.Sprintf("새 항목 %d", len(o.newItems))
				s.newNotes(r, head, o.newItems, note)
			} else {
				result = fmt.Sprintf("기준 %d개", len(o.items))
				seeded = true
			}
			for _, it := range add {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO watch_seen(watch_id,key,seen_at) VALUES(?,?,?)`, r.ID, it.key, stamp); err != nil {
					return err
				}
			}
			if len(add) > 0 && s.limits.SeenCap > 0 {
				if _, err := tx.Exec(`DELETE FROM watch_seen WHERE watch_id=? AND rowid NOT IN (SELECT rowid FROM watch_seen WHERE watch_id=? ORDER BY seen_at DESC, rowid DESC LIMIT ?)`, r.ID, r.ID, s.limits.SeenCap); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(`UPDATE watches SET last_at=?,last_status=?,last_ok=1,last_result=?,matched=?,seeded=?,fails=0,failing=0 WHERE id=?`,
			stamp, o.f.status, result, b2i(matched), b2i(seeded), r.ID)
		return err
	})
	if err != nil {
		return err
	}
	for _, n := range notes {
		if s.hooks.Notify == nil {
			break
		}
		if err := s.hooks.Notify(ctx, n); err != nil {
			log.Printf("watch %d: notice: %v", r.ID, err)
		}
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "(없음)"
	}
	return s
}

// subject is a mail's subject: the template filled from v, or the default.
func (s *Service) subject(w wire.WatchRule, v JSON) string {
	if w.Subject != "" {
		if t := render(w.Subject, v); t != "" {
			return cut(t, 200)
		}
	}
	if w.Cond.Kind == wire.WatchNew {
		key, _ := lookup(v, w.Cond.Key)
		return cut(fmt.Sprintf("[지켜보기] %s 새 항목 %s", w.Name, text(key)), 200)
	}
	return "[지켜보기] " + w.Name
}

// newNotes are one mail per new key up to Limits.MaxNew, then one for the
// rest.
func (s *Service) newNotes(r row, head string, items []item, note func(string, string, bool)) {
	limit := s.limits.MaxNew
	if limit < 1 {
		limit = 1
	}
	for i, it := range items {
		if i == limit {
			rest := make([]string, 0, len(items)-limit)
			for _, x := range items[limit:] {
				rest = append(rest, x.key)
			}
			if len(rest) > 20 {
				rest = append(rest[:20], "…")
			}
			note(fmt.Sprintf("[지켜보기] %s 새 항목 %d개 더", r.Name, len(items)-limit), head+fmt.Sprintf("조건: %s\n키: %s\n", describe(r.Cond), strings.Join(rest, ", ")), r.Urgent)
			return
		}
		body := head + fmt.Sprintf("조건: %s\n키: %s\n", describe(r.Cond), it.key)
		if fs := fields(it.v); len(fs) > 0 {
			body += strings.Join(fs, "\n") + "\n"
		}
		note(s.subject(r.WatchRule, it.v), body, r.Urgent)
	}
}

// Test fetches a rule once as written and evaluates it: nothing is stored
// and no mail is sent. With id, a secret header written without a value
// uses the stored one and a new-item rule counts against the stored keys.
func (s *Service) Test(ctx context.Context, id int64, w wire.WatchRule) (wire.WatchTest, error) {
	if err := s.normalize(ctx, &w); err != nil {
		return wire.WatchTest{}, err
	}
	var old row
	if id != 0 {
		var err error
		if old, err = s.get(ctx, id); err != nil {
			return wire.WatchTest{}, err
		}
	}
	hs, puts := s.headersFor(w.Headers, old.headers, "")
	values := map[string]string{}
	for _, h := range hs {
		k := strings.ToLower(h.Name)
		switch {
		case !h.Secret:
			values[k] = h.Value
		case puts[k] != "":
			values[k] = puts[k]
		default:
			stored, bad := s.secretValues(ctx, id, []storedHeader{{Name: h.Name, Secret: true, SetAt: oldSetAt(old.headers, k)}})
			if bad != "" || id == 0 {
				if bad == "" {
					bad = "머리글 " + h.Name + " 키를 적으세요"
				}
				return wire.WatchTest{Error: bad}, nil
			}
			values[k] = stored[k]
		}
	}
	f := s.fetch(ctx, w.URL, hs, values)
	var seen map[string]bool
	if id != 0 && old.seeded && w.Cond.Kind == wire.WatchNew && old.URL == w.URL && sameCond(old.Cond, w.Cond) {
		var err error
		if seen, err = s.seenSet(ctx, s.db, id); err != nil {
			return wire.WatchTest{}, err
		}
	}
	o := evaluate(w.Cond, f, func(k string) bool { return seen == nil || seen[k] })
	out := wire.WatchTest{Status: o.f.status, OK: o.f.err == "", Error: o.f.err, Matched: o.matched, Value: o.value, Items: len(o.items)}
	if seen != nil {
		out.New = len(o.newItems)
	}
	if out.OK {
		switch {
		case w.Cond.Kind == wire.WatchValue:
			out.Subject = s.subject(w, o.f.body)
		case len(o.items) > 0:
			first := o.items[0]
			if len(o.newItems) > 0 {
				first = o.newItems[0]
			}
			out.Subject = s.subject(w, first.v)
		}
	}
	return out, nil
}

func oldSetAt(hs []storedHeader, lower string) string {
	for _, h := range hs {
		if strings.ToLower(h.Name) == lower && h.Secret {
			return h.SetAt
		}
	}
	return ""
}

func sameCond(a, b wire.WatchCond) bool { return a == b }
