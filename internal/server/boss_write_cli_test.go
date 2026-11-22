package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The boss write commands (#399) through the real CLI: a boss role (a)
// closes mail, stops and removes jobs, revokes and restores a role, sets a
// question setting and makes the terminal tab's actions; writes that are
// hard to undo do nothing without --yes (exit 2, one line); a role that is
// no boss (b) gets 403.
func TestBossWriteCommands(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) {
		s.bossRoles = map[string]bool{"a": true}
		s.hosting = true
	})
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	boss := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}
	worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}

	w := callAs(h.s, "POST", "/api/messages", "secret-b", "b", map[string]string{"recipient": "b", "subject": "directive", "body": "x"})
	var m wire.Message
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	mid := strconv.FormatInt(m.ID, 10)
	w = callAs(h.s, "POST", "/api/jobs", "secret-b", "b", map[string]any{"machine": "m1", "kind": "command", "label": "build", "command": "make", "cwd": "/w"})
	var job wire.Job
	_ = json.Unmarshal(w.Body.Bytes(), &job)
	jid := strconv.FormatInt(job.ID, 10)

	for _, args := range [][]string{{"mail", "close", mid}, {"jobs", "stop", jid}, {"roles", "restore", "b"}, {"settings", "set", "team/*", "--can-ask", "on"}, {"host", "limit", "on"}} {
		if r := h.run(worker, args...); r.code == 0 || !strings.Contains(r.stderr, "403") {
			t.Fatalf("%v as a non-boss: %+v", args, r)
		}
	}

	if out := h.mustRun(boss, "mail", "close", mid); !strings.Contains(out, "closed #"+mid) {
		t.Fatalf("mail close: %s", out)
	}
	if out := h.mustRun(boss, "jobs", "stop", jid); !strings.Contains(out, "stop requested") {
		t.Fatalf("jobs stop: %s", out)
	}
	if _, err := h.s.db.Exec(`UPDATE jobs SET state='stopped', ended_at=1 WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}

	// Without --yes: one line, exit 2, nothing done.
	for _, args := range [][]string{{"jobs", "rm", jid}, {"roles", "revoke", "b"}, {"settings", "rm", "team/*"}, {"host", "revoke-node", "pc"}} {
		r := h.run(boss, args...)
		if r.code != 2 || !strings.HasPrefix(r.stdout, "would ") || strings.Count(strings.TrimSpace(r.stdout), "\n") != 0 {
			t.Fatalf("%v without --yes: %+v", args, r)
		}
	}
	if _, err := h.s.jobs().Get(job.ID); err != nil {
		t.Fatalf("job removed without --yes: %v", err)
	}
	if out := h.mustRun(boss, "jobs", "rm", jid, "--yes"); !strings.Contains(out, "removed job #"+jid) {
		t.Fatalf("jobs rm: %s", out)
	}
	if out := h.mustRun(boss, "roles", "revoke", "b", "--yes"); !strings.Contains(out, "revoked b") {
		t.Fatalf("roles revoke: %s", out)
	}
	if r := h.run(worker, "inbox"); r.code == 0 {
		t.Fatalf("revoked role still works: %+v", r)
	}
	h.mustRun(boss, "roles", "restore", "b")
	if out := h.mustRun(boss, "settings", "set", "team/*", "--can-ask", "on", "--watchers", "a"); !strings.Contains(out, "can_ask=true") || !strings.Contains(out, "watchers=a") {
		t.Fatalf("settings set: %s", out)
	}
	// Options left out keep the current value.
	if out := h.mustRun(boss, "settings", "set", "team/*", "--discord", "on"); !strings.Contains(out, "can_ask=true discord=true watchers=a") {
		t.Fatalf("settings set keeps: %s", out)
	}

	// The terminal tab.
	if _, err := h.s.db.Exec(`INSERT INTO nodes(name,token_hash,created_at) VALUES('pc','h',1)`); err != nil {
		t.Fatal(err)
	}
	rec, err := h.s.hostingService().Create(wire.HostedSessionCreate{Node: "pc", Base: "b", Profile: "p", Directory: "/work/app"})
	if err != nil {
		t.Fatal(err)
	}
	auto, err := h.s.hostingService().CreateAuto(rec.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	aid := strconv.FormatInt(auto.ID, 10)
	h.mustRun(boss, "host", "set", rec.ID, "--label", "renamed", "--class", "review")
	h.mustRun(boss, "host", "stop", rec.ID)
	if got, _ := h.s.hostingService().Get(rec.ID); got.Label != "renamed" || got.Class != "review" || got.Desired != "stopped" {
		t.Fatalf("host set/stop: %+v", got)
	}
	if out := h.mustRun(boss, "host", "limit", "on"); !strings.Contains(out, "limit on") {
		t.Fatalf("host limit on: %s", out)
	}
	if out := h.mustRun(boss, "host", "limit"); !strings.Contains(out, "true") {
		t.Fatalf("host limit: %s", out)
	}
	h.mustRun(boss, "host", "auto", "pause", aid)
	if out := h.mustRun(boss, "host", "auto", "show", aid); !strings.Contains(out, "paused") {
		t.Fatalf("host auto show: %s", out)
	}
	if r := h.run(boss, "host", "auto", "rm", aid); r.code != 2 {
		t.Fatalf("host auto rm without --yes: %+v", r)
	}
	h.mustRun(boss, "host", "auto", "rm", aid, "--yes")
	if r := h.run(boss, "host", "remove", rec.ID); r.code != 2 || !strings.HasPrefix(r.stdout, "would remove") {
		t.Fatalf("host remove without --yes: %+v", r)
	}
	h.mustRun(boss, "host", "remove", rec.ID, "--yes")
	if _, err := h.s.hostingService().Get(rec.ID); err == nil {
		t.Fatalf("session not removed")
	}
	h.mustRun(boss, "host", "revoke-node", "pc", "--yes")

	var n int
	_ = h.s.db.QueryRow(`SELECT COUNT(*) FROM boss_writes WHERE role='a' AND status=200`).Scan(&n)
	if n < 12 {
		t.Fatalf("boss writes recorded: %d", n)
	}
}
