package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/agent"
)

// Disabled local and project leftovers of the old plugin are one line;
// user-scope entries and enabled ones stay one line each (#485).
func TestPluginStatusFoldsDisabledOldEntries(t *testing.T) {
	t.Parallel()
	st := agent.PluginState{Agent: "claude", Available: true, Where: "/u", Want: "0.54.0", Installed: "0.54.0", Enabled: true,
		Legacy: []agent.LegacyPlugin{
			{ID: "old@m", Scope: "user", Version: "1", Enabled: false},
			{ID: "old@m", Scope: "local", Project: "/p/one", Version: "1", Enabled: false},
			{ID: "old@m", Scope: "local", Project: "/p/two", Version: "1", Enabled: false},
			{ID: "old@m", Scope: "project", Project: "/p/three", Version: "1", Enabled: false},
			{ID: "old@m", Scope: "local", Project: "/p/live", Version: "1", Enabled: true},
		}}
	var b bytes.Buffer
	printPluginState(&b, st)
	out := b.String()
	if n := strings.Count(out, "\n"); n != 4 {
		t.Fatalf("%d lines, want 4:\n%s", n, out)
	}
	for _, want := range []string{"old old@m 1 (user): disabled", "(local /p/live): enabled", "3 disabled old entries"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/p/one") || strings.Contains(out, "/p/three") {
		t.Fatalf("a disabled local or project entry is listed:\n%s", out)
	}
}
