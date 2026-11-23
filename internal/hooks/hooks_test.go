package hooks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// fakeSecrets keeps values by name; rekey makes every stored value
// unreadable, as a changed store key does.
type fakeSecrets struct {
	vals  map[string]string
	stale bool
}

func (f *fakeSecrets) Put(name, value string) error { f.vals[name] = value; return nil }
func (f *fakeSecrets) Delete(name string) error     { delete(f.vals, name); return nil }

func (f *fakeSecrets) Open(name string) (string, error) {
	v, ok := f.vals[name]
	if ok && f.stale {
		return "", errors.New("unreadable")
	}
	return v, nil
}

func (f *fakeSecrets) State(name string) (SecretState, error) {
	_, ok := f.vals[name]
	return SecretState{Set: ok, Unreadable: ok && f.stale, At: "2026-11-23T09:00:00Z"}, nil
}

type fixture struct {
	t      *testing.T
	db     *sql.DB
	clock  time.Time
	st     *State
	sec    *fakeSecrets
	mu     sync.Mutex
	alerts []Alert
	kicks  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &fixture{t: t, db: db, clock: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC), st: NewState(), sec: &fakeSecrets{vals: map[string]string{}}}
}

func (f *fixture) svc() *Service {
	return New(f.db, func() time.Time { return f.clock }, f.st, Hooks{
		Secrets: f.sec,
		Alert: func(_ context.Context, a Alert) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.alerts = append(f.alerts, a)
			return nil
		},
		RoleKnown: func(role string) bool { return role != "team/nobody" },
		Kick:      func() { f.kicks++ },
	})
}

// admin calls the admin API and returns the status and body.
func (f *fixture) admin(method, rest string, body any) (int, []byte) {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	rec := httptest.NewRecorder()
	f.svc().AdminAPI(rec, httptest.NewRequest(method, "/api/admin/hooks/"+rest, rd), "owner", rest)
	return rec.Code, rec.Body.Bytes()
}

func (f *fixture) add(v wire.HookSave) wire.HookCreated {
	f.t.Helper()
	code, b := f.admin("POST", "", v)
	if code != http.StatusCreated {
		f.t.Fatalf("add: %d %s", code, b)
	}
	var c wire.HookCreated
	if err := json.Unmarshal(b, &c); err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fixture) post(token, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/hook/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	f.svc().Receive(rec, req, token)
	return rec
}

const discordSample = `{"username":"monitor","embeds":[{"title":"CPU high on web-1","description":"load 9.5 for 5m","fields":[{"name":"host","value":"web-1"}]}]}`
const slackSample = `{"text":"Deploy finished","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*build 42* passed"}}],"attachments":[{"title":"details","fields":[{"title":"branch","value":"main"}]}]}`

func TestDiscordAndSlackMailTheRole(t *testing.T) {
	f := newFixture(t)
	d := f.add(wire.HookSave{Name: "monitor", Role: "team/boss", Mode: wire.HookModeMail, Urgent: true})
	s := f.add(wire.HookSave{Name: "deploys", Role: "team/worker", Mode: wire.HookModeMail})
	if rec := f.post(d.Token, discordSample); rec.Code != 200 {
		t.Fatalf("discord: %d %s", rec.Code, rec.Body)
	}
	if rec := f.post(s.Token, slackSample); rec.Code != 200 {
		t.Fatalf("slack: %d %s", rec.Code, rec.Body)
	}
	if len(f.alerts) != 2 {
		t.Fatalf("alerts = %+v", f.alerts)
	}
	a := f.alerts[0]
	if a.To != "team/boss" || a.Source != "hook:monitor" || a.Subject != "CPU high on web-1" || !a.Urgent || a.Window != 10*time.Minute {
		t.Errorf("discord alert = %+v", a)
	}
	for _, want := range []string{"load 9.5 for 5m", "host: web-1"} {
		if !strings.Contains(a.Body, want) {
			t.Errorf("discord body lacks %q:\n%s", want, a.Body)
		}
	}
	b := f.alerts[1]
	if b.To != "team/worker" || b.Subject != "Deploy finished" || b.Urgent {
		t.Errorf("slack alert = %+v", b)
	}
	for _, want := range []string{"*build 42* passed", "branch: main", "details"} {
		if !strings.Contains(b.Body, want) {
			t.Errorf("slack body lacks %q:\n%s", want, b.Body)
		}
	}
	for _, a := range f.alerts {
		if strings.Contains(a.Body, "{") {
			t.Errorf("raw JSON in mail:\n%s", a.Body)
		}
	}
	if f.kicks != 0 {
		t.Errorf("mail mode kicked the forwarder")
	}
}

