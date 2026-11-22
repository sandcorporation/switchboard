package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// ledgerCLI is a CLI harness with a boss role (a, profile boss) and a role
// that is no boss (b, profile worker); b owns one job and one schedule.
type ledgerCLI struct {
	*cliHarness
	boss, worker []string
	jid, sid     string
}

func newLedgerCLI(t *testing.T) ledgerCLI {
	t.Helper()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	w := callAs(h.s, "POST", "/api/jobs", "secret-b", "b", map[string]any{"machine": "m1", "label": "build", "command": "make secret-cmd", "cwd": "/w"})
	var j wire.Job
	_ = json.Unmarshal(w.Body.Bytes(), &j)
	w = callAs(h.s, "POST", "/api/schedules", "secret-b", "b", map[string]any{"machine": "m1", "kind": "cron", "cron": "0 9 * * *", "tz": "UTC", "prompt": "secret prompt", "label": "daily"})
	var sc wire.Schedule
	_ = json.Unmarshal(w.Body.Bytes(), &sc)
	if j.ID == 0 || sc.ID == 0 {
		t.Fatalf("setup: job %d schedule %d", j.ID, sc.ID)
	}
	return ledgerCLI{cliHarness: h, boss: []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}, worker: []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"},
		jid: strconv.FormatInt(j.ID, 10), sid: strconv.FormatInt(sc.ID, 10)}
}

// `jobs show ID` (boss) and `schedule show ID` (owner or boss) print the
// command or prompt in full; `--json` is the server's object. A boss role's
// `schedule list --owner` leaves the prompt out.
func TestLedgerShowCommands(t *testing.T) {
	t.Parallel()
	h := newLedgerCLI(t)
	if out := h.mustRun(h.boss, "jobs", "show", h.jid); !strings.Contains(out, "command  make secret-cmd") || !strings.Contains(out, "cwd      /w") {
		t.Fatalf("jobs show: %s", out)
	}
	var j wire.Job
	if err := json.Unmarshal([]byte(h.mustRun(h.boss, "jobs", "show", h.jid, "--json")), &j); err != nil || j.Command != "make secret-cmd" {
		t.Fatalf("jobs show --json: %v %+v", err, j)
	}
	for _, who := range [][]string{h.boss, h.worker} {
		if out := h.mustRun(who, "schedule", "show", h.sid); !strings.Contains(out, "prompt   secret prompt") {
			t.Fatalf("schedule show: %s", out)
		}
	}
	if r := h.run(h.worker, "jobs", "show", h.jid); r.code == 0 || !strings.Contains(r.stderr, "403") {
		t.Fatalf("jobs show as a non-boss: %+v", r)
	}
	if out := h.mustRun(h.boss, "schedule", "list", "--owner", "b"); strings.Contains(out, "secret") || !strings.Contains(out, "daily") {
		t.Fatalf("boss schedule list: %s", out)
	}
	if out := h.mustRun(h.worker, "schedule", "list", "--json"); !strings.Contains(out, "secret prompt") {
		t.Fatalf("own schedule list: %s", out)
	}
}

// `schedule pause ID [--reason]` and `schedule resume ID [--fire|--drop]`:
// the owner and a boss role over it; a once schedule whose time passed
// while paused resumes only with --fire or --drop (exit 2 without, one
// line); a role outside gets an error.
func TestSchedulePauseCommands(t *testing.T) {
	t.Parallel()
	h := newLedgerCLI(t)
	if out := h.mustRun(h.boss, "schedule", "pause", h.sid, "--reason", "noisy"); !strings.Contains(out, "paused by 역할 a") {
		t.Fatalf("boss pause: %s", out)
	}
	if out := h.mustRun(h.worker, "schedule", "show", h.sid); !strings.Contains(out, "by 역할 a") || !strings.Contains(out, "reason   noisy") {
		t.Fatalf("show paused: %s", out)
	}
	if out := h.mustRun(h.worker, "schedule", "resume", h.sid, "--json"); !strings.Contains(out, `"state":"active"`) {
		t.Fatalf("owner resume: %s", out)
	}
	w := callAs(h.s, "POST", "/api/schedules", "secret-b", "b", map[string]any{"machine": "m1", "kind": "once", "at": time.Now().Add(time.Hour), "prompt": "x", "label": "once"})
	var once wire.Schedule
	_ = json.Unmarshal(w.Body.Bytes(), &once)
	oid := strconv.FormatInt(once.ID, 10)
	h.mustRun(h.worker, "schedule", "pause", oid)
	if _, err := h.s.db.Exec(`UPDATE schedules SET next_fire_at=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), once.ID); err != nil {
		t.Fatal(err)
	}
	if out := h.mustRun(h.worker, "schedule", "list"); !strings.Contains(out, "(time passed)") {
		t.Fatalf("list passed: %s", out)
	}
	r := h.run(h.worker, "schedule", "resume", oid)
	if r.code != 2 || !strings.Contains(r.stderr, "--fire") || !strings.Contains(r.stderr, "--drop") {
		t.Fatalf("resume without a choice: %+v", r)
	}
	if r := h.run(h.worker, "schedule", "resume", oid, "--fire", "--drop"); r.code != 2 {
		t.Fatalf("both: %+v", r)
	}
	if out := h.mustRun(h.worker, "schedule", "resume", oid, "--drop"); !strings.Contains(out, "ended") {
		t.Fatalf("drop: %s", out)
	}
	w = callAs(h.s, "POST", "/api/schedules", "secret-a", "a", map[string]any{"machine": "m1", "kind": "cron", "cron": "0 9 * * *", "tz": "UTC", "prompt": "boss own", "label": "own"})
	var own wire.Schedule
	_ = json.Unmarshal(w.Body.Bytes(), &own)
	if r := h.run(h.worker, "schedule", "pause", strconv.FormatInt(own.ID, 10)); r.code == 0 || !strings.Contains(r.stderr, "404") {
		t.Fatalf("a role outside: %+v", r)
	}
}
