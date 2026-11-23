package server

import (
	"strings"
	"testing"
)

// inbox and outbox page with --limit and --before (#9).
func TestCLIListPaging(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "a"}, "a")
	h.configure([]string{"--profile", "b"}, "b")
	for _, s := range []string{"one", "two", "three"} {
		h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", s, "--body", "x") // #1..#3
	}
	for _, box := range []string{"inbox", "outbox"} {
		profile := map[string]string{"inbox": "b", "outbox": "a"}[box]
		first := h.mustRun(nil, "--profile", profile, box, "--limit", "2")
		if !strings.Contains(first, "three") || !strings.Contains(first, "two") || strings.Contains(first, "one") {
			t.Fatalf("%s --limit 2:\n%s", box, first)
		}
		next := h.mustRun(nil, "--profile", profile, box, "--limit", "2", "--before", "2")
		if !strings.Contains(next, "one") || strings.Contains(next, "two") {
			t.Fatalf("%s --before 2:\n%s", box, next)
		}
	}
	if r := h.run(nil, "--profile", "b", "inbox", "--limit", "0"); r.code == 0 {
		t.Fatal("--limit 0 accepted")
	}
}
