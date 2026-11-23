// Package watch holds the API watches (#528): rules a dashboard login
// writes (an address to GET on a period, its headers, a condition and the
// mail settings), the fetch that runs them on the server, and the mail to
// the rule's role when a value condition starts to hold, a new item shows
// up, fetches keep failing or work again. No script and no model is
// involved: a rule is data.
//
// A secret header's value never sits in this package's tables: it goes to
// the secret store passed in (Secrets) under the reference SecretRef, and
// no read gives it back. The mail goes through the notice function passed in
// (Notify), which folds the same source and subject within the rule's
// window into one mail. The watches and watch_seen tables are read nowhere
// else.
package watch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Notice is one alert mail from the system sender. Source is
// "watch:<name>"; the same source and subject within Window become one mail.
type Notice struct {
	To      string
	Source  string
	Subject string
	Body    string
	Urgent  bool
	Window  time.Duration
}

// Notify sends a notice.
type Notify func(ctx context.Context, n Notice) error

// ErrSecretLost is what Secrets.Get returns for a value stored under a key
// the server no longer has: it must be written again.
var ErrSecretLost = errors.New("secret must be registered again")

// Secrets is the secret store: write, read for the fetch, remove. Nothing
// else in the server reads a value back.
type Secrets interface {
	Put(ctx context.Context, ref, value string) error
	Get(ctx context.Context, ref string) (string, error)
	Delete(ctx context.Context, ref string) error
	// Info is what a read may say about a stored value: never the value.
	Info(ctx context.Context, ref string) (wire.SecretInfo, error)
}

// Limits bound a fetch and the work of one pass.
type Limits struct {
	Timeout  time.Duration // one fetch, connect to the last byte
	MaxBody  int64         // response bytes
	Parallel int           // fetches at once
	MaxNew   int           // new-item mails per run; the rest in one summary mail
	SeenCap  int           // keys kept per rule, newest first
}

// DefaultLimits are the server's.
var DefaultLimits = Limits{Timeout: 10 * time.Second, MaxBody: 1 << 20, Parallel: 4, MaxNew: 10, SeenCap: 5000}

// Hooks are what the server passes in.
type Hooks struct {
	Notify     Notify
	Secrets    Secrets
	RoleExists func(ctx context.Context, role string) bool
	// Fetch does the HTTP round trip; nil uses a client with Limits.Timeout
	// that follows no redirect (a secret header must not reach another
	// address).
	Fetch Fetcher
}

// Service is built per request (or per pass) from the server's state.
type Service struct {
	db     *sql.DB
	now    func() time.Time
	hooks  Hooks
	limits Limits
}

// New builds the service.
func New(db *sql.DB, now func() time.Time, limits Limits, hooks Hooks) *Service {
	if hooks.Fetch == nil {
		hooks.Fetch = httpFetcher(limits.Timeout)
	}
	return &Service{db: db, now: now, hooks: hooks, limits: limits}
}

// SecretRef is where a rule's secret header value is stored.
func SecretRef(id int64, header string) string {
	return fmt.Sprintf("watch:%d:%s", id, strings.ToLower(header))
}

// storedHeader is one header as the watches row keeps it: a secret
// header's value is never here, only when it was set.
type storedHeader struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	SetAt  string `json:"set_at,omitempty"`
}

type row struct {
	wire.Watch
	headers []storedHeader
	nextAt  string
	seeded  bool
}

const cols = `id,name,to_role,url,headers,every_min,cond,subject,urgent,window_min,fail_after,enabled,created_at,updated_at,next_at,last_at,last_status,last_ok,last_result,matched,seeded,fails,failing,(SELECT COUNT(*) FROM watch_seen WHERE watch_id=watches.id)`

type scanner interface{ Scan(...any) error }

