package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pluginLog is a fresh SWITCHBOARD_TEST_PLUGIN_LOG and a reader of its lines.
func pluginLog(t *testing.T) (string, func() []string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "plugin.log")
	return log, func() []string {
		b, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

// The first run of a new version installs the plugin once, quietly, before
// the command; the same version reads only the marker (#477).
func TestCLIPluginInstallsOnTheFirstRunOfAVersion(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	log, lines := pluginLog(t)
	env := []string{"SWITCHBOARD_TEST_PLUGIN_LOG=" + log}
	h.mustRun(env, "config", "set", "--endpoint", h.url, "--role", "a", "--token-file", h.tokens["a"])
	if got := lines(); len(got) != 1 || got[0] != "install profiles=0 flag=true" {
		t.Fatalf("first run: %q", got)
	}
	marker := filepath.Join(h.dir, "plugin-version")
	b, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		t.Fatalf("marker %q, %v", b, err)
	}
	h.mustRun(env, "inbox")
	if got := lines(); len(got) != 1 {
		t.Fatalf("same version installed again: %q", got)
	}
	if err := os.WriteFile(marker, []byte("v0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.mustRun(env, "inbox")
	if got := lines(); len(got) != 2 {
		t.Fatalf("new version not installed: %q", got)
	}
	if b2, _ := os.ReadFile(marker); string(b2) != string(b) {
		t.Fatalf("marker after the install %q, want %q", b2, b)
	}
}

// The line goes to stderr only: --json output stays clean, and a failed
// install leaves the command running and says how to retry.
func TestCLIPluginLineIsStderrAndAFailureRunsTheCommand(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure(nil, "a")
	log, lines := pluginLog(t)
	if err := os.Remove(filepath.Join(h.dir, "plugin-version")); err != nil {
		t.Fatal(err)
	}
	r := h.run([]string{"SWITCHBOARD_TEST_PLUGIN_LOG=" + log, "SWITCHBOARD_TEST_PLUGIN_RESULT=changed"}, "inbox", "--json")
	if r.code != 0 || strings.Contains(r.stdout, "plugin") || !strings.HasPrefix(strings.TrimSpace(r.stdout), "[") {
		t.Fatalf("inbox --json: %+v", r)
	}
	if strings.Count(r.stderr, "\n") != 1 || !strings.Contains(r.stderr, "plugin: fake: installed fake 1.0.0") {
		t.Fatalf("stderr %q", r.stderr)
	}
	if err := os.Remove(filepath.Join(h.dir, "plugin-version")); err != nil {
		t.Fatal(err)
	}
	r = h.run([]string{"SWITCHBOARD_TEST_PLUGIN_LOG=" + log, "SWITCHBOARD_TEST_PLUGIN_RESULT=fail"}, "inbox")
	if r.code != 0 || !strings.Contains(r.stderr, "install failed: boom") || !strings.Contains(r.stderr, "plugin install' to retry") {
		t.Fatalf("failed install: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "plugin-version")); err != nil {
		t.Fatalf("a failed install is not marked, so every command would retry it: %v", err)
	}
	if got := lines(); len(got) != 2 {
		t.Fatalf("calls %q", got)
	}
}

// SWITCHBOARD_PLUGIN=off, --no-plugin and the commands the plugin itself
// runs (version) never install on their own.
func TestCLIPluginOffSwitches(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	log, lines := pluginLog(t)
	env := []string{"SWITCHBOARD_TEST_PLUGIN_LOG=" + log}
	h.mustRun(append(env, "SWITCHBOARD_PLUGIN=off"), "config", "set", "--endpoint", h.url, "--role", "a", "--token-file", h.tokens["a"])
	h.mustRun(env, "--no-plugin", "inbox")
	h.mustRun(env, "version")
	if got := lines(); got != nil {
		t.Fatalf("installed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "plugin-version")); !os.IsNotExist(err) {
		t.Fatalf("marker written without an install: %v", err)
	}
	// Asked for, it installs even with the variable set.
	out := h.mustRun(append(env, "SWITCHBOARD_PLUGIN=off"), "plugin", "install")
	h.mustRun(env, "plugin", "status", "--json")
	if got := lines(); len(got) != 2 || got[0] != "install profiles=1 flag=false" || got[1] != "status profiles=1 flag=true" {
		t.Fatalf("plugin install/status: %q (out %q)", got, out)
	}
}

// join installs the plugin after the profile is saved; --no-plugin skips it.
func TestCLIJoinInstallsThePluginAfterTheProfile(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) {
		if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('team/boss',?,'now')`, bossToken); err != nil {
			t.Fatal(err)
		}
		s.bossRoles = map[string]bool{"team/boss": true}
	})
	bossFile := filepath.Join(h.dir, "boss.token")
	if err := os.WriteFile(bossFile, []byte(bossToken), 0600); err != nil {
		t.Fatal(err)
	}
	h.mustRun(nil, "--profile", "boss", "config", "set", "--endpoint", h.url, "--role", "team/boss", "--token-file", bossFile)
	code := func() string {
		out := h.mustRun(nil, "--profile", "boss", "invite", "--prefix", "partner-a", "--ttl", "24h")
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(line, "Code:"); ok {
				return strings.TrimSpace(v)
			}
		}
		t.Fatalf("invite output:\n%s", out)
		return ""
	}
	log, lines := pluginLog(t)
	theirs := t.TempDir()
	env := []string{"AGENT_MAIL_CONFIG_DIR=" + theirs, "SWITCHBOARD_TEST_PLUGIN_LOG=" + log}
	h.mustRun(env, "join", h.url, code(), "--role", "partner-a/dev")
	if got := lines(); len(got) != 1 || got[0] != "install profiles=1 flag=true" {
		t.Fatalf("join: %q", got)
	}
	other := t.TempDir()
	h.mustRun([]string{"AGENT_MAIL_CONFIG_DIR=" + other, "SWITCHBOARD_TEST_PLUGIN_LOG=" + log}, "join", h.url, code(), "--role", "partner-a/two", "--no-plugin")
	if got := lines(); len(got) != 1 {
		t.Fatalf("join --no-plugin installed: %q", got)
	}
}
