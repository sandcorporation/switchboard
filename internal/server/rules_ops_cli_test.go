package server

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/rules"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// `rules proposer|approve|reject` end to end (#409 T1): the boss (a) turns
// b into a proposer; b's tip on its own service is in force at once, a tag
// tip waits as a proposal that b does not list; a approves one through the
// replace flow and rejects another.
func TestRulesProposalCLI(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	boss, worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}, []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}

	h.mustRun(boss, "rules", "add", "--kind", "tip", "--scope", "tag:ci-tips", "--topic", "ci", "--new-topic", "--source", "x", "rerun once")
	if r := h.run(worker, "rules", "add", "--kind", "tip", "--scope", "service:b", "--topic", "ci", "--source", "x", "cache"); r.code == 0 || !strings.Contains(r.stderr, "403") {
		t.Fatalf("add before proposer: %+v", r)
	}
	if out := h.mustRun(boss, "rules", "proposer", "b", "on"); !strings.HasPrefix(out, "b (켠 이 a ") {
		t.Fatalf("proposer on: %q", out)
	}
	if out := h.mustRun(worker, "rules", "add", "--kind", "tip", "--scope", "service:b", "--topic", "ci", "--source", "x", "cache"); out != "#2 요령 ci [service:b] cache" {
		t.Fatalf("own-service tip: %q", out)
	}
	out := h.mustRun(worker, "rules", "add", "--kind", "tip", "--scope", "tag:ci-tips", "--topic", "ci", "--source", "x", "pin the runner")
	if !strings.Contains(out, "#3 요령 ci [tag:ci-tips] (제안) pin the runner") || !strings.Contains(out, "rules approve 3") {
		t.Fatalf("tag tip: %q", out)
	}
	if out = h.mustRun(worker, "rules", "list", "--kind", "tip"); strings.Contains(out, "pin the runner") {
		t.Fatalf("worker lists the proposal: %q", out)
	}
	if out = h.mustRun(boss, "rules", "list", "--kind", "tip"); !strings.Contains(out, "승인 기다리는 제안 1건") {
		t.Fatalf("boss list: %q", out)
	}
	if out = h.mustRun(boss, "rules", "list", "--proposed"); out != "#3 요령 ci [tag:ci-tips] (제안) pin the runner" {
		t.Fatalf("list --proposed: %q", out)
	}
	if r := h.run(boss, "rules", "approve", "3"); r.code == 0 || !strings.Contains(r.stdout, "--replace 1") {
		t.Fatalf("approve without a choice: %+v", r)
	}
	if out = h.mustRun(boss, "rules", "approve", "3", "--replace", "1"); !strings.Contains(out, "#3 요령 ci [tag:ci-tips] pin the runner") || !strings.Contains(out, "#1 대체됨") {
		t.Fatalf("approve: %q", out)
	}
	h.mustRun(worker, "rules", "add", "--kind", "tip", "--scope", "all", "--topic", "ci", "--source", "x", "skip lint")
	if out = h.mustRun(boss, "rules", "reject", "4", "--reason", "lint stays"); out != "#4 요령 ci [all] (거절됨) skip lint" {
		t.Fatalf("reject: %q", out)
	}
	if out = h.mustRun(boss, "rules", "proposer", "b", "off"); out != "제안 역할 없음" {
		t.Fatalf("proposer off: %q", out)
	}
}

// `rules unclassified|classify` end to end (#409 T2): b sees its own
// unclassified answers, marks one once and one as a rule; the boss sees
// the count per role.
func TestRulesClassifyCLI(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	boss, worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}, []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	for _, a := range []wire.AnswerRef{{ID: "q_one", Asker: "b", Header: "배포", AnsweredAt: at}, {ID: "q_two", Asker: "b", Header: "캐시", AnsweredAt: at.Add(time.Minute)}} {
		if err := store.InTx(t.Context(), h.s.db, func(tx *sql.Tx) error { return rules.RecordAnswer(tx, a) }); err != nil {
			t.Fatal(err)
		}
	}
	if out := h.mustRun(worker, "rules", "unclassified"); strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "q_one b ") || !strings.HasSuffix(out, " 캐시") {
		t.Fatalf("unclassified: %q", out)
	}
	if out := h.mustRun(boss, "rules", "unclassified"); !strings.HasSuffix(out, "  b 2건") {
		t.Fatalf("boss unclassified: %q", out)
	}
	if r := h.run(worker, "rules", "classify", "q_one"); r.code == 0 || !strings.Contains(r.stderr, "--rule N or --once") {
		t.Fatalf("classify without a class: %+v", r)
	}
	if out := h.mustRun(worker, "rules", "classify", "q_one", "--once"); out != "q_one 이번에만" {
		t.Fatalf("classify --once: %q", out)
	}
	h.mustRun(boss, "rules", "add", "--kind", "tip", "--scope", "service:b", "--topic", "ci", "--new-topic", "--source", "x", "cache it")
	if out := h.mustRun(worker, "rules", "classify", "q_two", "--rule", "1"); out != "q_two 규칙 #1" {
		t.Fatalf("classify --rule: %q", out)
	}
	if out := h.mustRun(worker, "rules", "unclassified"); out != "미분류 없음" {
		t.Fatalf("after: %q", out)
	}
}
