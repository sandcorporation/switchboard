// Package hooks receives incoming webhooks (#527): a tool posts a Discord-
// or Slack-style notice to POST /hook/<value>, and the hook's role gets it
// as alert mail, read as text. In forward mode the server also sends the
// body on to the original destination, retrying with backoff across
// restarts, and says so in mail when it finally fails.
//
// The address value is shown once (on adding and on making it again) and
// only its hash is stored. The original destination is sealed by the secret
// store and never read back through any API: lists say only whether one is
// stored. The secret store and the collapsing alert mail are passed in
// (Hooks), so this package imports only store and wire.
package hooks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

const (
	// MaxBody is the largest body a hook takes.
	MaxBody = 256 << 10
	// PerMinute is how many notices one hook takes in a minute by default.
	PerMinute = 60
	// keepReceipts is how many receipts a hook keeps.
	keepReceipts = 100
	// listReceipts is how many the detail shows.
	listReceipts   = 50
	forwardTimeout = 10 * time.Second
)

// Backoff is the wait before each retry of sending on; its length plus one
// is the number of tries.
var Backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// SecretState is what may be said of a stored secret: whether one is set,
// when, and whether the current key still opens it.
type SecretState struct {
	Set        bool
	Unreadable bool
	At         string
}

// Secrets is the secret store (#525), values by name: Open returns "" and
// no error when none is stored, an error when one is stored that the
// current key cannot open.
type Secrets interface {
	Put(name, value string) error
	Open(name string) (string, error)
	State(name string) (SecretState, error)
	Delete(name string) error
}

// destName is the secret name of a hook's original destination.
func destName(id int64) string { return "hook:" + strconv.FormatInt(id, 10) + ":dest" }

// Alert is one alert mail (#525): notices of the same Source and Subject
// within Window collapse into one mail.
type Alert struct {
	To      string
	Source  string
	Subject string
	Body    string
	Urgent  bool
	Window  time.Duration
}

// Hooks are the effects passed in.
type Hooks struct {
	Secrets   Secrets
	Alert     func(context.Context, Alert) error
	RoleKnown func(role string) bool
	Kick      func() // run ForwardDue soon, off the request
}

// State outlives a request: the per-hook rate limit, the forward pass lock
// and the client that sends on.
type State struct {
	PerMinute int
	Client    *http.Client
	mu        sync.Mutex // one forward pass at a time
	lmu       sync.Mutex
	counts    map[int64]rate
}

type rate struct {
	minute int64
	n      int
}