func scanRow(sc scanner) (row, error) {
	var r row
	var hs, cond string
	err := sc.Scan(&r.ID, &r.Name, &r.To, &r.URL, &hs, &r.Every, &cond, &r.Subject, &r.Urgent, &r.Window, &r.FailAfter, &r.Enabled,
		&r.CreatedAt, &r.UpdatedAt, &r.nextAt, &r.LastAt, &r.LastStatus, &r.LastOK, &r.LastResult, &r.Matched, &r.seeded, &r.Fails, &r.Failing, &r.Seen)
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(hs), &r.headers)
	_ = json.Unmarshal([]byte(cond), &r.Cond)
	r.Headers = make([]wire.WatchHeader, 0, len(r.headers))
	for _, h := range r.headers {
		wh := wire.WatchHeader{Name: h.Name, Secret: h.Secret}
		if h.Secret {
			wh.Set, wh.SetAt = h.SetAt != "", h.SetAt
		} else {
			wh.Value = h.Value
		}
		r.Headers = append(r.Headers, wh)
	}
	return r, nil
}

var errNotFound = errors.New("not found")

func (s *Service) get(ctx context.Context, id int64) (row, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM watches WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNotFound
	}
	if err == nil {
		s.secretInfo(ctx, &r)
	}
	return r, err
}

// secretInfo adds each secret header's trace and whether the key still
// opens it.
func (s *Service) secretInfo(ctx context.Context, r *row) {
	if s.hooks.Secrets == nil {
		return
	}
	for i := range r.Headers {
		h := &r.Headers[i]
		if !h.Secret || !h.Set {
			continue
		}
		info, err := s.hooks.Secrets.Info(ctx, SecretRef(r.ID, h.Name))
		if err != nil {
			continue
		}
		h.Hint, h.Unreadable = info.Hint, info.Unreadable
		if !info.Set {
			h.Set, h.SetAt = false, ""
		}
	}
}

// List is every rule, by name.
func (s *Service) List(ctx context.Context) ([]wire.Watch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM watches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.Watch{}
	var rs []row
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		rs = append(rs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range rs {
		s.secretInfo(ctx, &rs[i])
		out = append(out, rs[i].Watch)
	}
	return out, nil
}

// Summaries are the rows `switchboard watch list` shows; visible decides
// which rules a caller sees by their role.
func (s *Service) Summaries(ctx context.Context, visible func(to string) bool) ([]wire.WatchSummary, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []wire.WatchSummary{}
	for _, w := range all {
		if visible(w.To) {
			out = append(out, wire.WatchSummary{Name: w.Name, To: w.To, Every: w.Every, Enabled: w.Enabled, LastAt: w.LastAt, LastOK: w.LastOK, LastResult: w.LastResult})
		}
	}
	return out, nil
}

// errInvalid is a rule a write refuses, with the reason shown to the login.
type errInvalid string

func (e errInvalid) Error() string { return string(e) }

// normalize checks a rule and fills its defaults.
func (s *Service) normalize(ctx context.Context, w *wire.WatchRule) error {
	w.Name = strings.TrimSpace(w.Name)
	w.To = strings.TrimSpace(w.To)
	w.URL = strings.TrimSpace(w.URL)
	w.Subject = strings.TrimSpace(w.Subject)
	if w.Name == "" || len([]rune(w.Name)) > 64 || strings.IndexFunc(w.Name, unicode.IsControl) >= 0 {
		return errInvalid("이름은 1~64자")
	}
	if w.To == "" || (s.hooks.RoleExists != nil && !s.hooks.RoleExists(ctx, w.To)) {
		return errInvalid("받는 역할 없음: " + w.To)
	}
	if err := checkURL(w.URL); err != nil {
		return err
	}
	if len(w.Headers) > 10 {
		return errInvalid("머리글은 10개까지")
	}
	names := map[string]bool{}
	for i := range w.Headers {
		h := &w.Headers[i]
		h.Name = strings.TrimSpace(h.Name)
		if !validHeaderName(h.Name) {
			return errInvalid("머리글 이름이 올바르지 않음: " + h.Name)
		}
		k := strings.ToLower(h.Name)
		if names[k] || k == "host" || k == "content-length" {
			return errInvalid("머리글을 쓸 수 없음: " + h.Name)
		}
		names[k] = true
		if strings.ContainsAny(h.Value, "\r\n\x00") || len(h.Value) > 4096 {
			return errInvalid("머리글 값이 올바르지 않음: " + h.Name)
		}
	}
	if w.Every < 1 || w.Every > 1440 {
		return errInvalid("주기는 1~1440분")
	}
	if w.Window == 0 {
		w.Window = 10
	}
	if w.Window < 1 || w.Window > 1440 {
		return errInvalid("묶는 시간은 1~1440분")
	}
	if w.FailAfter == 0 {
		w.FailAfter = 3
	}
	if w.FailAfter < 1 || w.FailAfter > 100 {
		return errInvalid("실패 알림은 1~100번")
	}
	if len([]rune(w.Subject)) > 200 {
		return errInvalid("제목 틀은 200자까지")
	}
	c := &w.Cond
	c.Path, c.List, c.Key = strings.TrimSpace(c.Path), strings.TrimSpace(c.List), strings.TrimSpace(c.Key)
	switch c.Kind {
	case wire.WatchValue:
		known := false
		for _, op := range wire.WatchOps {
			known = known || op == c.Op
		}
		if !known {
			return errInvalid("조건 비교를 고르세요")
		}
		if (c.Op == "gt" || c.Op == "lt") && !isNumber(c.Value) {
			return errInvalid("크다·작다는 숫자와 비교")
		}
		c.List, c.Key = "", ""
		if c.Op == "exists" || c.Op == "missing" {
			c.Value = ""
		}
	case wire.WatchNew:
		if c.Key == "" {
			return errInvalid("항목 키 경로를 적으세요")
		}
		c.Path, c.Op, c.Value = "", "", ""
	default:
		return errInvalid("조건 종류는 value 또는 new")
	}
	return nil
}

func isNumber(s string) bool { _, ok := number(s); return ok }

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errInvalid("주소는 http(s)://로 시작")
	}
	if u.User != nil {
		return errInvalid("주소에 계정을 넣지 말고 머리글로")
	}
	if len(raw) > 2048 {
		return errInvalid("주소가 너무 김")
	}
	return nil
}

