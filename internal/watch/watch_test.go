package watch

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
	_ "modernc.org/sqlite"
)

const apiKey = "key-0123456789-sQ7zX"

type fakeSecrets struct {
	mu   sync.Mutex
	m    map[string]string
	lost map[string]bool
}

func (f *fakeSecrets) Put(_ context.Context, ref, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[ref] = v
	delete(f.lost, ref)
	return nil
}
func (f *fakeSecrets) Get(_ context.Context, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lost[ref] {
		return "", ErrSecretLost
	}
	v, ok := f.m[ref]
	if !ok {
		return "", ErrSecretLost
	}
	return v, nil
}
func (f *fakeSecrets) Info(_ context.Context, ref string) (wire.SecretInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[ref]
	if !ok {
		return wire.SecretInfo{}, nil
	}
	return wire.SecretInfo{Set: true, Hint: "…" + v[len(v)-4:], Unreadable: f.lost[ref]}, nil
}
func (f *fakeSecrets) Delete(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, ref)
	return nil
}

// fakeAPI answers with body (a JSON text) or code; it refuses a request
// without the key.
type fakeAPI struct {
	mu   sync.Mutex
	body string
	code int
	hits int
}

func (a *fakeAPI) set(code int, body string) { a.mu.Lock(); a.code, a.body = code, body; a.mu.Unlock() }

func (a *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hits++
	if r.Header.Get("X-Api-Key") != apiKey {
		http.Error(w, "no key", http.StatusUnauthorized)
		return
	}
	if a.code != 200 {
		// An API that echoes the key in its error page must not leak it.
		http.Error(w, "bad request with key "+apiKey, a.code)
		return
	}
	_, _ = w.Write([]byte(a.body))
}

type rig struct {
	db      *sql.DB
	now     time.Time
	secrets *fakeSecrets
	mu      sync.Mutex
	notes   []Notice
	api     *fakeAPI
	srv     *httptest.Server
	logs    *bytes.Buffer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "w.db")+"?_pragma=synchronous(OFF)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := store.Init(db); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{code: 200, body: `{}`}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return &rig{db: db, now: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC), secrets: &fakeSecrets{m: map[string]string{}, lost: map[string]bool{}}, api: api, srv: srv, logs: &bytes.Buffer{}}
}

// svc is a fresh service on the rig's database, as after a restart.
func (g *rig) svc() *Service {
	return New(g.db, func() time.Time { return g.now }, Limits{Timeout: 2 * time.Second, MaxBody: 4096, Parallel: 2, MaxNew: 3, SeenCap: 50}, Hooks{
		Secrets:    g.secrets,
		RoleExists: func(_ context.Context, role string) bool { return role == "team/worker" },
		Notify: func(_ context.Context, n Notice) error {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.notes = append(g.notes, n)
			return nil
		},
	})
}

func (g *rig) taken() []Notice {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.notes
	g.notes = nil
	return out
}

// tick moves the clock a period ahead and runs the due rules.
func (g *rig) tick(t *testing.T) {
	t.Helper()
	g.now = g.now.Add(time.Minute)
	if _, err := g.svc().RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (g *rig) rule(cond wire.WatchCond) wire.WatchRule {
	return wire.WatchRule{Name: "orders", To: "team/worker", URL: g.srv.URL + "/v1/orders?region=eu", Every: 1, Cond: cond, Enabled: true,
		Headers: []wire.WatchHeader{{Name: "X-Api-Key", Value: apiKey, Secret: true}, {Name: "Accept-Language", Value: "ko"}}}
}

func (g *rig) create(t *testing.T, w wire.WatchRule) wire.Watch {
	t.Helper()
	out, err := g.svc().Create(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func subjects(ns []Notice) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Subject)
	}
	return out
}

