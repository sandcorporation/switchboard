package server

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func setProposer(t *testing.T, s *Server, role string, on bool, want int) {
	t.Helper()
	w := callAs(s, "POST", "/api/rules/proposers", bossToken, internalBossRole, map[string]any{"role": role, "on": on})
	if w.Code != want {
		t.Fatalf("proposer %s %v: %d %s", role, on, w.Code, w.Body)
	}
}

func decodeRule(t *testing.T, w *httptest.ResponseRecorder) wire.Rule {
	t.Helper()
	var r wire.Rule
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return r
}

func mineIDs(t *testing.T, s *Server, token, role string) []int64 {
	t.Helper()
	_, h := mineAs(t, s, token, role, "")
	out := []int64{}
	for _, r := range h.Rules {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

// TestRuleProposals: a role is a proposer only when an admin or a global
// boss turns it on. Its tip on its own service alone is in force at once;
// on every role it waits as proposed (out of mine, footers and the other
// roles' lists, one notice to the global boss) until approved through the
// replace flow, or rejected. Decisions stay the full writers'.
func TestRuleProposals(t *testing.T) {
	s, _ := decisionServer(t)
	addRule(t, s, ruleBody("decision", "all", "ci", "keep main green", map[string]any{"new_topic": true}))
	tip := func(scope, text string, more map[string]any) map[string]any {
		return ruleBody("tip", scope, "ci", text, more)
	}

	// Off: the worker writes nothing, whatever its name.
	addRuleAs(t, s, workerToken, "team/worker", tip("service:team", "rerun once", nil), 403)
	if w := callAs(s, "POST", "/api/rules/proposers", workerToken, "team/worker", map[string]any{"role": "team/worker", "on": true}); w.Code != 403 {
		t.Fatalf("worker sets itself: %d", w.Code)
	}
	setProposer(t, s, "team/worker", true, 200)
	w := callAs(s, "GET", "/api/rules/proposers", bossToken, internalBossRole, nil)
	var ps wire.RuleProposers
	_ = json.Unmarshal(w.Body.Bytes(), &ps)
	if len(ps.Proposers) != 1 || ps.Proposers[0].Role != "team/worker" || ps.Proposers[0].SetBy != internalBossRole {
		t.Fatalf("proposers = %+v", ps)
	}
	if w := callAs(s, "GET", "/api/rules/proposers", workerToken, "team/worker", nil); w.Code != 403 {
		t.Fatalf("worker reads proposers: %d", w.Code)
	}

	addRuleAs(t, s, workerToken, "team/worker", ruleBody("decision", "service:team", "ci", "x", nil), 403)
	addRuleAs(t, s, workerToken, "team/worker", tip("service:team", "x", map[string]any{"topic": "new-one", "new_topic": true}), 400)
	addRuleAs(t, s, workerToken, "team/worker", tip("all", "x", map[string]any{"alongside": true}), 400)

	// Its own service alone: in force at once (no overlap with the all row).
	own := decodeRule(t, addRuleAs(t, s, workerToken, "team/worker", tip("service:team", "rerun once", nil), 201))
	if own.Status != wire.RuleActive || own.Author != "team/worker" {
		t.Fatalf("own tip = %+v", own)
	}
	// A second one on the same scope and topic meets the replace flow, with
	// the rows a limited reader may see.
	w = addRuleAs(t, s, workerToken, "team/worker", tip("service:team", "rerun twice", nil), 409)
	var conflict wire.RuleConflict
	_ = json.Unmarshal(w.Body.Bytes(), &conflict)
	if len(conflict.Rules) != 1 || conflict.Rules[0].ID != own.ID || conflict.Rules[0].Author != "" {
		t.Fatalf("conflict = %+v", conflict)
	}
	two := decodeRule(t, addRuleAs(t, s, workerToken, "team/worker", tip("service:team", "rerun twice", map[string]any{"replace": []int64{own.ID}}), 201))

	// Every role: proposed.
	prop := decodeRule(t, addRuleAs(t, s, workerToken, "team/worker", tip("all", "cache the modules", map[string]any{"idempotency_key": "p1"}), 201))
	if prop.Status != wire.RuleProposed {
		t.Fatalf("all-scope tip = %+v", prop)
	}
	if again := decodeRule(t, addRuleAs(t, s, workerToken, "team/worker", tip("all", "cache the modules", map[string]any{"idempotency_key": "p1"}), 200)); again.ID != prop.ID {
		t.Fatalf("retry made a new proposal: %d", again.ID)
	}
	for _, who := range []struct{ token, role string }{{workerToken, "team/worker"}, {teamOtherToken, "team/other"}} {
		if ids := ruleIDs(listRulesAs(t, s, who.token, who.role, "").Rules); slices.Contains(ids, prop.ID) {
			t.Fatalf("%s lists the proposal: %v", who.role, ids)
		}
		if ids := mineIDs(t, s, who.token, who.role); slices.Contains(ids, prop.ID) {
			t.Fatalf("%s mine has the proposal: %v", who.role, ids)
		}
		showRule(t, s, who.token, who.role, prop.ID, 404)
	}
	if w := callAs(s, "GET", "/api/rules?proposed=1", workerToken, "team/worker", nil); w.Code != 403 {
		t.Fatalf("worker lists proposals: %d", w.Code)
	}
	full := listRulesAs(t, s, bossToken, internalBossRole, "")
	if full.Proposed != 1 || slices.Contains(ruleIDs(full.Rules), prop.ID) {
		t.Fatalf("boss default list = %+v", full)
	}
	if ids := ruleIDs(listRulesAs(t, s, bossToken, internalBossRole, "?proposed=1").Rules); !slices.Equal(ids, []int64{prop.ID}) {
		t.Fatalf("proposed list = %v", ids)
	}
	notices := 0
	for _, m := range inboxOf(t, s, bossToken, internalBossRole) {
		if strings.HasPrefix(m.Subject, "[규칙 제안] #"+itoa(prop.ID)) {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("notices = %d", notices)
	}
	footer, err := s.rules().Footer("team/other")
	if err != nil || strings.Contains(footer, "cache the modules") || strings.Contains(footer, "#"+itoa(prop.ID)) {
		t.Fatalf("footer = %q, %v", footer, err)
	}

	// Approval: only full callers; the all-scope decision on the topic
	// needs a choice.
	if w := callAs(s, "POST", "/api/rules/"+itoa(prop.ID)+"/approve", workerToken, "team/worker", nil); w.Code != 403 {
		t.Fatalf("worker approves: %d", w.Code)
	}
	if w := callAs(s, "POST", "/api/rules/"+itoa(prop.ID)+"/approve", bossToken, internalBossRole, nil); w.Code != 409 {
		t.Fatalf("approve without a choice: %d %s", w.Code, w.Body)
	}
	w = callAs(s, "POST", "/api/rules/"+itoa(prop.ID)+"/approve", bossToken, internalBossRole, map[string]any{"alongside": true})
	if w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if r := decodeRule(t, w); r.Status != wire.RuleActive || r.ReviewedBy != internalBossRole || r.ReviewedAt == nil {
		t.Fatalf("approved = %+v", r)
	}
	if ids := mineIDs(t, s, teamOtherToken, "team/other"); !slices.Contains(ids, prop.ID) || !slices.Contains(ids, two.ID) {
		t.Fatalf("team/other mine after approval = %v", ids)
	}
	if w := callAs(s, "POST", "/api/rules/"+itoa(prop.ID)+"/approve", bossToken, internalBossRole, nil); w.Code != 409 {
		t.Fatalf("approve twice: %d", w.Code)
	}

	// Rejection keeps the row out of force, with its reason; off stops more.
	rej := decodeRule(t, addRuleAs(t, s, workerToken, "team/worker", tip("all", "skip the lint", nil), 201))
	w = callAs(s, "POST", "/api/rules/"+itoa(rej.ID)+"/reject", bossToken, internalBossRole, map[string]any{"reason": "lint stays"})
	if r := decodeRule(t, w); w.Code != 200 || r.Status != wire.RuleRejected || r.ReviewNote != "lint stays" {
		t.Fatalf("reject: %d %+v", w.Code, r)
	}
	if ids := mineIDs(t, s, teamOtherToken, "team/other"); slices.Contains(ids, rej.ID) {
		t.Fatalf("rejected row in mine: %v", ids)
	}
	setProposer(t, s, "team/worker", false, 200)
	addRuleAs(t, s, workerToken, "team/worker", tip("service:team", "rerun thrice", map[string]any{"alongside": true}), 403)
}

func inboxOf(t *testing.T, s *Server, token, role string) []wire.Message {
	t.Helper()
	w := callAs(s, "GET", "/api/messages", token, role, nil)
	if w.Code != 200 {
		t.Fatalf("inbox %s: %d %s", role, w.Code, w.Body)
	}
	var ms []wire.Message
	_ = json.Unmarshal(w.Body.Bytes(), &ms)
	return ms
}

func unclassifiedAs(t *testing.T, s *Server, token, role, query, etag string) (*httptest.ResponseRecorder, wire.Unclassified) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/rules/unclassified"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Switchboard-Role", role)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	w := httptest.NewRecorder()
	s.api(w, req)
	var u wire.Unclassified
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
			t.Fatalf("%v: %s", err, w.Body)
		}
	} else if w.Code != 304 {
		t.Fatalf("unclassified as %s: %d %s", role, w.Code, w.Body)
	}
	return w, u
}

