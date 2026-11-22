package claude_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent/claude"
)

const cmd = "/bin/sb node statusline --adapter claude --out /s/a.json"

func TestReportArgsJoinTheSettings(t *testing.T) {
	a := claude.Adapter{}
	line := `{"statusLine":{"type":"command","command":"` + cmd + `"}}`
	skip := a.PermissionSkipArgs(nil)
	for _, c := range []struct {
		name      string
		own, args []string
		want      []string
	}{
		{"alone", nil, []string{"--model", "x"}, []string{"--settings", line}},
		{"with the permission skip", skip, nil, []string{"--dangerously-skip-permissions", "--settings", `{"skipDangerousModePermissionPrompt":true,"statusLine":{"type":"command","command":"` + cmd + `"}}`}},
		{"the record's own settings win", skip, []string{"--settings", "{}"}, skip},
		{"the record's own settings= win", nil, []string{"--settings={}"}, nil},
	} {
		if got := a.ReportArgs(c.own, c.args, cmd); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !slices.Equal(skip, a.PermissionSkipArgs(nil)) {
		t.Error("ReportArgs changed its input")
	}
}

func TestReadUsage(t *testing.T) {
	a := claude.Adapter{}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	none := env(nil)
	for _, c := range []struct {
		name, input string
		known       bool
		remaining   int
		reset       int64
	}{
		{"both windows: the tighter", `{"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1738425600},"seven_day":{"used_percentage":41.2,"resets_at":1738857600}}}`, true, 58, 1738857600},
		{"one window", `{"rate_limits":{"five_hour":{"used_percentage":100,"resets_at":1738425600}}}`, true, 0, 1738425600},
		{"before the first response", `{"model":{"id":"m"}}`, false, 0, 0},
		{"windows filled again", `{"rate_limits":{}}`, false, 0, 0},
		{"not json", `nope`, false, 0, 0},
	} {
		u := a.ReadUsage([]byte(c.input), none)
		if u.Known != c.known || u.Remaining != c.remaining || (c.reset != 0) != !u.Reset.IsZero() || c.reset != 0 && !u.Reset.Equal(time.Unix(c.reset, 0)) || u.Account != "" {
			t.Errorf("%s: %+v", c.name, u)
		}
	}
	if u := a.ReadUsage([]byte(`{}`), env(map[string]string{"CLAUDE_CONFIG_DIR": "/home/u/.config/acct-b/"})); u.Account != "acct-b" {
		t.Errorf("account %q", u.Account)
	}
}

func TestChainedCommandFollowsSettingsOrder(t *testing.T) {
	dir := t.TempDir()
	home, project, conf := filepath.Join(dir, "home"), filepath.Join(dir, "p"), filepath.Join(dir, "conf")
	write := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := claude.Adapter{}
	in := []byte(`{"cwd":"` + filepath.ToSlash(project) + `","workspace":{"project_dir":"` + filepath.ToSlash(project) + `"}}`)
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	chained := func() string { return a.ChainedCommand(in, get, os.ReadFile, home) }
	if got := chained(); got != "" {
		t.Fatalf("no settings: %q", got)
	}
	write(filepath.Join(home, ".claude", "settings.json"), `{"statusLine":{"type":"command","command":"user-line"}}`)
	if got := chained(); got != "user-line" {
		t.Fatalf("user: %q", got)
	}
	write(filepath.Join(project, ".claude", "settings.json"), `{"model":"x","statusLine":{"type":"command","command":"project-line"}}`)
	if got := chained(); got != "project-line" {
		t.Fatalf("project: %q", got)
	}
	write(filepath.Join(project, ".claude", "settings.local.json"), `{"statusLine":{"type":"command","command":"local-line","padding":1}}`)
	if got := chained(); got != "local-line" {
		t.Fatalf("local: %q", got)
	}
	env["CLAUDE_CONFIG_DIR"] = conf
	write(filepath.Join(conf, "settings.json"), `{"statusLine":{"type":"command","command":"conf-line"}}`)
	in = []byte(`{}`)
	if got := chained(); got != "conf-line" {
		t.Fatalf("config dir: %q", got)
	}
	failing := func(string) ([]byte, error) { return nil, errors.New("no") }
	if got := a.ChainedCommand(in, get, failing, home); got != "" {
		t.Fatalf("unreadable: %q", got)
	}
}
