package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// `rules …` end to end: a global boss (a) adds, meets the 409 and replaces,
// shows the history, voids and sets tags; a role that is no boss (b) reads
// its own scope only, without the scope or history.
func TestRulesCLI(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	boss, worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}, []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}

	out := h.mustRun(boss, "rules", "add", "--kind", "decision", "--scope", "service:b", "--topic", "release", "--new-topic", "--source", "panel answer", "ship on green")
	if !strings.HasPrefix(out, "#1 결정 release [service:b] ship on green") {
		t.Fatalf("add: %q", out)
	}
	if r := h.run(boss, "rules", "add", "--kind", "decision", "--scope", "service:b", "--topic", "nope", "--source", "x", "y"); r.code == 0 || !strings.Contains(r.stderr, "--new-topic") {
		t.Fatalf("add on an unknown topic: %+v", r)
	}
	r := h.run(boss, "rules", "add", "--kind", "decision", "--scope", "service:b", "--topic", "release", "--source", "x", "ship on Fridays")
	if r.code == 0 || !strings.Contains(r.stdout, "#1 결정 release") || !strings.Contains(r.stdout, "--replace 1") {
		t.Fatalf("conflict: %+v", r)
	}
	out = h.mustRun(boss, "rules", "add", "--kind", "decision", "--scope", "service:b", "--topic", "release", "--source", "x", "--replace", "1", "ship on Fridays")
	if !strings.Contains(out, "#2 결정 release") || !strings.Contains(out, "#1 대체됨") {
		t.Fatalf("replace: %q", out)
	}
	h.mustRun(boss, "rules", "add", "--kind", "tip", "--scope", "tag:ci-tips", "--topic", "ci", "--new-topic", "--source", "x", "rerun once")
	h.mustRun(boss, "rules", "add", "--kind", "tip", "--scope", "service:other", "--topic", "ci", "--source", "x", "other service")

	if out = h.mustRun(boss, "rules", "show", "1"); !strings.Contains(out, "(대체됨 → #2)") || !strings.Contains(out, "범위 service:b") || !strings.Contains(out, "이력\n  #2") {
		t.Fatalf("show 1: %q", out)
	}
	if out = h.mustRun(boss, "rules", "list", "--all"); strings.Count(out, "\n") != 3 {
		t.Fatalf("list --all: %q", out)
	}
	var l wire.RuleList
	if err := json.Unmarshal([]byte(h.mustRun(boss, "rules", "list", "--topic", "release", "--json")), &l); err != nil || !l.Full || len(l.Rules) != 1 || l.Rules[0].ID != 2 {
		t.Fatalf("list --json: %v %+v", err, l)
	}

	if out = h.mustRun(worker, "rules"); out != "#2 결정 release ship on Fridays" {
		t.Fatalf("worker list: %q", out)
	}
	if out = h.mustRun(boss, "rules", "tag", "set", "b", "ci-tips"); out != "b: ci-tips" {
		t.Fatalf("tag set: %q", out)
	}
	if out = h.mustRun(worker, "rules", "list"); !strings.Contains(out, "#3 요령 ci rerun once") || strings.Contains(out, "other service") {
		t.Fatalf("worker list with the tag: %q", out)
	}
	if out = h.mustRun(boss, "rules", "tag", "role", "b", "--", "-ci-tips"); out != "b -ci-tips → (태그 없음)" {
		t.Fatalf("tag role: %q", out)
	}
	if out = h.mustRun(worker, "rules", "list"); strings.Contains(out, "rerun once") {
		t.Fatalf("worker list without the tag: %q", out)
	}
	if out = h.mustRun(boss, "rules", "tag", "list"); out != "b: ci-tips\nb -ci-tips → (태그 없음)" {
		t.Fatalf("tag list: %q", out)
	}
	if out = h.mustRun(worker, "rules", "show", "2"); out != "#2 결정 release\n\nship on Fridays" {
		t.Fatalf("worker show: %q", out)
	}
	if r := h.run(worker, "rules", "show", "4"); r.code == 0 || !strings.Contains(r.stderr, "404") {
		t.Fatalf("worker show out of scope: %+v", r)
	}
	if r := h.run(worker, "rules", "void", "2"); r.code == 0 || !strings.Contains(r.stderr, "403") {
		t.Fatalf("worker void: %+v", r)
	}
	if out = h.mustRun(worker, "rules", "topics"); out != "release 1" {
		t.Fatalf("worker topics: %q", out)
	}
	if out = h.mustRun(boss, "rules", "void", "2"); !strings.Contains(out, "#2 결정 release [service:b] (무효)") {
		t.Fatalf("void: %q", out)
	}
	if out = h.mustRun(worker, "rules"); out != "규칙 없음" {
		t.Fatalf("worker list after void: %q", out)
	}

	// #408 T1: a Korean topic, a text of several lines (list shows the
	// first, show the whole), and a usage error's reason as the last line.
	h.mustRun(boss, "rules", "add", "--kind", "tip", "--scope", "service:b", "--topic", "배포", "--new-topic", "--source", "x", "green first\n- then tag\n- then notes")
	if out = h.mustRun(worker, "rules", "list", "--topic", "배포"); out != "#5 요령 배포 green first" {
		t.Fatalf("korean topic list: %q", out)
	}
	if out = h.mustRun(worker, "rules", "show", "5"); out != "#5 요령 배포\n\ngreen first\n- then tag\n- then notes" {
		t.Fatalf("multi-line show: %q", out)
	}
	r = h.run(boss, "rules", "add", "--kind", "tip", "--scope", "all", "--topic", "배 포", "--source", "x", "y")
	lines := strings.Split(strings.TrimSpace(r.stderr), "\n")
	if r.code != 2 || len(lines) < 2 || !strings.Contains(lines[len(lines)-1], "topic \"배 포\"") || !strings.Contains(lines[0], "--help") {
		t.Fatalf("usage error: %+v", r)
	}
}