func TestValueRuleMailsOnceWhenItStartsToHold(t *testing.T) {
	g := newRig(t)
	g.api.set(200, `{"data":{"count":2}}`)
	w := g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "data.count", Op: "gt", Value: "5"})
	w.Subject = "주문 {data.count}건"
	w.Urgent = true
	g.create(t, w)

	g.tick(t)
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("no match, mail %v", subjects(n))
	}
	g.api.set(200, `{"data":{"count":7}}`)
	g.tick(t)
	n := g.taken()
	if len(n) != 1 || n[0].Subject != "주문 7건" || !n[0].Urgent || n[0].To != "team/worker" || n[0].Source != "watch:orders" || n[0].Window != 10*time.Minute {
		t.Fatalf("match: %+v", n)
	}
	if !strings.Contains(n[0].Body, "data.count > 5") || !strings.Contains(n[0].Body, "값: 7") || strings.Contains(n[0].Body, "region=eu") {
		t.Fatalf("body: %q", n[0].Body)
	}
	g.api.set(200, `{"data":{"count":9}}`)
	g.tick(t)
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("still holding, mail again: %v", subjects(n))
	}
	g.api.set(200, `{"data":{"count":1}}`)
	g.tick(t)
	g.api.set(200, `{"data":{"count":8}}`)
	g.tick(t)
	if n := g.taken(); len(n) != 1 || n[0].Subject != "주문 8건" {
		t.Fatalf("holds again after not: %v", subjects(n))
	}
}

func TestRuleRunsOnItsPeriod(t *testing.T) {
	g := newRig(t)
	w := g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "x", Op: "exists"})
	w.Every = 5
	g.create(t, w)
	for i := 0; i < 10; i++ {
		g.tick(t)
	}
	if g.api.hits != 2 {
		t.Fatalf("10 minutes at a 5-minute period: %d fetches", g.api.hits)
	}
}

func TestNewItemRuleOneMailPerKeyAcrossRestart(t *testing.T) {
	g := newRig(t)
	g.api.set(200, `{"results":[{"id":1,"title":"a"},{"id":2,"title":"b"}]}`)
	w := g.rule(wire.WatchCond{Kind: wire.WatchNew, List: "results", Key: "id"})
	w.Subject = "새 글 {title}"
	g.create(t, w)

	g.tick(t)
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("baseline mailed: %v", subjects(n))
	}
	g.api.set(200, `{"results":[{"id":3,"title":"c","body":{"long":"x"}},{"id":1,"title":"a"},{"id":2,"title":"b"}]}`)
	g.tick(t)
	n := g.taken()
	if len(n) != 1 || n[0].Subject != "새 글 c" || !strings.Contains(n[0].Body, "키: 3") || !strings.Contains(n[0].Body, "title: c") || strings.Contains(n[0].Body, "long") {
		t.Fatalf("new key: %+v", n)
	}
	// A restart is a new service on the same database.
	g.tick(t)
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("same key again: %v", subjects(n))
	}
	list, _ := g.svc().List(context.Background())
	if list[0].Seen != 3 || list[0].LastResult != "새 항목 0" {
		t.Fatalf("state: %+v", list[0])
	}
}

func TestNewItemRuleCapsMailsAndKeys(t *testing.T) {
	g := newRig(t)
	g.api.set(200, `{"results":[]}`)
	g.create(t, g.rule(wire.WatchCond{Kind: wire.WatchNew, List: "results", Key: "id"}))
	g.tick(t)
	var items []string
	for i := 0; i < 60; i++ {
		items = append(items, fmt.Sprintf(`{"id":%d}`, i))
	}
	g.api.set(200, `{"results":[`+strings.Join(items, ",")+`]}`)
	g.tick(t)
	n := g.taken()
	if len(n) != 4 || n[3].Subject != "[지켜보기] orders 새 항목 57개 더" || n[0].Subject != "[지켜보기] orders 새 항목 0" {
		t.Fatalf("capped mails: %v", subjects(n))
	}
	list, _ := g.svc().List(context.Background())
	if list[0].Seen != 50 {
		t.Fatalf("kept keys: %d", list[0].Seen)
	}
}