// NewState builds the shared state with the defaults.
func NewState() *State {
	return &State{
		PerMinute: PerMinute,
		Client: &http.Client{
			Timeout: forwardTimeout,
			// A redirect is an answer, not a place to send the body again.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		counts: map[int64]rate{},
	}
}

// allow counts one notice of hook id in this minute.
func (st *State) allow(id int64, now time.Time) bool {
	st.lmu.Lock()
	defer st.lmu.Unlock()
	m := now.Unix() / 60
	for k, v := range st.counts {
		if v.minute != m {
			delete(st.counts, k)
		}
	}
	c := st.counts[id]
	if c.minute != m {
		c = rate{minute: m}
	}
	if c.n >= st.PerMinute {
		return false
	}
	c.n++
	st.counts[id] = c
	return true
}

// Service serves the hooks over one database.
type Service struct {
	db  *sql.DB
	now func() time.Time
	st  *State
	h   Hooks
}

// New builds the service.
func New(db *sql.DB, now func() time.Time, st *State, h Hooks) *Service {
	return &Service{db: db, now: now, st: st, h: h}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// writeJSON writes v, a value of the API (any: the encoder takes any).
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// tokenShape is an address value's shape: anything else is 404 unread.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hookRow is one stored hook.
type hookRow struct {
	id                   int64
	name, role, mode     string
	urgent               bool
	window               int
	lastAt               sql.NullString
	createdAt, updatedAt string
}

const hookCols = `id,name,role,mode,urgent,window_min,last_at,created_at,updated_at`

func scanHook(sc interface{ Scan(...any) error }) (*hookRow, error) {
	var h hookRow
	err := sc.Scan(&h.id, &h.name, &h.role, &h.mode, &h.urgent, &h.window, &h.lastAt, &h.createdAt, &h.updatedAt)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

var errNoSecrets = errors.New("no secret store")

// open is the original destination, "" when none; an error when it is
// stored but cannot be opened (another key).
func (s *Service) open(h *hookRow) (string, error) {
	if s.h.Secrets == nil {
		return "", errNoSecrets
	}
	return s.h.Secrets.Open(destName(h.id))
}

func (s *Service) wireHook(h *hookRow) wire.Hook {
	out := wire.Hook{ID: h.id, Name: h.name, Role: h.role, Mode: h.mode, Urgent: h.urgent, WindowMin: h.window,
		Dest: wire.HookDestNone, LastAt: h.lastAt.String, CreatedAt: h.createdAt, UpdatedAt: h.updatedAt}
	if s.h.Secrets == nil {
		return out
	}
	st, err := s.h.Secrets.State(destName(h.id))
	switch {
	case err != nil || st.Unreadable:
		out.Dest = wire.HookDestUnreadable
	case st.Set:
		out.Dest = wire.HookDestSet
	}
	if st.Set {
		out.DestAt = st.At
	}
	return out
}

// Receive serves POST /hook/<token>.
func (s *Service) Receive(w http.ResponseWriter, r *http.Request, token string) {
	if !tokenShape.MatchString(token) {
		http.NotFound(w, r)
		return
	}
	h, err := scanHook(s.db.QueryRow(`SELECT `+hookCols+` FROM hooks WHERE token_hash=?`, store.TokenHash(token)))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	now := s.now()
	if !s.st.allow(h.id, now) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many notices", http.StatusTooManyRequests)
		return
	}
	ct := r.Header.Get("Content-Type")
	if ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || mt != "application/json" && !strings.HasSuffix(mt, "+json") {
			http.Error(w, "send JSON", http.StatusUnsupportedMediaType)
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "could not read the body", http.StatusBadRequest)
		return
	}
	dest, destErr := s.open(h)
	n, err := Render(body, "웹훅 "+h.name, dest)
	if err != nil {
		http.Error(w, "body is not JSON", http.StatusBadRequest)
		return
	}
	forward := wire.HookForwardNone
	var tail []string
	if h.mode == wire.HookModeForward {
		switch {
		case destErr != nil:
			forward = wire.HookForwardFailed
			tail = append(tail, "원래 목적지 전송 실패: 저장된 목적지를 풀 수 없음 — 대시보드에서 다시 등록")
		case dest == "":
			forward = wire.HookForwardFailed
			tail = append(tail, "원래 목적지 전송 실패: 원래 목적지 없음")
		default:
			forward = wire.HookForwardPending
		}
	}
	text := n.Text
	if len(tail) > 0 {
		text = strings.TrimSpace(text + "\n\n" + strings.Join(tail, "\n"))
	}
	if err = s.alert(h, n.Subject, text); err != nil {
		log.Printf("hook %s: alert mail failed: %v", h.name, err)
		http.Error(w, "could not mail", http.StatusServiceUnavailable)
		return
	}
	var keep []byte
	var next any
	if forward == wire.HookForwardPending {
		keep, next = body, stamp(now)
	}
	err = store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO hook_receipts(hook_id,at,subject,forward,body,content_type,next_at) VALUES(?,?,?,?,?,?,?)`,
			h.id, stamp(now), n.Subject, forward, keep, ct, next); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hooks SET last_at=? WHERE id=?`, stamp(now), h.id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM hook_receipts WHERE hook_id=? AND forward!=? AND id NOT IN (SELECT id FROM hook_receipts WHERE hook_id=? ORDER BY id DESC LIMIT ?)`,
			h.id, wire.HookForwardPending, h.id, keepReceipts)
		return err
	})
	if err != nil {
		http.Error(w, "could not store", http.StatusInternalServerError)
		return
	}
	if forward == wire.HookForwardPending && s.h.Kick != nil {
		s.h.Kick()
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok")
}

func (s *Service) alert(h *hookRow, subject, text string) error {
	if s.h.Alert == nil {
		return errors.New("no alert mail")
	}
	return s.h.Alert(context.Background(), Alert{To: h.role, Source: "hook:" + h.name, Subject: subject,
		Body: "웹훅 " + h.name + "\n\n" + text, Urgent: h.urgent, Window: time.Duration(h.window) * time.Minute})
}

// due is one receipt waiting to be sent on.
type due struct {
	id, hookID int64
	body       []byte
	ct         string
	attempts   int
	at         string
	subject    string
}

// ForwardDue sends on every receipt whose next try is due and returns how
// many it settled (sent or failed). One pass runs at a time; a pass already
// running makes this one return at once.
func (s *Service) ForwardDue(ctx context.Context) (int, error) {
	if !s.st.mu.TryLock() {
		return 0, nil
	}
	defer s.st.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id,hook_id,body,content_type,attempts,at,subject FROM hook_receipts
		WHERE forward=? AND next_at<=? ORDER BY id LIMIT 50`, wire.HookForwardPending, stamp(s.now()))
	if err != nil {
		return 0, err
	}
	var list []due
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.id, &d.hookID, &d.body, &d.ct, &d.attempts, &d.at, &d.subject); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	settled := 0
	for _, d := range list {
		done, err := s.forwardOne(ctx, d)
		if err != nil {
			return settled, err
		}
		if done {
			settled++
		}
	}
	return settled, nil
}

