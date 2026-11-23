package server

import (
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/cli"
)

// Given the ID mail had before a session took it, read, complete, reply,
// close and withdraw print one line naming the ID it has now; --json keeps
// its output and only adds that stderr line. Lists show the old ID (#392).
func TestCLIOldIDOfMovedMail(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "a"}, "a")
	h.configure(nil, "b")
	for _, subject := range []string{"do X", "take back"} {
		h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", subject, "--body", "x")
	}
	// What moveHeld leaves: the mail under a new ID, held_id the sent one.
	if _, err := h.s.db.Exec(`UPDATE messages SET id=id+4,held_id=id WHERE id IN (1,2)`); err != nil {
		t.Fatal(err)
	}
	moved := func(r cliResult, old string, now int64) {
		t.Helper()
		if r.code != 0 || !strings.Contains(r.stderr, cli.MovedNote(old, now)) {
			t.Fatalf("want %q on stderr: %+v", cli.MovedNote(old, now), r)
		}
	}

	w := h.run(nil, "--profile", "a", "withdraw", "2")
	moved(w, "2", 6)
	if strings.TrimSpace(w.stdout) != "#6 withdrawn" {
		t.Fatalf("withdraw: %+v", w)
	}

	r := h.run(nil, "read", "1")
	moved(r, "1", 5)
	if !strings.HasPrefix(r.stdout, "#5 (옛 #1) do X\n") {
		t.Fatalf("read: %+v", r)
	}
	j := h.run(nil, "read", "1", "--json")
	moved(j, "1", 5)
	if !strings.Contains(j.stdout, `"id": 5`) || !strings.Contains(j.stdout, `"held_id": 1`) || strings.Contains(j.stdout, "옛") {
		t.Fatalf("read --json: %+v", j)
	}
	if out := h.mustRun(nil, "--profile", "a", "outbox"); !strings.Contains(out, "#5 (옛 #1) do X") {
		t.Fatalf("outbox: %s", out)
	}

	moved(h.run(nil, "reply", "1", "--body", "on it", "--json"), "1", 5)
	moved(h.run(nil, "complete", "1", "--left-none", "--json"), "1", 5)
	c := h.run(nil, "--profile", "a", "close", "1")
	moved(c, "1", 5)
	if !strings.HasPrefix(c.stdout, "#5 closed") {
		t.Fatalf("close: %+v", c)
	}

	// The ID it has now prints no such line.
	if r := h.run(nil, "read", "5"); r.code != 0 || strings.Contains(r.stderr, "→") {
		t.Fatalf("read 5: %+v", r)
	}
}
