package server

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/term/termtest"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// A node's sessions run its status command; what it reads reaches the host
// in the state frame and GET /api/admin/nodes lists it per account (#434).
func TestNodeReportsAgentUsage(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	dir := t.TempDir()
	fake := termtest.New()
	attachNodeWith(t, base, token, fake, node.Config{Switchboard: "/opt/sb/switchboard", StatusDir: dir})
	h := createRecord(t, base, cookie, newRecord)
	eventually(t, "launched", func() bool { return len(fake.Launches()) == 1 })
	argv := fake.Launches()[0].Argv
	if i := slices.Index(argv, "--settings"); i < 0 || !strings.Contains(argv[i+1], `"statusLine"`) || !strings.Contains(argv[i+1], h.ID+".json") {
		t.Fatalf("argv %q", argv)
	}
	nodes := func() []wire.NodeView {
		var out []wire.NodeView
		got := adminCall(t, base, "GET", "/api/admin/nodes", cookie, false, "")
		if err := json.Unmarshal([]byte(got.body), &out); err != nil {
			t.Fatalf("%d %s", got.code, got.body)
		}
		return out
	}
	eventually(t, "unknown usage", func() bool {
		n := nodes()
		return len(n) == 1 && len(n[0].Usage) == 1 && n[0].Usage[0].Remaining == nil && n[0].Usage[0].Adapter == agent.Default().Name()
	})
	reset := time.Now().Add(time.Hour).Unix()
	node.StatusLine(agent.Default().Name(), filepath.Join(dir, h.ID+".json"), node.StatusIO{
		Stdin:  strings.NewReader(fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":25,"resets_at":%d}}}`, reset)),
		Stdout: io.Discard, Getenv: func(string) string { return "" }, Home: t.TempDir(),
	})
	eventually(t, "known usage", func() bool {
		n := nodes()
		if len(n) != 1 || len(n[0].Usage) != 1 {
			return false
		}
		u := n[0].Usage[0]
		return u.Remaining != nil && *u.Remaining == 75 && u.ResetsAt != nil && *u.ResetsAt == reset*1000
	})
}

// `node statusline` never fails the agent's status line; its options belong
// to it alone.
func TestNodeStatusLineCLI(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	if code, out, stderr := binaryCLI(t, dbPath, "node", "statusline", "--adapter", "nosuch", "--out", filepath.Join(t.TempDir(), "a.json")); code != 0 || out != "" || stderr != "" {
		t.Fatalf("statusline = %d %q %q", code, out, stderr)
	}
	if code, _, stderr := binaryCLI(t, dbPath, "node", "--host", "http://127.0.0.1:1", "--out", "x"); code != 2 || !strings.Contains(stderr, "node statusline") {
		t.Fatalf("--out elsewhere = %d %s", code, stderr)
	}
}
