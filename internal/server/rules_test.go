package server

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// rulesServer is questionServer (global boss sandcorp/boss, team/worker)
// with the dashboard's admin proxy.
func rulesServer(t *testing.T) *Server {
	t.Helper()
	s := questionServer(t)
	s.admin.ProxyToken = "proxy-secret"
	return s
}

func ruleBody(kind, scope, topic, text string, more map[string]any) map[string]any {
	sc, err := wire.ParseRuleScope(scope)
	if err != nil {
		panic(err)
	}
	b := map[string]any{"kind": kind, "scope": sc, "topic": topic, "text": text, "source_text": "panel answer of the day"}
	for k, v := range more {
		b[k] = v
	}
	return b
}

func addRuleAs(t *testing.T, s *Server, token, role string, body map[string]any, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := callAs(s, "POST", "/api/rules", token, role, body)
	if w.Code != want {
		t.Fatalf("add rule as %s: %d %s", role, w.Code, w.Body)
	}
	return w
}

func addRule(t *testing.T, s *Server, body map[string]any) wire.Rule {
	t.Helper()
	w := addRuleAs(t, s, bossToken, internalBossRole, body, 201)
	var r wire.Rule
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func listRulesAs(t *testing.T, s *Server, token, role, query string) wire.RuleList {
	t.Helper()
	w := callAs(s, "GET", "/api/rules"+query, token, role, nil)
	if w.Code != 200 {
		t.Fatalf("list rules as %s%s: %d %s", role, query, w.Code, w.Body)
	}
	var l wire.RuleList
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func ruleIDs(rs []wire.Rule) []int64 {
	out := []int64{}
	for _, r := range rs {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

func showRule(t *testing.T, s *Server, token, role string, id int64, want int) wire.RuleDetail {
	t.Helper()
	w := callAs(s, "GET", "/api/rules/"+itoa(id), token, role, nil)
	if w.Code != want {
		t.Fatalf("show rule %d as %s: %d %s", id, role, w.Code, w.Body)
	}
	var d wire.RuleDetail
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	return d
}

// TestRuleReplaceFlow: a new row on the same scope and topic as a row in
// force is refused with 409 and that row until the writer picks replace or
// alongside; a replaced row leaves the default list and stays in history.
func TestRuleReplaceFlow(t *testing.T) {
	s := rulesServer(t)
	addRuleAs(t, s, bossToken, internalBossRole, ruleBody("decision", "service:team", "release", "ship on green", nil), 400) // no such topic
	first := addRule(t, s, ruleBody("decision", "service:team", "release", "ship on green", map[string]any{"new_topic": true}))
	if first.Status != wire.RuleActive || first.Author != internalBossRole || first.Scope.String() != "service:team" {
		t.Fatalf("first = %+v", first)
	}

	w := addRuleAs(t, s, bossToken, internalBossRole, ruleBody("decision", "service:partner-x,team", "release", "ship on Fridays too", nil), 409)
	var c wire.RuleConflict
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil || len(c.Rules) != 1 || c.Rules[0].ID != first.ID {
		t.Fatalf("conflict = %s", w.Body)
	}
	// Another topic, another scope kind: no conflict.
	other := addRule(t, s, ruleBody("tip", "all", "ci", "rerun once", map[string]any{"new_topic": true}))

	second := addRule(t, s, ruleBody("decision", "service:team", "release", "ship on green and a review", map[string]any{"replace": []int64{first.ID}}))
	if !slices.Equal(second.Replaces, []int64{first.ID}) {
		t.Fatalf("second = %+v", second)
	}
	old := showRule(t, s, bossToken, internalBossRole, first.ID, 200)
	if old.Status != wire.RuleReplaced || old.ReplacedBy != second.ID || len(old.History) != 1 || old.History[0].ID != second.ID {
		t.Fatalf("replaced row = %+v", old)
	}
	if got := ruleIDs(listRulesAs(t, s, bossToken, internalBossRole, "").Rules); !slices.Equal(got, []int64{other.ID, second.ID}) {
		t.Fatalf("in force = %v", got)
	}
	if got := ruleIDs(listRulesAs(t, s, bossToken, internalBossRole, "?all=1").Rules); len(got) != 3 {
		t.Fatalf("all = %v", got)
	}

	third := addRule(t, s, ruleBody("tip", "service:team", "release", "tag the build", map[string]any{"alongside": true}))
	if got := ruleIDs(listRulesAs(t, s, bossToken, internalBossRole, "?topic=release").Rules); !slices.Equal(got, []int64{second.ID, third.ID}) {
		t.Fatalf("alongside = %v", got)
	}
	if got := listRulesAs(t, s, bossToken, internalBossRole, "?topic=release&kind=tip").Rules; len(got) != 1 || got[0].ID != third.ID {
		t.Fatalf("kind=tip = %v", got)
	}
	// Replacing a row no longer in force is refused.
	addRuleAs(t, s, bossToken, internalBossRole, ruleBody("decision", "service:team", "release", "x", map[string]any{"replace": []int64{first.ID}, "alongside": false}), 400)
	addRuleAs(t, s, bossToken, internalBossRole, ruleBody("decision", "service:team", "release", "x", map[string]any{"replace": []int64{second.ID}, "alongside": true}), 400)

	// Void takes a row out of force; a second void is a conflict.
	if w := callAs(s, "POST", "/api/rules/"+itoa(third.ID)+"/void", bossToken, internalBossRole, nil); w.Code != 200 {
		t.Fatalf("void: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/rules/"+itoa(third.ID)+"/void", bossToken, internalBossRole, nil); w.Code != 409 {
		t.Fatalf("void again: %d %s", w.Code, w.Body)
	}
	v := showRule(t, s, bossToken, internalBossRole, third.ID, 200)
	if v.Status != wire.RuleVoid || v.VoidedBy != internalBossRole || v.VoidedAt == nil {
		t.Fatalf("void row = %+v", v)
	}
}

// TestRuleWriters: only an admin and a global boss write; the admin's rows
// carry its name. A retry with the same idempotency key returns the first.
func TestRuleWriters(t *testing.T) {
	s := rulesServer(t)
	addRuleAs(t, s, workerToken, "team/worker", ruleBody("tip", "all", "ci", "rerun once", map[string]any{"new_topic": true}), 403)
	b, _ := json.Marshal(ruleBody("tip", "all", "ci", "rerun once", map[string]any{"new_topic": true}))
	w := adminWrite(s, "POST", "/api/admin/rules", string(b))
	if w.Code != 201 {
		t.Fatalf("admin add: %d %s", w.Code, w.Body)
	}
	var r wire.Rule
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r.Author == "" || r.Author[:6] != "admin:" {
		t.Fatalf("admin row author = %q", r.Author)
	}
	if w := callAs(s, "POST", "/api/rules/"+itoa(r.ID)+"/void", workerToken, "team/worker", nil); w.Code != 403 {
		t.Fatalf("worker void: %d", w.Code)
	}
	if w := adminWrite(s, "GET", "/api/admin/rules/"+itoa(r.ID), ""); w.Code != 200 {
		t.Fatalf("admin show: %d", w.Code)
	}

	body := ruleBody("decision", "service:team", "release", "ship on green", map[string]any{"new_topic": true, "idempotency_key": "k1"})
	one := addRule(t, s, body)
	w = addRuleAs(t, s, bossToken, internalBossRole, body, 200)
	var again wire.Rule
	_ = json.Unmarshal(w.Body.Bytes(), &again)
	if again.ID != one.ID {
		t.Fatalf("retry made a new row: %d vs %d", again.ID, one.ID)
	}
}

// TestRuleSources: a source must be an answered question; a full reader
// sees the answers with the row.
func TestRuleSources(t *testing.T) {
	s := rulesServer(t)
	q := askQuestionAs(t, s, workerToken, "team/worker", 201)
	addRuleAs(t, s, bossToken, internalBossRole, ruleBody("decision", "all", "release", "ship", map[string]any{"new_topic": true, "sources": []string{q.ID}, "source_text": ""}), 400)
	answerQuestionAs(t, s, bossToken, internalBossRole, q.ID, sampleAnswers(), 200)
	r := addRule(t, s, ruleBody("decision", "all", "release", "ship", map[string]any{"new_topic": true, "sources": []string{q.ID}, "source_text": ""}))
	d := showRule(t, s, bossToken, internalBossRole, r.ID, 200)
	if len(d.Answers) != 1 || d.Answers[0].ID != q.ID || d.Answers[0].Items[0].Chosen[0] != "예" {
		t.Fatalf("answers = %+v", d.Answers)
	}
	// The worker reads the row in its scope without the source.
	wd := showRule(t, s, workerToken, "team/worker", r.ID, 200)
	if wd.Text != "ship" || wd.Sources != nil || wd.Answers != nil || wd.Scope != nil || wd.Status != "" || wd.Author != "" {
		t.Fatalf("worker detail = %+v", wd)
	}
}
