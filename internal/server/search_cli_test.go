package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// switchboard search (#481): two lines a result, the page cursor, --json,
// the filters as parameters, a server refusal as a failure.
func TestSearchCommand(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	h.configure([]string{"--profile", "boss"}, "a")
	var ids []int64
	for _, body := range []string{"first\nCache TTL is 300s\nend", "cache ttl again"} {
		w := callAs(h.s, "POST", "/api/messages", "secret-b", "b", map[string]string{"recipient": "a", "subject": "cache", "body": body})
		var m wire.Message
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || w.Code != 201 {
			t.Fatalf("send: %d %s", w.Code, w.Body)
		}
		ids = append(ids, m.ID)
	}
	first, second := strconv.FormatInt(ids[0], 10), strconv.FormatInt(ids[1], 10)

	out := h.mustRun(nil, "search", "TTL", "--limit", "1", "--profile", "boss")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "mail #"+second+" ") || !strings.HasSuffix(lines[0], " b → a · read "+second) ||
		lines[1] != "  cache ttl again" || !strings.HasPrefix(lines[2], "더 있음: --after ") {
		t.Fatalf("page 1:\n%s", out)
	}
	after := strings.TrimPrefix(lines[2], "더 있음: --after ")
	out = h.mustRun(nil, "search", "TTL", "--limit", "1", "--after", after, "--profile", "boss")
	if lines := strings.Split(out, "\n"); len(lines) != 2 || !strings.HasSuffix(lines[0], "read "+first) || lines[1] != "  Cache TTL is 300s" {
		t.Fatalf("page 2:\n%s", out)
	}
	if out := h.mustRun(nil, "search", "TTL", "--case", "--kind", "mail", "--kind", "report", "--profile", "boss"); !strings.Contains(out, "read "+first) || strings.Contains(out, "read "+second) {
		t.Fatalf("--case:\n%s", out)
	}
	if out := h.mustRun(nil, "search", `ttl\s+is\s+\d+`, "--regex", "--since", "2000-01-01", "--role", "b", "--profile", "boss"); !strings.Contains(out, "read "+first) || strings.Contains(out, "read "+second) {
		t.Fatalf("--regex:\n%s", out)
	}
	if out := h.mustRun(nil, "search", "TTL", "--until", "2000-01-01", "--profile", "boss"); out != "없음" {
		t.Fatalf("--until: %q", out)
	}
	var res wire.SearchResult
	if err := json.Unmarshal([]byte(h.mustRun(nil, "search", "ttl", "--json", "--profile", "boss")), &res); err != nil || len(res.Hits) != 2 || res.Scope != "all" || res.Hits[0].Open != "read "+second {
		t.Fatalf("--json: %v %+v", err, res)
	}
	if r := h.run(nil, "search", "(", "--regex", "--profile", "boss"); r.code == 0 || !strings.Contains(r.stderr, "400") {
		t.Fatalf("bad regexp: %d %s", r.code, r.stderr)
	}
}
