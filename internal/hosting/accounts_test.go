package hosting

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// usage stores what node pc reports of its agent accounts, as a state
// frame does.
func usage(t *testing.T, s *Service, list ...wire.AgentUsage) {
	t.Helper()
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setNodeUsage("pc", string(b)); err != nil {
		t.Fatal(err)
	}
}

func account(t *testing.T, s *Service, adapter, name string) wire.AccountLimit {
	t.Helper()
	list, err := s.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.Node == "pc" && a.Adapter == adapter && a.Account == name {
			return a
		}
	}
	t.Fatalf("no account pc/%s/%s in %+v", adapter, name, list)
	return wire.AccountLimit{}
}

func left(a wire.AccountLimit) string {
	if a.Remaining == nil {
		return "unknown/" + a.Source
	}
	return strings.TrimSuffix(ThresholdText(a.Remaining), "%") + "/" + a.Source
}

// An account's remaining limit is the operator's switch, then the
// operator's value, then the adapter's, then the screen's message, then
// not known; the adapter's is dropped once its window refills (#435).
func TestAccountRemainingOrder(t *testing.T) {
	s, now := limitService(t)
	ad := s.nodeDefaultAdapter("pc")
	h := add(t, s, "/w/a", "", "")
	if a := account(t, s, ad, ""); left(a) != "unknown/" || a.Sessions != 1 {
		t.Fatalf("before any report: %s, %d sessions", left(a), a.Sessions)
	}
	reset := now.UnixMilli() + 3_600_000
	usage(t, s, wire.AgentUsage{Adapter: ad, Remaining: pct(40), ResetsAt: &reset})
	if got := left(account(t, s, ad, "")); got != "40/adapter" {
		t.Fatalf("adapter: %s", got)
	}
	if _, err := s.SetAccountLimit(wire.AccountLimitChange{Node: "pc", Adapter: ad, Remaining: pct(70)}); err != nil {
		t.Fatal(err)
	}
	if a := account(t, s, ad, ""); left(a) != "70/manual" || a.Manual == nil || *a.Manual != 70 {
		t.Fatalf("manual: %s %v", left(a), a.Manual)
	}
	if _, err := s.SetLimit(true); err != nil {
		t.Fatal(err)
	}
	if got := left(account(t, s, ad, "")); got != "0/operator" {
		t.Fatalf("operator on: %s", got)
	}
	// Off clears the operator's values too.
	if _, err := s.SetLimit(false); err != nil {
		t.Fatal(err)
	}
	if a := account(t, s, ad, ""); left(a) != "40/adapter" || a.Manual != nil {
		t.Fatalf("operator off: %s %v", left(a), a.Manual)
	}
	*now = now.Add(61 * 60 * 1e9)
	if got := left(account(t, s, ad, "")); got != "unknown/" {
		t.Fatalf("after resets_at: %s", got)
	}
	// A node that reports no usage: its screens' message counts as 0.
	usage(t, s)
	since := now.UnixMilli()
	if err := s.setNodeLimit("pc", wire.NodeLimit{Limited: true, Since: since}); err != nil {
		t.Fatal(err)
	}
	if got := left(account(t, s, ad, "")); got != "0/screen" {
		t.Fatalf("screen: %s", got)
	}
	// The account the node reports a session uses is its own.
	if err := s.setSessionAccount(h.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(h.ID); err != nil || got.Account != "work" {
		t.Fatalf("record account %q %v", got.Account, err)
	}
	if a := account(t, s, ad, "work"); a.Sessions != 1 {
		t.Fatalf("work account sessions %d", a.Sessions)
	}
	for _, bad := range []wire.AccountLimitChange{{Adapter: ad}, {Node: "pc", Adapter: ad, Remaining: pct(101)}, {Node: "pc", Adapter: "Bad Name"}} {
		if _, err := s.SetAccountLimit(bad); err == nil {
			t.Fatalf("%+v taken", bad)
		}
	}
}

// A session stops taking work when its account falls below its group's
// threshold and takes it again when the account comes back up; the
// account's limit-cleared goes out once each time it leaves 0 (#435).
func TestThresholdCrossedDownAndBackUp(t *testing.T) {
	s, now := limitService(t)
	ad := s.nodeDefaultAdapter("pc")
	addGroup(t, s, "slow", pct(20))
	addGroup(t, s, "fast", nil)
	slow := add(t, s, "/w/slow", "slow", "")
	fast := add(t, s, "/w/fast", "fast", "")
	none := add(t, s, "/w/none", "", "")
	var kinds []string
	hub := NewHub(s, func(k ...string) { kinds = append(kinds, k...) })
	if err := hub.Sync(); err != nil {
		t.Fatal(err)
	}
	cleared := func() int {
		n := 0
		for _, k := range kinds {
			if k == LimitClearedKind {
				n++
			}
		}
		return n
	}
	report := func(left *int) {
		t.Helper()
		reset := now.UnixMilli() + 3_600_000
		usage(t, s, wire.AgentUsage{Adapter: ad, Remaining: left, ResetsAt: &reset})
		if err := hub.checkLimit(); err != nil {
			t.Fatal(err)
		}
	}
	want := func(step string, slowOK bool) {
		t.Helper()
		exp := map[string]bool{slow.ID: slowOK, fast.ID: true, none.ID: true}
		list, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range list {
			if h.AcceptingWork != exp[h.ID] {
				t.Fatalf("%s: list %s (%s) accepting %v", step, h.ID, h.Group, h.AcceptingWork)
			}
		}
	}
	want("not known", true)
	report(pct(20))
	want("at the threshold", true)
	report(pct(19))
	want("below", false)
	report(pct(0))
	want("empty", false)
	if cleared() != 0 {
		t.Fatalf("cleared before it rose: %v", kinds)
	}
	report(pct(30))
	want("back up", true)
	if cleared() != 1 {
		t.Fatalf("limit-cleared %d, want 1: %v", cleared(), kinds)
	}
	if a := account(t, s, ad, ""); a.ClearedAt == nil {
		t.Fatalf("no cleared_at: %+v", a)
	}
	report(pct(0))
	*now = now.Add(61 * 60 * 1e9)
	if err := hub.Sync(); err != nil {
		t.Fatal(err)
	}
	want("window refilled", true)
	if cleared() != 2 {
		t.Fatalf("limit-cleared %d after the window refilled, want 2", cleared())
	}
	// The operator's value counts over the adapter's.
	if _, err := hub.SetAccountLimit(wire.AccountLimitChange{Node: "pc", Adapter: ad, Remaining: pct(5)}); err != nil {
		t.Fatal(err)
	}
	want("manual 5", false)
}

// PUT /api/admin/terminals/limit/accounts sets and clears an account's
// operator value; the node list carries the accounts.
func TestAccountLimitAPI(t *testing.T) {
	s, _ := limitService(t)
	ad := s.nodeDefaultAdapter("pc")
	add(t, s, "/w/a", "", "")
	hub := NewHub(s, nil)
	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		hub.AdminTerminalsAPI(w, httptest.NewRequest(http.MethodPut, "/api/admin/terminals/limit/accounts", strings.NewReader(body)), "limit/accounts")
		return w
	}
	w := put(`{"node":"pc","adapter":"` + ad + `","account":"","remaining":12}`)
	var l wire.HostingLimit
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &l) != nil || len(l.Accounts) != 1 || l.Accounts[0].Source != AccountManual {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	if w := put(`{"node":"pc","adapter":"` + ad + `","remaining":300}`); w.Code != http.StatusBadRequest {
		t.Fatalf("300: %d", w.Code)
	}
	nodes, err := s.Nodes()
	if err != nil || len(nodes) != 1 || len(nodes[0].Accounts) != 1 || *nodes[0].Accounts[0].Manual != 12 {
		t.Fatalf("nodes %+v %v", nodes, err)
	}
	if w := put(`{"node":"pc","adapter":"` + ad + `","remaining":null}`); w.Code != http.StatusOK || account(t, s, ad, "").Manual != nil {
		t.Fatalf("clear: %d %s", w.Code, w.Body)
	}
}
