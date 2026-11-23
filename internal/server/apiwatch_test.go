package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

const watchKey = "watch-key-abcdef-0123"

// watchServer is slotServer with an API watch's hooks and a fake API that
// answers body to a request carrying the key.
func watchServer(t *testing.T) (*Server, *fakeClock, func(string), string) {
	t.Helper()
	s, clock := slotServer(t)
	s.admin.ProxyToken = "proxy-secret"
	var mu sync.Mutex
	body := `{"data":{"count":1}}`
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != watchKey {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(api.Close)
	return s, clock, func(b string) { mu.Lock(); body = b; mu.Unlock() }, api.URL
}

func watchRuleJSON(url string) string {
	b, _ := json.Marshal(wire.WatchRule{Name: "builds", To: "team/worker", URL: url + "/v1/builds?token=q", Every: 1, Enabled: true,
		Headers: []wire.WatchHeader{{Name: "X-Api-Key", Value: watchKey, Secret: true}},
		Cond:    wire.WatchCond{Kind: wire.WatchValue, Path: "data.count", Op: "gt", Value: "5"}, Subject: "빌드 {data.count}개"})
	return string(b)
}

func TestWatchDashboardFlowAndMail(t *testing.T) {
	s, clock, setBody, url := watchServer(t)
	rule := watchRuleJSON(url)

	// 시험 조회: a result, no mail, nothing stored.
	setBody(`{"data":{"count":9}}`)
	w := adminWrite(s, "POST", "/api/admin/watches/test", rule)
	var tr wire.WatchTest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &tr) != nil || !tr.OK || !tr.Matched || tr.Subject != "빌드 9개" {
		t.Fatalf("test=%d %s", w.Code, w.Body)
	}
	if got := systemMail(t, s, workerToken, "team/worker"); len(got) != 0 {
		t.Fatalf("test fetch mailed: %+v", got)
	}
	if w := adminRequest(s, "GET", "/api/admin/watches", "", nil); w.Body.String() != "[]\n" {
		t.Fatalf("test stored: %s", w.Body)
	}

	w = adminWrite(s, "POST", "/api/admin/watches", rule)
	if w.Code != 201 {
		t.Fatalf("add=%d %s", w.Code, w.Body)
	}
	var created wire.Watch
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if w := adminWrite(s, "POST", "/api/admin/watches", rule); w.Code != 409 {
		t.Fatalf("same name=%d", w.Code)
	}

	clock.add(time.Minute)
	s.runWatches(context.Background())
	clock.add(time.Minute)
	s.runWatches(context.Background())
	mails := systemMail(t, s, workerToken, "team/worker")
	if len(mails) != 1 || mails[0].Subject != "빌드 9개" {
		t.Fatalf("mail: %+v", mails)
	}
	// The mail is a collapsing alert (#525) from this rule; the key is sealed
	// in the secret store, not kept in plain text.
	var source string
	var sealed []byte
	if err := s.db.QueryRow(`SELECT alert_source FROM messages WHERE recipient='team/worker' AND subject='빌드 9개'`).Scan(&source); err != nil || source != "watch:builds" {
		t.Fatalf("alert source=%q %v", source, err)
	}
	if err := s.db.QueryRow(`SELECT sealed FROM secrets WHERE name LIKE 'watch:%'`).Scan(&sealed); err != nil || strings.Contains(string(sealed), watchKey) {
		t.Fatalf("secret row: %v", err)
	}

	// The role's list: name, role, period and the last result only.
	r := callAs(s, "GET", "/api/watches", workerToken, "team/worker", nil)
	var rows []wire.WatchSummary
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].LastResult != "맞음" || strings.Contains(r.Body.String(), "v1/builds") {
		t.Fatalf("role list=%d %s", r.Code, r.Body)
	}
	if r := callAs(s, "GET", "/api/watches", "secret-b", "b", nil); r.Body.String() != "[]\n" {
		t.Fatalf("other role sees: %s", r.Body)
	}

	// The key is in no response and no mail.
	all := adminRequest(s, "GET", "/api/admin/watches", "", nil).Body.String() + w.Body.String() + mails[0].Body
	if strings.Contains(all, watchKey) || !strings.Contains(all, `"set":true`) {
		t.Fatalf("key shown or not marked: %s", all)
	}

	// Off, then removed.
	if w := adminWrite(s, "POST", "/api/admin/watches/"+strconv.FormatInt(created.ID, 10)+"/off", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("off=%d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "DELETE", "/api/admin/watches/"+strconv.FormatInt(created.ID, 10), ""); w.Code != 204 {
		t.Fatalf("delete=%d %s", w.Code, w.Body)
	}
	var left int
	if s.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE name LIKE 'watch:%'`).Scan(&left); left != 0 {
		t.Fatalf("secret left after delete: %d", left)
	}
}

func TestWatchWritesNeedTheDashboardGuard(t *testing.T) {
	s, _, _, _ := watchServer(t)
	rule := watchRuleJSON("https://api.example")
	if w := adminRequest(s, "POST", "/api/admin/watches", rule, nil); w.Code == 201 {
		t.Fatalf("write without the dashboard headers: %d", w.Code)
	}
	if w := adminWrite(s, "POST", "/api/admin/watches", strings.Replace(rule, "team/worker", "team/none", 1)); w.Code != 400 || !strings.Contains(w.Body.String(), "받는 역할 없음") {
		t.Fatalf("unknown role=%d %s", w.Code, w.Body)
	}
}
