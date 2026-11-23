package server

import (
	"strings"
	"testing"
)

// The CLI side of #483: complete <answer> --left by the directive's sender
// records into the directive's ledger, and a refused --left prints the
// server's whole reason on stderr and exits non-zero.
func TestCLICompleteAnswerLeft(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "sender"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	if w := call(h.s, "POST", "/api/messages", "secret-a", map[string]string{"recipient": "b", "subject": "do it", "body": "x"}, ""); w.Code != 201 {
		t.Fatal(w.Body)
	}
	if w := call(h.s, "POST", "/api/messages/1/reply", "secret-b", map[string]string{"body": "half done"}, ""); w.Code != 201 {
		t.Fatal(w.Body)
	}
	h.mustRun(nil, "--profile", "sender", "complete", "2", "--left", "rename", "--left", "docs")
	got := leftRows(t, h.s)
	if strings.Join(got, "\n") != "1 item open a rename\n1 item open a docs" {
		t.Fatalf("ledger: %q", got)
	}

	r := h.run(nil, "--profile", "worker", "complete", "1", "--left", "late")
	if r.code == 0 || !strings.Contains(r.stderr, `server returned 400 Bad Request: --left goes to a directive's ledger`) || !strings.Contains(r.stderr, `left add 1 "ITEM"`) {
		t.Fatalf("refused complete: exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "" {
		t.Fatalf("refused complete printed on stdout: %s", r.stdout)
	}
}
