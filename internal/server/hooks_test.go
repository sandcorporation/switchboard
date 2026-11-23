package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/hooks"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// postHook posts body to /hook/<token> through the server's handler, with
// no sign-in: the address value is the key (#527).
func postHook(s *Server, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/hook/"+token, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}

func addHook(t *testing.T, s *Server, v wire.HookSave) wire.HookCreated {
	t.Helper()
	b, _ := json.Marshal(v)
	w := adminWrite(s, "POST", "/api/admin/hooks", string(b))
	if w.Code != 201 {
		t.Fatalf("add hook=%d %s", w.Code, w.Body)
	}
	var c wire.HookCreated
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func inboxAlerts(t *testing.T, s *Server, token string) []wire.Message {
	t.Helper()
	w := call(s, "GET", "/api/messages?box=inbox", token, nil, "")
	var inbox []wire.Message
	if err := json.Unmarshal(w.Body.Bytes(), &inbox); err != nil {
		t.Fatal(w.Body.String())
	}
	var out []wire.Message
	for _, m := range inbox {
		if strings.HasPrefix(m.AlertSource, "hook:") {
			out = append(out, m)
		}
	}
	return out
}

// Discord- and Slack-style notices reach the hook's role as alert mail;
// three of the same inside the window are one mail ×3; a forward hook's
// body reaches the original destination unchanged.
func TestHookNoticesBecomeRoleMail(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.admin.ProxyToken = "proxy-secret"
	var mu sync.Mutex
	var got []string
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dst.Close()
	d := addHook(t, s, wire.HookSave{Name: "monitor", Role: "b", Mode: wire.HookModeForward, Urgent: true, Dest: dst.URL + "/api/webhooks/1/x"})
	sl := addHook(t, s, wire.HookSave{Name: "deploys", Role: "a", Mode: wire.HookModeMail})

	discord := `{"embeds":[{"title":"disk full on db-1","description":"98% used"}]}`
	for i := 0; i < 3; i++ {
		if w := postHook(s, d.Token, discord); w.Code != 200 {
			t.Fatalf("post %d=%d %s", i, w.Code, w.Body)
		}
	}
	if w := postHook(s, sl.Token, `{"text":"deploy done","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"build 42"}}]}`); w.Code != 200 {
		t.Fatalf("slack=%d %s", w.Code, w.Body)
	}
	b := inboxAlerts(t, s, "secret-b")
	if len(b) != 1 || b[0].Subject != "disk full on db-1" || b[0].AlertCount != 3 || b[0].Priority != "urgent" || !strings.Contains(b[0].Body, "98% used") {
		t.Fatalf("b's alerts %+v", b)
	}
	a := inboxAlerts(t, s, "secret-a")
	if len(a) != 1 || a[0].Subject != "deploy done" || a[0].Priority == "urgent" || !strings.Contains(a[0].Body, "build 42") {
		t.Fatalf("a's alerts %+v", a)
	}
	// The sweep sends on whatever the kick did not.
	if _, err := s.hooks().ForwardDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = s.hooks().ForwardDue(context.Background())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || got[0] != discord {
		t.Fatalf("destination got %q", got)
	}
}

// A destination that answers 500 is tried again with backoff; the final
// failure is mail to the role, with the status code and no address.
func TestHookForwardFailureIsMailed(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.admin.ProxyToken = "proxy-secret"
	now := time.Now()
	s.clock = func() time.Time { return now }
	tries := 0
	var mu sync.Mutex
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tries++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dst.Close()
	h := addHook(t, s, wire.HookSave{Name: "alerts", Role: "b", Mode: wire.HookModeForward, Dest: dst.URL})
	if w := postHook(s, h.Token, `{"content":"queue stuck"}`); w.Code != 200 {
		t.Fatalf("post=%d %s", w.Code, w.Body)
	}
	for i := 0; i <= len(hooks.Backoff); i++ {
		if i > 0 {
			now = now.Add(hooks.Backoff[i-1])
		}
		// Wait out a pass the kick may be running.
		for {
			mu.Lock()
			n := tries
			mu.Unlock()
			if n > i {
				break
			}
			if _, err := s.hooks().ForwardDue(context.Background()); err != nil {
				t.Fatal(err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	alerts := inboxAlerts(t, s, "secret-b")
	var fail *wire.Message
	for i := range alerts {
		if alerts[i].Subject == "원래 목적지 전송 실패: queue stuck" {
			fail = &alerts[i]
		}
	}
	if fail == nil || !strings.Contains(fail.Body, "상태 코드 500") || strings.Contains(fail.Body, dst.URL) {
		t.Fatalf("alerts %+v", alerts)
	}
	w := adminRequest(s, "GET", "/api/admin/hooks/"+itoa(h.ID), "", nil)
	var d wire.HookDetail
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || len(d.Receipts) != 1 || d.Receipts[0].Forward != wire.HookForwardFailed || d.Receipts[0].Status != 500 {
		t.Fatalf("detail %s", w.Body)
	}
}

// After adding, neither the address value nor the original destination is
// in any answer: the admin list and detail, the role list (the CLI's) or
// the boss role's view; a removed hook's address answers 404.
func TestHookSecretsNeverReadBack(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.admin.ProxyToken = "proxy-secret"
	dest := "https://hooks.example/services/T0/B0/destination-value"
	h := addHook(t, s, wire.HookSave{Name: "alerts", Role: "b", Mode: wire.HookModeForward, Dest: dest})
	if h.Dest != wire.HookDestSet {
		t.Fatalf("created %+v", h)
	}
	answers := []*httptest.ResponseRecorder{
		adminRequest(s, "GET", "/api/admin/hooks", "", nil),
		adminRequest(s, "GET", "/api/admin/hooks/"+itoa(h.ID), "", nil),
		adminWrite(s, "PUT", "/api/admin/hooks/"+itoa(h.ID), `{"name":"alerts","role":"b","mode":"forward","window_min":5}`),
		call(s, "GET", "/api/hooks", "secret-a", nil, ""),
	}
	for _, w := range answers {
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, h.Token) || strings.Contains(body, "destination-value") || strings.Contains(body, "…") {
			t.Fatalf("answer %d %s", w.Code, body)
		}
	}
	var list []wire.HookSummary
	if err := json.Unmarshal(answers[3].Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].Role != "b" || list[0].Mode != "forward" {
		t.Fatalf("role list %s", answers[3].Body)
	}
	var stored string
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE name=?`, "hook:"+itoa(h.ID)+":dest").Scan(&stored)
	if stored != "1" {
		t.Fatalf("destination not sealed in secrets: %q", stored)
	}
	if w := adminWrite(s, "DELETE", "/api/admin/hooks/"+itoa(h.ID), ""); w.Code != 200 {
		t.Fatalf("delete=%d %s", w.Code, w.Body)
	}
	if w := postHook(s, h.Token, `{"content":"x"}`); w.Code != 404 {
		t.Fatalf("deleted hook=%d", w.Code)
	}
}

// Reading needs view; changing needs a signed-in login with sessions, and
// a boss role's write route does not reach it (#476).
func TestHookAdminNeedsPerms(t *testing.T) {
	t.Parallel()
	s, secret, now := builtinServer(t)
	w := doAuth(s, authReq{method: "POST", path: "/auth/login", body: loginBody("alice", "correct horse battery", codeAt(t, secret, *now, 0))})
	c := sessionFrom(t, w)
	guard := map[string]string{"X-Switchboard-Dashboard": "1", "Content-Type": "application/json"}
	body := `{"name":"x","role":"a","mode":"mail"}`
	allowLogin(t, s, "alice", "view")
	if w := doAuth(s, authReq{method: "GET", path: "/api/admin/hooks", cookie: c}); w.Code != 200 {
		t.Fatalf("list with view=%d %s", w.Code, w.Body)
	}
	if w := doAuth(s, authReq{method: "POST", path: "/api/admin/hooks", cookie: c, headers: guard, body: body}); w.Code != 403 || !strings.Contains(w.Body.String(), "(sessions)") {
		t.Fatalf("add without sessions=%d %s", w.Code, w.Body)
	}
	allowLogin(t, s, "alice", "")
	if w := doAuth(s, authReq{method: "GET", path: "/api/admin/hooks", cookie: c}); w.Code != 403 {
		t.Fatalf("list without view=%d", w.Code)
	}
	allowLogin(t, s, "alice", "view,sessions")
	if w := doAuth(s, authReq{method: "POST", path: "/api/admin/hooks", cookie: c, headers: guard, body: body}); w.Code != 201 {
		t.Fatalf("add with sessions=%d %s", w.Code, w.Body)
	}
}