func answerIDs(u wire.Unclassified) []string {
	out := []string{}
	for _, a := range u.Answers {
		out = append(out, a.ID)
	}
	return out
}

// TestAnswerClasses: a person's answer (the dashboard) to a panel question
// starts unclassified for its asker; a boss role's answer is not counted.
// The asker marks it once or as a rule's source (or a row added with it as
// a source does); another role cannot; a full reader sees every role's
// count.
func TestAnswerClasses(t *testing.T) {
	s, _ := decisionServer(t)
	answerByPerson := func(token, role, header string) string {
		a := askOne(t, s, token, role, header)
		if w := adminWrite(s, "POST", "/api/admin/questions/"+a.ID+"/answer", `{"answers":[{"selected":["예"]}]}`); w.Code != 200 {
			t.Fatalf("dashboard answer: %d %s", w.Code, w.Body)
		}
		return a.ID
	}
	q1 := answerByPerson(workerToken, "team/worker", "배포")
	q2 := answerByPerson(workerToken, "team/worker", "캐시")
	q3 := answerByPerson(teamOtherToken, "team/other", "린트")
	byBoss := decide(t, s, workerToken, "team/worker", "보스 답", "예")

	w, u := unclassifiedAs(t, s, workerToken, "team/worker", "", "")
	if u.Count != 2 || !slices.Equal(answerIDs(u), []string{q1, q2}) || u.Answers[0].Header != "배포" || u.Answers[0].Asker != "team/worker" || u.Roles != nil {
		t.Fatalf("worker unclassified = %+v (boss answer %s)", u, byBoss.ID)
	}
	etag := w.Header().Get("ETag")
	if w, _ := unclassifiedAs(t, s, workerToken, "team/worker", "", etag); w.Code != 304 {
		t.Fatalf("same etag: %d", w.Code)
	}
	_, full := unclassifiedAs(t, s, bossToken, internalBossRole, "", "")
	if full.Count != 3 || len(full.Roles) != 2 || full.Roles[0] != (wire.RoleCount{Role: "team/other", Count: 1}) || full.Roles[1] != (wire.RoleCount{Role: "team/worker", Count: 2}) {
		t.Fatalf("full unclassified = %+v", full)
	}
	if _, one := unclassifiedAs(t, s, bossToken, internalBossRole, "?role=team/other", ""); !slices.Equal(answerIDs(one), []string{q3}) {
		t.Fatalf("?role = %+v", one)
	}

	classify := func(token, role string, body map[string]any, want int) wire.AnswerRef {
		t.Helper()
		w := callAs(s, "POST", "/api/rules/classify", token, role, body)
		if w.Code != want {
			t.Fatalf("classify %v as %s: %d %s", body, role, w.Code, w.Body)
		}
		var a wire.AnswerRef
		_ = json.Unmarshal(w.Body.Bytes(), &a)
		return a
	}
	classify(teamOtherToken, "team/other", map[string]any{"question": q1, "once": true}, 404)
	classify(workerToken, "team/worker", map[string]any{"question": byBoss.ID, "once": true}, 404)
	classify(workerToken, "team/worker", map[string]any{"question": q1}, 400)
	classify(workerToken, "team/worker", map[string]any{"question": q1, "once": true, "rule": 1}, 400)
	if a := classify(workerToken, "team/worker", map[string]any{"question": q1, "once": true}, 200); a.Class != wire.AnswerOnce {
		t.Fatalf("once = %+v", a)
	}
	// A rule the worker does not see is no rule to it.
	other := addRule(t, s, ruleBody("decision", "service:elsewhere", "ci", "x", map[string]any{"new_topic": true}))
	classify(workerToken, "team/worker", map[string]any{"question": q2, "rule": other.ID}, 400)
	mineRow := addRule(t, s, ruleBody("decision", "service:team", "ci", "cache it", nil))
	if a := classify(workerToken, "team/worker", map[string]any{"question": q2, "rule": mineRow.ID}, 200); a.Class != wire.AnswerRule || a.Rule != mineRow.ID {
		t.Fatalf("rule = %+v", a)
	}
	if _, u = unclassifiedAs(t, s, workerToken, "team/worker", "", ""); u.Count != 0 || len(u.Answers) != 0 {
		t.Fatalf("worker after classify = %+v", u)
	}

	// Adding a row with the answer as its source classifies it.
	src := addRule(t, s, ruleBody("tip", "all", "ci", "lint first", map[string]any{"sources": []string{q3}, "source_text": ""}))
	if _, u = unclassifiedAs(t, s, teamOtherToken, "team/other", "", ""); u.Count != 0 {
		t.Fatalf("team/other after the source row #%d = %+v", src.ID, u)
	}
	if _, full = unclassifiedAs(t, s, bossToken, internalBossRole, "", ""); full.Count != 0 || len(full.Roles) != 0 {
		t.Fatalf("full after = %+v", full)
	}

	// The dashboard reads every answer's class (#581); only a full reader.
	if w := callAs(s, "GET", "/api/rules/classes", workerToken, "team/worker", nil); w.Code != 403 {
		t.Fatalf("classes as a worker: %d %s", w.Code, w.Body)
	}
	w = callAs(s, "GET", "/api/rules/classes", bossToken, internalBossRole, nil)
	var all wire.AnswerClasses
	if err := json.Unmarshal(w.Body.Bytes(), &all); w.Code != 200 || err != nil {
		t.Fatalf("classes: %d %s", w.Code, w.Body)
	}
	got := map[string]wire.AnswerRef{}
	for _, a := range all.Answers {
		got[a.ID] = a
	}
	if len(all.Answers) != 3 || got[q1].Class != wire.AnswerOnce || got[q2].Class != wire.AnswerRule || got[q2].Rule != mineRow.ID || got[q3].Rule != src.ID {
		t.Fatalf("classes = %+v", all.Answers)
	}
}