func (s *Service) forwardOne(ctx context.Context, d due) (bool, error) {
	h, err := scanHook(s.db.QueryRowContext(ctx, `SELECT `+hookCols+` FROM hooks WHERE id=?`, d.hookID))
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.ExecContext(ctx, `DELETE FROM hook_receipts WHERE id=?`, d.id)
		return false, err
	}
	if err != nil {
		return false, err
	}
	d.attempts++
	dest, err := s.open(h)
	code, why := 0, ""
	switch {
	case err != nil:
		why = "저장된 목적지를 풀 수 없음 — 대시보드에서 다시 등록"
	case dest == "":
		why = "원래 목적지 없음"
	default:
		code, why = s.post(ctx, dest, d.ct, d.body)
	}
	if code/100 == 2 {
		_, err = s.db.ExecContext(ctx, `UPDATE hook_receipts SET forward=?,status=?,attempts=?,body=NULL,next_at=NULL WHERE id=?`,
			wire.HookForwardSent, code, d.attempts, d.id)
		return err == nil, err
	}
	retry := dest != "" && (code == 0 || code == http.StatusTooManyRequests || code >= 500)
	if retry && d.attempts <= len(Backoff) {
		_, err = s.db.ExecContext(ctx, `UPDATE hook_receipts SET status=?,attempts=?,next_at=? WHERE id=?`,
			code, d.attempts, stamp(s.now().Add(Backoff[d.attempts-1])), d.id)
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE hook_receipts SET forward=?,status=?,attempts=?,body=NULL,next_at=NULL WHERE id=?`,
		wire.HookForwardFailed, code, d.attempts, d.id)
	if err != nil {
		return false, err
	}
	result := why
	if code != 0 {
		result = fmt.Sprintf("상태 코드 %d", code)
	}
	body := fmt.Sprintf("받은 때 %s\n시도 %d번 · %s", d.at, d.attempts, result)
	if err := s.alert(h, clip("원래 목적지 전송 실패: "+d.subject, subjectMax), body); err != nil {
		log.Printf("hook %s: failure mail failed: %v", h.name, err)
	}
	return true, nil
}

// post sends the body on and returns the status code, or 0 and why it
// could not connect. The error text is not kept: it names the address.
func (s *Service) post(ctx context.Context, dest, ct string, body []byte) (int, string) {
	if ct == "" {
		ct = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest, strings.NewReader(string(body)))
	if err != nil {
		return 0, "목적지 주소가 올바르지 않음"
	}
	req.Header.Set("Content-Type", ct)
	resp, err := s.st.Client.Do(req)
	if err != nil {
		return 0, "연결 실패"
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return resp.StatusCode, ""
}

// List is every hook by name.
func (s *Service) List(ctx context.Context) ([]wire.Hook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+hookCols+` FROM hooks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.Hook{}
	for rows.Next() {
		h, err := scanHook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s.wireHook(h))
	}
	return out, rows.Err()
}