func TestFailuresMailOnceThenRecovery(t *testing.T) {
	g := newRig(t)
	g.api.set(200, `{"ok":true}`)
	g.create(t, g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "ok", Op: "eq", Value: "false"}))
	g.tick(t)
	g.api.set(503, "")
	g.tick(t)
	g.tick(t)
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("below 3 failures: %v", subjects(n))
	}
	g.tick(t)
	n := g.taken()
	if len(n) != 1 || n[0].Subject != "[지켜보기] orders 조회 실패" || !strings.Contains(n[0].Body, "HTTP 503") || n[0].Urgent {
		t.Fatalf("failure: %+v", n)
	}
	g.tick(t)
	g.api.set(200, `not json`)
	g.tick(t)
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("still failing: %v", subjects(n))
	}
	list, _ := g.svc().List(context.Background())
	if list[0].LastResult != "실패: JSON 아님" || !list[0].Failing {
		t.Fatalf("state: %+v", list[0])
	}
	g.api.set(200, `{"ok":true}`)
	g.tick(t)
	g.tick(t)
	if n := g.taken(); len(n) != 1 || n[0].Subject != "[지켜보기] orders 다시 됨" {
		t.Fatalf("recovery: %v", subjects(n))
	}
}

func TestTestFetchSendsNothingAndStoresNothing(t *testing.T) {
	g := newRig(t)
	g.api.set(200, `{"results":[{"id":1},{"id":2}],"data":{"count":7}}`)
	w := g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "data.count", Op: "gt", Value: "5"})
	got, err := g.svc().Test(context.Background(), 0, w)
	if err != nil || !got.OK || got.Status != 200 || !got.Matched || got.Value != "7" {
		t.Fatalf("value test: %+v %v", got, err)
	}
	stored := g.create(t, g.rule(wire.WatchCond{Kind: wire.WatchNew, List: "results", Key: "id"}))
	g.tick(t)
	g.api.set(200, `{"results":[{"id":1},{"id":2},{"id":3}]}`)
	nw := g.rule(wire.WatchCond{Kind: wire.WatchNew, List: "results", Key: "id"})
	nw.Headers[0].Value = "" // the stored key
	got, err = g.svc().Test(context.Background(), stored.ID, nw)
	if err != nil || !got.OK || got.New != 1 || got.Items != 3 {
		t.Fatalf("new test: %+v %v", got, err)
	}
	g.now = g.now.Add(10 * time.Minute)
	g.taken()
	if _, err := g.svc().Test(context.Background(), stored.ID, nw); err != nil {
		t.Fatal(err)
	}
	if n := g.taken(); len(n) != 0 {
		t.Fatalf("test mailed: %v", subjects(n))
	}
	list, _ := g.svc().List(context.Background())
	if list[0].Seen != 2 {
		t.Fatalf("test stored keys: %d", list[0].Seen)
	}
}

func TestKeyNeverShown(t *testing.T) {
	g := newRig(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(nil)
	g.api.set(400, "")
	w := g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "x", Op: "exists"})
	w.FailAfter = 1
	created := g.create(t, w)
	g.tick(t)
	got, _ := g.svc().Test(context.Background(), 0, w)
	// A dead address: the network error names the address.
	g.srv.Close()
	got2, _ := g.svc().Test(context.Background(), 0, w)
	list, _ := g.svc().List(context.Background())
	notes := g.taken()
	if len(notes) != 1 {
		t.Fatalf("notes: %v", subjects(notes))
	}
	everything, _ := json.Marshal([]any{created, got, got2, list, notes})
	// The rule's own address (with its query) is the login's to read; the
	// errors, results, mail and logs carry neither the query nor the key.
	told, _ := json.Marshal([]any{got, got2, list[0].LastResult, notes})
	for _, s := range []string{string(everything), string(told), logs.String()} {
		// Not even in part: the store's hint ("…cret") is not passed on.
		if strings.Contains(s, apiKey) || strings.Contains(s, apiKey[len(apiKey)-4:]) || strings.Contains(s, `"hint"`) {
			t.Fatalf("key shown: %s", s)
		}
	}
	for _, s := range []string{string(told), logs.String()} {
		if strings.Contains(s, "region=eu") {
			t.Fatalf("query shown: %s", s)
		}
	}
	if !strings.Contains(got2.Error, "연결 실패") || !strings.Contains(string(everything), `"set":true`) {
		t.Fatalf("result: %+v / %s", got2, everything)
	}
	var stored string
	_ = g.db.QueryRow(`SELECT headers FROM watches`).Scan(&stored)
	if strings.Contains(stored, apiKey) {
		t.Fatal("key in watches row")
	}
}

