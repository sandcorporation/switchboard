package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

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
