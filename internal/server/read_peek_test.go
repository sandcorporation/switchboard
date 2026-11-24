package server

import (
	"encoding/json"
	"testing"
)

// The plugin takes a mail's body with ?peek=1 and marks it read only when the
// session's turn starts with it (#590): the peek leaves the mail pending.
func TestReadPeekLeavesMailPending(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	if c := sendAs(s, "secret-a", "a", "b"); c != 201 && c != 200 {
		t.Fatalf("send: %d", c)
	}
	status := func(path string) string {
		t.Helper()
		w := callAs(s, "GET", path, "secret-b", "b", nil)
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
		}
		var m struct {
			Body   string `json:"body"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.Body != "x" {
			t.Fatalf("GET %s body %q", path, m.Body)
		}
		return m.Status
	}
	for range 2 {
		if st := status("/api/messages/1?peek=1"); st != "pending" {
			t.Fatalf("peek marked the mail %q", st)
		}
	}
	if st := status("/api/messages/1"); st != "read" {
		t.Fatalf("plain read left the mail %q", st)
	}
}
