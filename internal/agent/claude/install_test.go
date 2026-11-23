package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/sandcorporation/switchboard/internal/agent"
)

// fakeCLI keeps the state `claude plugin …` would change and answers its
// --json commands the way the real CLI does (cli-reference.md).
type fakeCLI struct {
	markets map[string]string // name → directory
	plugins []listed
	calls   []string // "dir|args…"
	envs    [][]string
	fail    string // a call prefix (args) that fails
}

func (f *fakeCLI) run(_ context.Context, exe, dir string, env []string, args ...string) ([]byte, error) {
	if exe != "/bin/claude" {
		return nil, errors.New("wrong exe " + exe)
	}
	line := strings.Join(args, " ")
	f.calls = append(f.calls, dir+"|"+line)
	f.envs = append(f.envs, env)
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return []byte(`{"outcome":"failed","message":"boom"}`), errors.New("exit status 1")
	}
	ok := func() ([]byte, error) { return []byte("Validating\n" + `{"outcome":"ok","message":"done"}`), nil }
	goal := func() ([]byte, error) {
		return []byte(`{"outcome":"failed","failureCode":"already_in_goal_state"}`), errors.New("exit status 1")
	}
	a := args[1:]
	switch {
	case slices.Equal(a, []string{"marketplace", "list", "--json"}):
		var out []market
		for n, p := range f.markets {
			out = append(out, market{Name: n, Source: "directory", Path: p})
		}
		return json.Marshal(out)
	case slices.Equal(a, []string{"list", "--json"}):
		return json.Marshal(f.plugins)
	case a[0] == "marketplace" && a[1] == "add":
		f.markets[marketName] = a[2]
		return ok()
	case a[0] == "marketplace" && a[1] == "remove":
		delete(f.markets, a[2])
		return ok()
	case a[0] == "marketplace" && a[1] == "update":
		return ok()
	case a[0] == "install":
		f.plugins = append(f.plugins, listed{ID: a[1], Version: "new", Scope: "user", Enabled: true})
		return ok()
	case a[0] == "update", a[0] == "enable":
		return ok()
	case a[0] == "disable":
		for i, p := range f.plugins {
			if p.ID == a[1] && p.Scope == a[3] && p.Enabled {
				f.plugins[i].Enabled = false
				return ok()
			}
		}
		return goal()
	}
	return nil, errors.New("unexpected " + line)
}

// commands lists the calls that change something (not the lists).
func (f *fakeCLI) commands() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.Contains(c, "list --json") {
			out = append(out, c)
		}
	}
	return out
}

var testFiles = fstest.MapFS{
	".claude-plugin/plugin.json": {Data: []byte(`{"name":"switchboard","version":"0.0.0-dev","description":"x"}`)},
	".mcp.json":                  {Data: []byte(`{}`)},
	"hooks/register.tsx":         {Data: []byte("export {}\n")},
}

func newTest(f *fakeCLI) installer {
	return installer{files: testFiles, look: func(string) (string, error) { return "/bin/claude", nil }, run: f.run}
}

