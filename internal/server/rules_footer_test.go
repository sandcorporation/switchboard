package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func postMail(t *testing.T, s *Server, token, role, path string, body map[string]any) wire.Message {
	t.Helper()
	var b bytes.Buffer
	_ = json.NewEncoder(&b).Encode(body)
	r := httptest.NewRequest("POST", path, &b)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Switchboard-Role", role)
	if k, ok := body["idem"].(string); ok {
		r.Header.Set("Idempotency-Key", k)
	}
	w := httptest.NewRecorder()
	s.api(w, r)
	if w.Code != 201 && w.Code != 200 {
		t.Fatalf("send as %s: %d %s", role, w.Code, w.Body)
	}
	var m wire.Message
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestDirectiveRulesFooter (#408 T3): a boss's directive ends with the
// recipient's rows in force that have no device and no condition, each
// decision on a line and the tips on one; replies, mail from other roles
// and SWITCHBOARD_RULES_FOOTER=off get none, and a retry keeps the body.
func TestDirectiveRulesFooter(t *testing.T) {
	t.Parallel()
	s := rulesServer(t)
	rel := addRule(t, s, ruleBody("decision", "service:team", "release", "ship on green\n- tag after", map[string]any{"new_topic": true}))
	addRule(t, s, ruleBody("decision", "service:team", "push", "no force push", map[string]any{"new_topic": true, "match": map[string]any{"tool": "Bash"}}))
	addRule(t, s, ruleBody("decision", "service:team", "lint", "gofmt", map[string]any{"new_topic": true, "device": "pre-commit hook"}))
	addRule(t, s, ruleBody("tip", "service:team", "db", "migrations only add", map[string]any{"new_topic": true}))
	addRule(t, s, ruleBody("tip", "all", "ci", "rerun once", map[string]any{"new_topic": true}))
	addRule(t, s, ruleBody("tip", "service:team", "ci", "read the log", map[string]any{"alongside": true}))
	addRule(t, s, ruleBody("decision", "service:partner-a", "release", "other", nil))

	want := "do the thing\n\n---\n결정 #" + itoa(rel.ID) + " release\n요령 3줄 (주제: ci, db) — switchboard rules list --kind tip"
	d := postMail(t, s, bossToken, internalBossRole, "/api/messages", map[string]any{"recipient": "team/worker", "subject": "task", "body": "do the thing\n", "idem": "k1"})
	if d.Body != want {
		t.Fatalf("directive body:\n%s", d.Body)
	}
	if again := postMail(t, s, bossToken, internalBossRole, "/api/messages", map[string]any{"recipient": "team/worker", "subject": "task", "body": "do the thing\n", "idem": "k1"}); again.ID != d.ID || again.Body != want {
		t.Fatalf("retry: first %d, again %+v", d.ID, again)
	}
	if n := postMail(t, s, bossToken, internalBossRole, "/api/messages", map[string]any{"recipient": "team/worker", "subject": "fyi", "body": "note", "no_reply": true}); n.Body != "note" {
		t.Fatalf("no_reply mail: %q", n.Body)
	}
	up := postMail(t, s, workerToken, "team/worker", "/api/messages", map[string]any{"recipient": internalBossRole, "subject": "q", "body": "question"})
	if up.Body != "question" {
		t.Fatalf("worker mail: %q", up.Body)
	}
	if rep := postMail(t, s, bossToken, internalBossRole, "/api/messages/"+itoa(up.ID)+"/reply", map[string]any{"body": "answer"}); rep.Body != "answer" {
		t.Fatalf("reply: %q", rep.Body)
	}
	s.rulesFooterOff = true
	if off := postMail(t, s, bossToken, internalBossRole, "/api/messages", map[string]any{"recipient": "team/worker", "subject": "task 2", "body": "x"}); off.Body != "x" {
		t.Fatalf("footer off: %q", off.Body)
	}
}