// RoleAPI serves GET /api/hooks to any role: names, roles, modes and the
// last notice time only.
func (s *Service) RoleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := s.List(r.Context())
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	out := make([]wire.HookSummary, 0, len(list))
	for _, h := range list {
		out = append(out, wire.HookSummary{Name: h.Name, Role: h.Role, Mode: h.Mode, LastAt: h.LastAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminAPI serves /api/admin/hooks… (rest is the path after it) for a
// caller the server has let through: GET for a login that may view, the
// writes for a signed-in login (user) only.
func (s *Service) AdminAPI(w http.ResponseWriter, r *http.Request, user, rest string) {
	idText, action, _ := strings.Cut(rest, "/")
	if idText == "" {
		switch r.Method {
		case http.MethodGet:
			list, err := s.List(r.Context())
			if err != nil {
				http.Error(w, "query failed", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, list)
		case http.MethodPost:
			s.create(w, r, user)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	id, err := wire.ParseID(idText)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	h, err := scanHook(s.db.QueryRowContext(r.Context(), `SELECT `+hookCols+` FROM hooks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.detail(w, r, h)
	case action == "" && r.Method == http.MethodPut:
		s.update(w, r, h)
	case action == "" && r.Method == http.MethodDelete:
		err = store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(`DELETE FROM hook_receipts WHERE hook_id=?`, h.id); err != nil {
				return err
			}
			_, err := tx.Exec(`DELETE FROM hooks WHERE id=?`, h.id)
			return err
		})
		if err == nil && s.h.Secrets != nil {
			err = s.h.Secrets.Delete(destName(h.id))
		}
		if err != nil {
			http.Error(w, "could not delete", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	case action == "address" && r.Method == http.MethodPost:
		token, err := newToken()
		if err != nil {
			http.Error(w, "could not make an address", http.StatusInternalServerError)
			return
		}
		if _, err = s.db.ExecContext(r.Context(), `UPDATE hooks SET token_hash=?,updated_at=? WHERE id=?`, store.TokenHash(token), stamp(s.now()), h.id); err != nil {
			http.Error(w, "could not store", http.StatusInternalServerError)
			return
		}
		s.writeCreated(w, r, h.id, token, http.StatusOK)
	case action == "" || action == "address":
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) detail(w http.ResponseWriter, r *http.Request, h *hookRow) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT at,subject,forward,status,attempts FROM hook_receipts WHERE hook_id=? ORDER BY id DESC LIMIT ?`, h.id, listReceipts)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	d := wire.HookDetail{Hook: s.wireHook(h), Receipts: []wire.HookReceipt{}}
	for rows.Next() {
		var rc wire.HookReceipt
		if err = rows.Scan(&rc.At, &rc.Subject, &rc.Forward, &rc.Status, &rc.Attempts); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		d.Receipts = append(d.Receipts, rc)
	}
	writeJSON(w, http.StatusOK, d)
}

// decodeSave reads and checks a HookSave, and that its role exists.
func (s *Service) decodeSave(w http.ResponseWriter, r *http.Request) (wire.HookSave, bool) {
	var v wire.HookSave
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return v, false
	}
	if err := v.Check(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return v, false
	}
	if s.h.RoleKnown != nil && !s.h.RoleKnown(v.Role) {
		http.Error(w, "unknown role "+v.Role, http.StatusBadRequest)
		return v, false
	}
	return v, true
}

func (s *Service) create(w http.ResponseWriter, r *http.Request, user string) {
	v, ok := s.decodeSave(w, r)
	if !ok {
		return
	}
	if v.Mode == wire.HookModeForward && v.Dest == "" {
		http.Error(w, "forward needs the original destination", http.StatusBadRequest)
		return
	}
	if v.Dest != "" && s.h.Secrets == nil {
		http.Error(w, "no secret store", http.StatusServiceUnavailable)
		return
	}
	now := stamp(s.now())
	token, err := newToken()
	if err != nil {
		http.Error(w, "could not make an address", http.StatusInternalServerError)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO hooks(name,role,mode,urgent,window_min,token_hash,created_at,created_by,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, v.Name, v.Role, v.Mode, v.Urgent, v.WindowMin, store.TokenHash(token), now, user, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "a hook named "+v.Name+" exists", http.StatusConflict)
			return
		}
		http.Error(w, "could not store", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	if v.Dest != "" {
		if err = s.h.Secrets.Put(destName(id), v.Dest); err != nil {
			_, _ = s.db.ExecContext(r.Context(), `DELETE FROM hooks WHERE id=?`, id)
			http.Error(w, "could not store the destination", http.StatusInternalServerError)
			return
		}
	}
	s.writeCreated(w, r, id, token, http.StatusCreated)
}

func (s *Service) writeCreated(w http.ResponseWriter, r *http.Request, id int64, token string, code int) {
	h, err := scanHook(s.db.QueryRowContext(r.Context(), `SELECT `+hookCols+` FROM hooks WHERE id=?`, id))
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, code, wire.HookCreated{Hook: s.wireHook(h), Token: token})
}

func (s *Service) update(w http.ResponseWriter, r *http.Request, h *hookRow) {
	v, ok := s.decodeSave(w, r)
	if !ok {
		return
	}
	had := false
	if s.h.Secrets != nil {
		st, err := s.h.Secrets.State(destName(h.id))
		had = err == nil && st.Set
	}
	if v.Mode == wire.HookModeForward && !(had && !v.ClearDest || v.Dest != "") {
		http.Error(w, "forward needs the original destination", http.StatusBadRequest)
		return
	}
	if (v.Dest != "" || v.ClearDest) && s.h.Secrets == nil {
		http.Error(w, "no secret store", http.StatusServiceUnavailable)
		return
	}
	_, err := s.db.ExecContext(r.Context(), `UPDATE hooks SET name=?,role=?,mode=?,urgent=?,window_min=?,updated_at=? WHERE id=?`,
		v.Name, v.Role, v.Mode, v.Urgent, v.WindowMin, stamp(s.now()), h.id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "a hook named "+v.Name+" exists", http.StatusConflict)
			return
		}
		http.Error(w, "could not store", http.StatusInternalServerError)
		return
	}
	switch {
	case v.Dest != "":
		err = s.h.Secrets.Put(destName(h.id), v.Dest)
	case v.ClearDest:
		err = s.h.Secrets.Delete(destName(h.id))
	}
	if err != nil {
		http.Error(w, "could not store the destination", http.StatusInternalServerError)
		return
	}
	n, err := scanHook(s.db.QueryRowContext(r.Context(), `SELECT `+hookCols+` FROM hooks WHERE id=?`, h.id))
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, s.wireHook(n))
}