func TestInstallPluginFreshRegistersInstallsAndStamps(t *testing.T) {
	dir := t.TempDir()
	f := &fakeCLI{markets: map[string]string{}}
	rep, err := newTest(f).install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir, Env: []string{"CLAUDE_CONFIG_DIR=/cfg/a", "PATH=/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		dir + "|plugin marketplace add " + dir + " --scope user --json",
		dir + "|plugin install switchboard@switchboard --scope user --json",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%q\nwant\n%q", got, want)
	}
	if rep.Where != "/cfg/a" || len(rep.Changes) != 2 || rep.Skipped != "" {
		t.Fatalf("report %+v", rep)
	}
	for _, env := range f.envs {
		if !slices.Contains(env, "CLAUDE_CONFIG_DIR=/cfg/a") {
			t.Fatalf("command env %q lacks the session's config dir", env)
		}
	}
	var m struct {
		Name    string
		Version string
	}
	b, err := os.ReadFile(filepath.Join(dir, "0.52.0", ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(b, &m) != nil || m.Version != "0.52.0" || m.Name != "switchboard" {
		t.Fatalf("stamped manifest %s (%v)", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "0.52.0", "hooks", "register.tsx")); err != nil {
		t.Fatal(err)
	}
	var mk struct {
		Name    string
		Plugins []struct{ Name, Source string }
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if json.Unmarshal(b, &mk) != nil || mk.Name != "switchboard" || len(mk.Plugins) != 1 || mk.Plugins[0].Source != "./0.52.0" {
		t.Fatalf("marketplace %s", b)
	}
}

func TestInstallPluginSameVersionChangesNothing(t *testing.T) {
	dir := t.TempDir()
	f := &fakeCLI{markets: map[string]string{marketName: dir}, plugins: []listed{{ID: pluginID, Version: "0.52.0", Scope: "user", Enabled: true}}}
	rep, err := newTest(f).install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.commands(); len(got) != 0 || len(rep.Changes) != 0 {
		t.Fatalf("commands %q, changes %q", got, rep.Changes)
	}
}

func TestInstallPluginNewVersionUpdatesAndKeepsTheOldFolder(t *testing.T) {
	dir := t.TempDir()
	f := &fakeCLI{markets: map[string]string{marketName: dir}, plugins: []listed{{ID: pluginID, Version: "0.51.0", Scope: "user", Enabled: false}}}
	in := newTest(f)
	if _, err := in.unpack(dir, "v0.51.0"); err != nil {
		t.Fatal(err)
	}
	rep, err := in.install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		dir + "|plugin marketplace update switchboard --json",
		dir + "|plugin update switchboard@switchboard --scope user --json",
		dir + "|plugin enable switchboard@switchboard --scope user --json",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%q\nwant\n%q", got, want)
	}
	if !slices.Contains(rep.Changes, "updated switchboard@switchboard 0.51.0 → 0.52.0") {
		t.Fatalf("changes %q", rep.Changes)
	}
	for _, v := range []string{"0.51.0", "0.52.0"} {
		if _, err := os.Stat(filepath.Join(dir, v, ".claude-plugin", "plugin.json")); err != nil {
			t.Fatalf("version folder %s: %v", v, err)
		}
	}
}

func TestInstallPluginMovesItsMarketplaceToTheFolder(t *testing.T) {
	dir := t.TempDir()
	f := &fakeCLI{markets: map[string]string{marketName: "/old/place"}, plugins: []listed{{ID: pluginID, Version: "0.52.0", Scope: "user", Enabled: true}}}
	if _, err := newTest(f).install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		dir + "|plugin marketplace remove switchboard --scope user --json",
		dir + "|plugin marketplace add " + dir + " --scope user --json",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%q\nwant\n%q", got, want)
	}
}

func TestInstallPluginDisablesTheOldPluginWithoutRemovingIt(t *testing.T) {
	dir := t.TempDir()
	on, off, gone := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "removed")
	if err := os.MkdirAll(filepath.Join(on, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(on, ".claude", "settings.local.json"), []byte(`{"enabledPlugins":{"switchboard@kit":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeCLI{markets: map[string]string{marketName: dir}, plugins: []listed{
		{ID: pluginID, Version: "0.52.0", Scope: "user", Enabled: true},
		{ID: "switchboard@kit", Version: "0.19.0", Scope: "user", Enabled: true},
		{ID: "switchboard@kit", Version: "0.16.1", Scope: "local", Enabled: true, ProjectPath: on},
		{ID: "switchboard@kit", Version: "0.16.1", Scope: "local", Enabled: true, ProjectPath: off},
		{ID: "switchboard@kit", Version: "0.16.1", Scope: "local", Enabled: true, ProjectPath: gone},
		{ID: "other@kit", Version: "1.0.0", Scope: "user", Enabled: true},
	}}
	in := newTest(f)
	st, err := in.status(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed != "0.52.0" || !st.Enabled || st.Want != "0.52.0" || len(st.Legacy) != 4 || !st.Legacy[0].Enabled || !st.Legacy[1].Enabled || st.Legacy[2].Enabled || st.Legacy[3].Enabled {
		t.Fatalf("status %+v", st)
	}
	rep, err := in.install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		dir + "|plugin disable switchboard@kit --scope user --json",
		on + "|plugin disable switchboard@kit --scope local --json",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%q\nwant\n%q", got, want)
	}
	if len(rep.Changes) != 2 || !strings.HasPrefix(rep.Changes[0], "disabled switchboard@kit (user)") {
		t.Fatalf("changes %q", rep.Changes)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "uninstall") || strings.Contains(c, "other@kit") {
			t.Fatalf("touched %s", c)
		}
	}
}

func TestInstallPluginWithoutClaudeCodeSkips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins")
	in := installer{files: testFiles, look: func(string) (string, error) { return "", exec.ErrNotFound }}
	rep, err := in.install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir})
	if err != nil || rep.Skipped == "" {
		t.Fatalf("report %+v, %v", rep, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unpacked without Claude Code: %v", err)
	}
	st, err := in.status(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: dir})
	if err != nil || st.Available {
		t.Fatalf("status %+v, %v", st, err)
	}
}

func TestInstallPluginFailureIsAnError(t *testing.T) {
	f := &fakeCLI{markets: map[string]string{}, fail: "plugin install"}
	_, err := newTest(f).install(context.Background(), agent.PluginOptions{Version: "v0.52.0", Dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
}

func TestPluginVersionOfADevelopmentBuildFollowsTheContent(t *testing.T) {
	in := newTest(&fakeCLI{})
	a := in.version("dev")
	if !strings.HasPrefix(a, "0.0.0-dev.") || a != in.version("dev") {
		t.Fatalf("dev version %q", a)
	}
	other := installer{files: fstest.MapFS{"x": {Data: []byte("y")}}}
	if other.version("dev") == a {
		t.Fatal("different content, same version")
	}
	if v := in.version("v1.2.3"); v != "1.2.3" {
		t.Fatalf("release version %q", v)
	}
}

// The carried plugin is the source folder: the manifest names the plugin
// and the files the MCP and hooks config point at are there.
func TestCarriedPluginIsComplete(t *testing.T) {
	files := defaultInstaller().files
	for _, p := range []string{".claude-plugin/plugin.json", ".mcp.json", "hooks/hooks.json", "hooks/register.tsx", "mcp/server.mjs", "core/mailbox.ts"} {
		if _, err := fs.Stat(files, p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

// TestInstallPluginWithClaudeCode runs the install against the real CLI in
// a scratch config folder (CI's plugin job, SWITCHBOARD_TEST_CLAUDE=require;
// skipped elsewhere): installed and enabled, then a second run changes
// nothing, then a new version updates it.
func TestInstallPluginWithClaudeCode(t *testing.T) {
	if os.Getenv("SWITCHBOARD_TEST_CLAUDE") != "require" {
		t.Skip("SWITCHBOARD_TEST_CLAUDE=require runs this against the installed CLI")
	}
	cfg, dir := t.TempDir(), t.TempDir()
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+cfg)
	in := defaultInstaller()
	ctx := context.Background()
	rep, err := in.install(ctx, agent.PluginOptions{Version: "v0.52.0", Dir: dir, Env: env})
	if err != nil || len(rep.Changes) == 0 {
		t.Fatalf("first install %+v, %v", rep, err)
	}
	st, err := in.status(ctx, agent.PluginOptions{Version: "v0.52.0", Dir: dir, Env: env})
	if err != nil || st.Installed != "0.52.0" || !st.Enabled {
		t.Fatalf("status %+v, %v", st, err)
	}
	rep, err = in.install(ctx, agent.PluginOptions{Version: "v0.52.0", Dir: dir, Env: env})
	if err != nil || len(rep.Changes) != 0 {
		t.Fatalf("second install %+v, %v", rep, err)
	}
	rep, err = in.install(ctx, agent.PluginOptions{Version: "v0.53.0", Dir: dir, Env: env})
	if err != nil || len(rep.Changes) != 1 {
		t.Fatalf("new version %+v, %v", rep, err)
	}
	st, err = in.status(ctx, agent.PluginOptions{Version: "v0.53.0", Dir: dir, Env: env})
	if err != nil || st.Installed != "0.53.0" || !st.Enabled {
		t.Fatalf("status after update %+v, %v", st, err)
	}
	b, err := os.ReadFile(filepath.Join(cfg, "settings.json"))
	if err != nil || !strings.Contains(string(b), `"switchboard@switchboard": true`) {
		t.Fatalf("user settings %s (%v)", b, err)
	}
}

// Once: a node launching many sessions installs a version into the same
// settings folder once; another settings folder, or a failure, runs again.
func TestInstallPluginOnce(t *testing.T) {
	dir := t.TempDir()
	f := &fakeCLI{markets: map[string]string{}}
	in := newTest(f)
	in.done = &sync.Map{}
	o := agent.PluginOptions{Version: "v0.52.0", Dir: dir, Env: []string{configDirEnv + "=/a"}, Once: true}
	for range 2 {
		if _, err := in.install(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	n := len(f.calls)
	if _, err := in.install(context.Background(), o); err != nil || len(f.calls) != n {
		t.Fatalf("installed again: %v, %d calls after %d", err, len(f.calls), n)
	}
	o.Env = []string{configDirEnv + "=/b"}
	if _, err := in.install(context.Background(), o); err != nil || len(f.calls) == n {
		t.Fatalf("another settings folder not installed: %v", err)
	}
	o.Once, n = false, len(f.calls)
	if _, err := in.install(context.Background(), o); err != nil || len(f.calls) == n {
		t.Fatalf("without Once nothing ran: %v", err)
	}
}
