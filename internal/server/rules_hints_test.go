package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func mineAs(t *testing.T, s *Server, token, role, etag string) (*httptest.ResponseRecorder, wire.RuleHints) {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/rules/mine", &bytes.Buffer{})
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Switchboard-Role", role)
	if etag != "" {
		r.Header.Set("If-None-Match", etag)
	}
	w := httptest.NewRecorder()
	s.api(w, r)
	var h wire.RuleHints
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
			t.Fatal(err)
		}
	}
	return w, h
}

// hitOf is what a session hook does with mine: the rows whose conditions
// hold for a tool call.
func hitOf(h wire.RuleHints, call wire.ToolCall) []int64 {
	out := []int64{}
	for _, r := range h.Rules {
		if r.Match != nil && r.Match.Hits(call) {
			out = append(out, r.ID)
		}
	}
	return out
}

// TestRuleHints (#408 T2): conditions are checked when a row goes in; mine
// gives a role its rows in force without a device, with an ETag; a tool
// call meets a row's conditions; hits count the caller's rows only and full
// readers see the count.
func TestRuleHints(t *testing.T) {
	t.Parallel()
	s := rulesServer(t)
	bad := ruleBody("decision", "service:team", "release", "x", map[string]any{"new_topic": true, "match": map[string]any{"command": "git push (--force"}})
	if w := addRuleAs(t, s, bossToken, internalBossRole, bad, 400); !bytes.Contains(w.Body.Bytes(), []byte("match command")) {
		t.Fatalf("bad regexp: %s", w.Body)
	}
	push := addRule(t, s, ruleBody("decision", "service:team", "release", "no force push\n- use --force-with-lease", map[string]any{
		"new_topic": true, "match": map[string]any{"tool": "Bash", "command": `git push .*--force(\s|$)`}}))
	mig := addRule(t, s, ruleBody("tip", "service:team", "db", "migrations only add", map[string]any{
		"new_topic": true, "match": map[string]any{"tool": "Edit*", "path": "**/migrations/*.sql"}}))
	plain := addRule(t, s, ruleBody("tip", "service:team", "ci", "rerun once", map[string]any{"new_topic": true}))
	addRule(t, s, ruleBody("decision", "service:team", "lint", "gofmt", map[string]any{"new_topic": true, "device": "pre-commit hook", "match": map[string]any{"tool": "Bash"}}))
	other := addRule(t, s, ruleBody("decision", "service:partner-a", "release", "other", map[string]any{"match": map[string]any{"tool": "Bash"}}))

	w, h := mineAs(t, s, workerToken, "team/worker", "")
	if w.Code != 200 || w.Header().Get("ETag") == "" {
		t.Fatalf("mine: %d %v", w.Code, w.Header())
	}
	var ids []int64
	for _, r := range h.Rules {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{push.ID, mig.ID, plain.ID}) {
		t.Fatalf("mine rows: %v", ids)
	}
	for _, r := range h.Rules {
		if r.ID == push.ID && (r.Summary != "no force push" || r.Match == nil || r.Match.Tool != "Bash") {
			t.Fatalf("mine row: %+v", r)
		}
	}
	if w2, _ := mineAs(t, s, workerToken, "team/worker", w.Header().Get("ETag")); w2.Code != 304 {
		t.Fatalf("If-None-Match: %d", w2.Code)
	}

	for _, c := range []struct {
		call wire.ToolCall
		want []int64
	}{
		{wire.ToolCall{Tool: "Bash", Command: "git push origin x --force"}, []int64{push.ID}},
		{wire.ToolCall{Tool: "Bash", Command: "git push --force-with-lease"}, []int64{}},
		{wire.ToolCall{Tool: "Edit", Path: "/repo/internal/store/migrations/001.sql"}, []int64{mig.ID}},
		{wire.ToolCall{Tool: "Edit", Path: "migrations/a/001.sql"}, []int64{}},
		{wire.ToolCall{Tool: "Read", Path: "/repo/migrations/001.sql"}, []int64{}},
	} {
		if got := hitOf(h, c.call); !slices.Equal(got, c.want) {
			t.Errorf("%+v: hit %v, want %v", c.call, got, c.want)
		}
	}

	hit := func(ids ...int64) []int64 {
		w := callAs(s, "POST", "/api/rules/hits", workerToken, "team/worker", wire.RuleHitsRequest{IDs: ids})
		var res wire.RuleHitsResult
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &res) != nil {
			t.Fatalf("hits: %d %s", w.Code, w.Body)
		}
		return res.Counted
	}
	if got := hit(push.ID, push.ID, other.ID, 999); !slices.Equal(got, []int64{push.ID}) {
		t.Fatalf("hits counted %v", got)
	}
	hit(push.ID)
	if d := showRule(t, s, bossToken, internalBossRole, push.ID, 200); d.Hits != 2 || d.LastHitAt == nil {
		t.Fatalf("hits in show: %+v", d.Rule)
	}
	if d := showRule(t, s, workerToken, "team/worker", push.ID, 200); d.Hits != 0 {
		t.Fatalf("a limited reader sees hits: %+v", d.Rule)
	}
	for _, r := range listRulesAs(t, s, bossToken, internalBossRole, "?topic=release").Rules {
		if r.ID == push.ID && r.Hits != 2 {
			t.Fatalf("hits in list: %+v", r)
		}
	}
	if d := showRule(t, s, bossToken, internalBossRole, other.ID, 200); d.Hits != 0 {
		t.Fatalf("out of scope counted: %+v", d.Rule)
	}
	// A new row changes the set and its ETag.
	addRule(t, s, ruleBody("tip", "service:team", "ci", "read the log first", map[string]any{"alongside": true}))
	if w3, _ := mineAs(t, s, workerToken, "team/worker", w.Header().Get("ETag")); w3.Code != 200 {
		t.Fatalf("after a change: %d", w3.Code)
	}
}