func TestLostSecretAndUpdateKeepsStoredKey(t *testing.T) {
	g := newRig(t)
	g.api.set(200, `{"x":1}`)
	w := g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "x", Op: "exists"})
	created := g.create(t, w)
	w.Headers[0].Value = ""
	w.Every = 2
	if _, err := g.svc().Update(context.Background(), created.ID, w); err != nil {
		t.Fatal(err)
	}
	g.tick(t)
	list, _ := g.svc().List(context.Background())
	if !list[0].LastOK {
		t.Fatalf("kept key: %+v", list[0])
	}
	g.secrets.lost[SecretRef(created.ID, "X-Api-Key")] = true
	g.tick(t)
	g.tick(t)
	list, _ = g.svc().List(context.Background())
	if list[0].LastOK || list[0].LastResult != "실패: 머리글 X-Api-Key 키 다시 등록 필요" {
		t.Fatalf("lost key: %+v", list[0])
	}
	if err := g.svc().Delete(context.Background(), created.ID); err != nil || len(g.secrets.m) != 0 {
		t.Fatalf("delete left secrets: %v %v", err, g.secrets.m)
	}
}

func TestRefusedRules(t *testing.T) {
	g := newRig(t)
	ok := g.rule(wire.WatchCond{Kind: wire.WatchValue, Path: "x", Op: "exists"})
	for name, change := range map[string]func(*wire.WatchRule){
		"role":     func(w *wire.WatchRule) { w.To = "team/nobody" },
		"scheme":   func(w *wire.WatchRule) { w.URL = "ftp://api.example/x" },
		"userinfo": func(w *wire.WatchRule) { w.URL = "https://u:p@api.example/x" },
		"period":   func(w *wire.WatchRule) { w.Every = 0 },
		"op":       func(w *wire.WatchRule) { w.Cond.Op = "like" },
		"gt text":  func(w *wire.WatchRule) { w.Cond.Op, w.Cond.Value = "gt", "abc" },
		"no key":   func(w *wire.WatchRule) { w.Cond = wire.WatchCond{Kind: wire.WatchNew, List: "x"} },
		"header":   func(w *wire.WatchRule) { w.Headers[1].Name = "Bad Name" },
		"newline":  func(w *wire.WatchRule) { w.Headers[1].Value = "a\r\nb" },
		"secret":   func(w *wire.WatchRule) { w.Headers[0].Value = "" },
	} {
		w := ok
		w.Headers = append([]wire.WatchHeader(nil), ok.Headers...)
		change(&w)
		if _, err := g.svc().Create(context.Background(), w); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	g.create(t, ok)
	if _, err := g.svc().Create(context.Background(), ok); err != errConflict {
		t.Fatalf("same name: %v", err)
	}
}

func TestHolds(t *testing.T) {
	body, _ := decode([]byte(`{"data":{"count":10,"name":"build ok","tags":["a","b"]},"items":[{"status":"open"}],"none":null}`))
	for _, c := range []struct {
		path, op, value string
		want            bool
	}{
		{"data.count", "eq", "10.0", true},
		{"data.count", "ne", "10", false},
		{"data.count", "gt", "9", true},
		{"data.count", "lt", "9", false},
		{"items.0.status", "eq", "open", true},
		{"items.1.status", "exists", "", false},
		{"none", "missing", "", true},
		{"data.name", "contains", "ok", true},
		{"data.tags", "contains", "b", true},
		{"data", "contains", "count", true},
		{"nope", "ne", "x", false},
	} {
		got, _ := holds(wire.WatchCond{Kind: wire.WatchValue, Path: c.path, Op: c.op, Value: c.value}, body)
		if got != c.want {
			t.Errorf("%s %s %s = %v", c.path, c.op, c.value, got)
		}
	}
	if s := render("{data.name} · {data.count} · {nope}", body); s != "build ok · 10 · ?" {
		t.Fatalf("render: %q", s)
	}
}