func validHeaderName(n string) bool {
	if n == "" || len(n) > 100 {
		return false
	}
	for _, r := range n {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Create stores a new rule and its secret header values.
func (s *Service) Create(ctx context.Context, w wire.WatchRule) (wire.Watch, error) {
	if err := s.normalize(ctx, &w); err != nil {
		return wire.Watch{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	hs, puts := s.headersFor(w.Headers, nil, now)
	for _, h := range hs {
		if h.Secret && h.SetAt == "" {
			return wire.Watch{}, errInvalid("비밀 머리글 값을 적으세요: " + h.Name)
		}
	}
	if s.hooks.Secrets == nil && len(puts) > 0 {
		return wire.Watch{}, errNoSecrets
	}
	hj, _ := json.Marshal(hs)
	cj, _ := json.Marshal(w.Cond)
	var id int64
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM watches WHERE name=?`, w.Name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errConflict
		}
		res, err := tx.Exec(`INSERT INTO watches(name,to_role,url,headers,every_min,cond,subject,urgent,window_min,fail_after,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			w.Name, w.To, w.URL, string(hj), w.Every, string(cj), w.Subject, b2i(w.Urgent), w.Window, w.FailAfter, b2i(w.Enabled), now, now)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return wire.Watch{}, err
	}
	for name, v := range puts {
		if err := s.hooks.Secrets.Put(ctx, SecretRef(id, name), v); err != nil {
			_ = s.Delete(ctx, id)
			return wire.Watch{}, fmt.Errorf("secret store: %w", err)
		}
	}
	r, err := s.get(ctx, id)
	return r.Watch, err
}

var (
	errConflict  = errors.New("같은 이름의 규칙이 있음")
	errNoSecrets = errors.New("비밀 값 보관이 준비되지 않음")
)

// headersFor turns written headers into stored ones: a secret header with
// a value is to be put (returned by lower-case name), one without keeps the
// old SetAt when old has it.
func (s *Service) headersFor(in []wire.WatchHeader, old []storedHeader, now string) ([]storedHeader, map[string]string) {
	prev := map[string]storedHeader{}
	for _, h := range old {
		prev[strings.ToLower(h.Name)] = h
	}
	out := make([]storedHeader, 0, len(in))
	puts := map[string]string{}
	for _, h := range in {
		sh := storedHeader{Name: h.Name, Secret: h.Secret}
		switch {
		case !h.Secret:
			sh.Value = h.Value
		case h.Value != "":
			sh.SetAt = now
			puts[strings.ToLower(h.Name)] = h.Value
		default:
			if p, ok := prev[strings.ToLower(h.Name)]; ok && p.Secret {
				sh.SetAt = p.SetAt
			}
		}
		out = append(out, sh)
	}
	return out, puts
}

// Update replaces a rule. A changed address or condition starts it over:
// the value state, the seen keys and the failure count are cleared and it
// runs on the next pass.
func (s *Service) Update(ctx context.Context, id int64, w wire.WatchRule) (wire.Watch, error) {
	old, err := s.get(ctx, id)
	if err != nil {
		return wire.Watch{}, err
	}
	if err := s.normalize(ctx, &w); err != nil {
		return wire.Watch{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	hs, puts := s.headersFor(w.Headers, old.headers, now)
	for _, h := range hs {
		if h.Secret && h.SetAt == "" {
			return wire.Watch{}, errInvalid("비밀 머리글 값을 적으세요: " + h.Name)
		}
	}
	if s.hooks.Secrets == nil && len(puts) > 0 {
		return wire.Watch{}, errNoSecrets
	}
	for name, v := range puts {
		if err := s.hooks.Secrets.Put(ctx, SecretRef(id, name), v); err != nil {
			return wire.Watch{}, fmt.Errorf("secret store: %w", err)
		}
	}
	keep := map[string]bool{}
	for _, h := range hs {
		if h.Secret {
			keep[strings.ToLower(h.Name)] = true
		}
	}
	oc, _ := json.Marshal(old.Cond)
	hj, _ := json.Marshal(hs)
	cj, _ := json.Marshal(w.Cond)
	restart := old.URL != w.URL || string(oc) != string(cj)
	err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM watches WHERE name=? AND id<>?`, w.Name, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errConflict
		}
		if _, err := tx.Exec(`UPDATE watches SET name=?,to_role=?,url=?,headers=?,every_min=?,cond=?,subject=?,urgent=?,window_min=?,fail_after=?,enabled=?,updated_at=? WHERE id=?`,
			w.Name, w.To, w.URL, string(hj), w.Every, string(cj), w.Subject, b2i(w.Urgent), w.Window, w.FailAfter, b2i(w.Enabled), now, id); err != nil {
			return err
		}
		if old.Every != w.Every || (!old.Enabled && w.Enabled) {
			if _, err := tx.Exec(`UPDATE watches SET next_at='' WHERE id=?`, id); err != nil {
				return err
			}
		}
		if !restart {
			return nil
		}
		if _, err := tx.Exec(`UPDATE watches SET next_at='',matched=0,seeded=0,fails=0,failing=0,last_result='',last_status=0,last_ok=0,last_at='' WHERE id=?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM watch_seen WHERE watch_id=?`, id)
		return err
	})
	if err != nil {
		return wire.Watch{}, err
	}
	for _, h := range old.headers {
		if h.Secret && !keep[strings.ToLower(h.Name)] && s.hooks.Secrets != nil {
			_ = s.hooks.Secrets.Delete(ctx, SecretRef(id, h.Name))
		}
	}
	r, err := s.get(ctx, id)
	return r.Watch, err
}

// SetEnabled turns a rule on or off; on runs it on the next pass.
func (s *Service) SetEnabled(ctx context.Context, id int64, on bool) (wire.Watch, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE watches SET enabled=?,next_at='',updated_at=? WHERE id=?`, b2i(on), s.now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return wire.Watch{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return wire.Watch{}, errNotFound
	}
	r, err := s.get(ctx, id)
	return r.Watch, err
}

// Delete removes a rule, its seen keys and its secret values.
func (s *Service) Delete(ctx context.Context, id int64) error {
	old, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM watch_seen WHERE watch_id=?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM watches WHERE id=?`, id)
		return err
	})
	if err != nil {
		return err
	}
	for _, h := range old.headers {
		if h.Secret && s.hooks.Secrets != nil {
			_ = s.hooks.Secrets.Delete(ctx, SecretRef(id, h.Name))
		}
	}
	return nil
}