func TestOtherJSONShowsTopKeys(t *testing.T) {
	n, err := Render([]byte(`{"status":"firing","count":3,"labels":{"a":"b"},"k4":1,"k5":1,"k6":1,"k7":1,"k8":1,"k9":1}`), "웹훅 x")
	if err != nil {
		t.Fatal(err)
	}
	want := "status: firing\ncount: 3\nlabels: {\"a\":\"b\"}"
	if !strings.HasPrefix(n.Text, want) || !strings.Contains(n.Text, "(키 1개 더)") || n.Subject != "status: firing" {
		t.Errorf("notice = %+v", n)
	}
	if _, err := Render([]byte("not json"), "x"); err == nil {
		t.Error("non-JSON rendered")
	}
}

func TestMailHidesTheDestination(t *testing.T) {
	f := newFixture(t)
	dest := "https://hooks.example/services/T000/B000/secretpart"
	c := f.add(wire.HookSave{Name: "x", Role: "team/boss", Mode: wire.HookModeMail, Dest: dest})
	f.post(c.Token, `{"content":"sent to `+dest+` and https://chat.example/api/webhooks/1/abc"}`)
	if len(f.alerts) != 1 {
		t.Fatal(f.alerts)
	}
	if body := f.alerts[0].Body + f.alerts[0].Subject; strings.Contains(body, "secretpart") || strings.Contains(body, "webhooks/1/abc") {
		t.Errorf("mail carries an address:\n%s", body)
	}
}

func TestForwardSendsTheSameBody(t *testing.T) {
	var got []byte
	var gotType string
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		gotType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dst.Close()
	f := newFixture(t)
	c := f.add(wire.HookSave{Name: "alerts", Role: "team/boss", Mode: wire.HookModeForward, Dest: dst.URL + "/api/webhooks/1/x"})
	f.post(c.Token, discordSample)
	if f.kicks != 1 {
		t.Errorf("kicks = %d", f.kicks)
	}
	n, err := f.svc().ForwardDue(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("ForwardDue = %d, %v", n, err)
	}
	if string(got) != discordSample || gotType != "application/json" {
		t.Errorf("destination got %q (%s)", got, gotType)
	}
	_, b := f.admin("GET", itoa(c.ID), nil)
	var d wire.HookDetail
	_ = json.Unmarshal(b, &d)
	if len(d.Receipts) != 1 || d.Receipts[0].Forward != wire.HookForwardSent || d.Receipts[0].Status != 204 {
		t.Errorf("receipts = %+v", d.Receipts)
	}
	var left int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM hook_receipts WHERE body IS NOT NULL`).Scan(&left)
	if left != 0 {
		t.Errorf("%d bodies kept after sending", left)
	}
}

func TestForwardRetriesThenMailsTheFailure(t *testing.T) {
	tries := 0
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dst.Close()
	f := newFixture(t)
	c := f.add(wire.HookSave{Name: "alerts", Role: "team/boss", Mode: wire.HookModeForward, Dest: dst.URL})
	f.post(c.Token, discordSample)
	ctx := context.Background()
	for i := 0; i <= len(Backoff); i++ {
		// A pass before the next try is due sends nothing.
		if i > 0 {
			f.clock = f.clock.Add(Backoff[i-1] - time.Second)
			if _, err := f.svc().ForwardDue(ctx); err != nil {
				t.Fatal(err)
			}
			if tries != i {
				t.Fatalf("tried early: %d tries before try %d", tries, i+1)
			}
			f.clock = f.clock.Add(time.Second)
		}
		// A new service each pass: the retry state lives in the database,
		// so a restarted server carries on.
		f.st = NewState()
		if _, err := f.svc().ForwardDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if tries != len(Backoff)+1 {
		t.Errorf("tries = %d", tries)
	}
	if len(f.alerts) != 2 {
		t.Fatalf("alerts = %+v", f.alerts)
	}
	fail := f.alerts[1]
	if fail.Subject != "원래 목적지 전송 실패: CPU high on web-1" || fail.Source != "hook:alerts" || fail.To != "team/boss" {
		t.Errorf("failure alert = %+v", fail)
	}
	if !strings.Contains(fail.Body, "상태 코드 500") || !strings.Contains(fail.Body, "시도 5번") || strings.Contains(fail.Body, dst.URL) {
		t.Errorf("failure body:\n%s", fail.Body)
	}
	f.clock = f.clock.Add(time.Hour)
	if _, err := f.svc().ForwardDue(ctx); err != nil || tries != len(Backoff)+1 {
		t.Errorf("tried after failing: %d, %v", tries, err)
	}
}

func TestForwardWithAnUnreadableDestinationSaysSo(t *testing.T) {
	f := newFixture(t)
	c := f.add(wire.HookSave{Name: "alerts", Role: "team/boss", Mode: wire.HookModeForward, Dest: "https://hooks.example/a"})
	f.sec.stale = true
	f.post(c.Token, discordSample)
	if len(f.alerts) != 1 || !strings.Contains(f.alerts[0].Body, "원래 목적지 전송 실패") {
		t.Fatalf("alerts = %+v", f.alerts)
	}
	_, b := f.admin("GET", "", nil)
	var list []wire.Hook
	_ = json.Unmarshal(b, &list)
	if len(list) != 1 || list[0].Dest != wire.HookDestUnreadable {
		t.Errorf("list = %+v", list)
	}
}

func TestAddressAndDestinationNeverReadBack(t *testing.T) {
	f := newFixture(t)
	dest := "https://hooks.example/services/T0/B0/destvalue"
	c := f.add(wire.HookSave{Name: "alerts", Role: "team/boss", Mode: wire.HookModeForward, Dest: dest})
	if c.Token == "" || c.Dest != wire.HookDestSet {
		t.Fatalf("created = %+v", c)
	}
	f.post(c.Token, discordSample)
	var all [][]byte
	_, b := f.admin("GET", "", nil)
	all = append(all, b)
	_, b = f.admin("GET", itoa(c.ID), nil)
	all = append(all, b)
	_, b = f.admin("PUT", itoa(c.ID), wire.HookSave{Name: "alerts", Role: "team/boss", Mode: wire.HookModeForward, WindowMin: 5})
	all = append(all, b)
	rec := httptest.NewRecorder()
	f.svc().RoleAPI(rec, httptest.NewRequest("GET", "/api/hooks", nil))
	all = append(all, rec.Body.Bytes())
	for _, b := range all {
		if bytes.Contains(b, []byte(c.Token)) || bytes.Contains(b, []byte("destvalue")) || bytes.Contains(b, []byte(store.TokenHash(c.Token))) {
			t.Errorf("response carries a secret: %s", b)
		}
	}
	var sum []wire.HookSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil || len(sum) != 1 || sum[0].Name != "alerts" || sum[0].LastAt == "" {
		t.Errorf("role list = %s", rec.Body)
	}
	if f.sec.vals[destName(c.ID)] != dest {
		t.Error("destination not in the secret store")
	}
	if code, _ := f.admin("DELETE", itoa(c.ID), nil); code != 200 || len(f.sec.vals) != 0 {
		t.Errorf("delete left the destination: %d %v", code, len(f.sec.vals))
	}
}

func TestAddressRemakeAndDelete(t *testing.T) {
	f := newFixture(t)
	c := f.add(wire.HookSave{Name: "x", Role: "team/boss", Mode: wire.HookModeMail})
	code, b := f.admin("POST", itoa(c.ID)+"/address", nil)
	var again wire.HookCreated
	_ = json.Unmarshal(b, &again)
	if code != 200 || again.Token == "" || again.Token == c.Token {
		t.Fatalf("remake = %d %s", code, b)
	}
	if rec := f.post(c.Token, discordSample); rec.Code != 404 {
		t.Errorf("old address = %d", rec.Code)
	}
	if rec := f.post(again.Token, discordSample); rec.Code != 200 {
		t.Errorf("new address = %d", rec.Code)
	}
	if code, _ := f.admin("DELETE", itoa(c.ID), nil); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if rec := f.post(again.Token, discordSample); rec.Code != 404 {
		t.Errorf("deleted hook = %d", rec.Code)
	}
	if rec := f.post("short", discordSample); rec.Code != 404 {
		t.Errorf("malformed value = %d", rec.Code)
	}
}

func TestSaveChecks(t *testing.T) {
	f := newFixture(t)
	for _, v := range []wire.HookSave{
		{Name: "x", Role: "team/boss", Mode: wire.HookModeForward},
		{Name: "x", Role: "team/nobody", Mode: wire.HookModeMail},
		{Name: "x", Role: "team/boss", Mode: "other"},
		{Name: "x", Role: "team/boss", Mode: wire.HookModeMail, WindowMin: 2000},
		{Name: "x", Role: "team/boss", Mode: wire.HookModeMail, Dest: "ftp://hooks.example/a"},
	} {
		if code, b := f.admin("POST", "", v); code != 400 {
			t.Errorf("%+v: %d %s", v, code, b)
		}
	}
	f.add(wire.HookSave{Name: "x", Role: "team/boss", Mode: wire.HookModeMail})
	if code, _ := f.admin("POST", "", wire.HookSave{Name: "x", Role: "team/boss", Mode: wire.HookModeMail}); code != 409 {
		t.Errorf("duplicate name = %d", code)
	}
}

func TestBodyAndRateLimits(t *testing.T) {
	f := newFixture(t)
	f.st.PerMinute = 2
	c := f.add(wire.HookSave{Name: "x", Role: "team/boss", Mode: wire.HookModeMail})
	big := `{"content":"` + strings.Repeat("a", MaxBody) + `"}`
	if rec := f.post(c.Token, big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big body = %d", rec.Code)
	}
	f.post(c.Token, discordSample)
	if rec := f.post(c.Token, discordSample); rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the rate = %d", rec.Code)
	}
	f.clock = f.clock.Add(time.Minute)
	if rec := f.post(c.Token, discordSample); rec.Code != 200 {
		t.Errorf("next minute = %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/hook/"+c.Token, strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	f.svc().Receive(rec, req, c.Token)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("form body = %d", rec.Code)
	}
}

func itoa(id int64) string { b, _ := json.Marshal(id); return string(b) }
