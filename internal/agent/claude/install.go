package claude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
)

// The Claude Code plugin this binary carries (#477): plugin/ is its source,
// tested by its own suites (plugin/e2e/suite.sh, plugin/e2e/run.sh).
//
//go:embed all:plugin
var pluginFiles embed.FS

const (
	// pluginName is the plugin's name in its manifest; marketName the local
	// marketplace the install registers (the folder PluginOptions.Dir).
	pluginName = "switchboard"
	marketName = "switchboard"
	pluginID   = pluginName + "@" + marketName
	// cmdTimeout bounds each `claude plugin …` command.
	cmdTimeout = 60 * time.Second
)

var _ agent.PluginInstaller = Adapter{}

// InstallPlugin installs the carried plugin through Claude Code's own plugin
// commands (https://code.claude.com/docs/en/plugins/cli-reference.md): the
// files are unpacked to Dir/<version>/, Dir becomes the local marketplace
// "switchboard" (`plugin marketplace add`, a directory source), and the
// plugin is installed, updated to this version and enabled at user scope
// (`plugin install|update|enable --scope user`). A switchboard plugin from
// another marketplace is disabled (`plugin disable`, never uninstalled) at
// user scope, and at a project's local or project scope where that
// project's settings turn it on.
func (Adapter) InstallPlugin(ctx context.Context, o agent.PluginOptions) (agent.PluginReport, error) {
	in := defaultInstaller()
	in.done = &installed
	return in.install(ctx, o)
}

// installed holds the (folder, version, settings folder) this process
// installed, for PluginOptions.Once.
var installed sync.Map

// PluginStatus reads `plugin list --json`.
func (Adapter) PluginStatus(ctx context.Context, o agent.PluginOptions) (agent.PluginState, error) {
	return defaultInstaller().status(ctx, o)
}

// installer is the install with its seams: where the CLI is found and how a
// command runs.
type installer struct {
	files fs.FS
	look  func(string) (string, error)
	// run runs the CLI exe with args in dir under env and returns stdout.
	run func(ctx context.Context, exe, dir string, env []string, args ...string) ([]byte, error)
	// done remembers the installs made (PluginOptions.Once); nil: none.
	done *sync.Map
}

func defaultInstaller() installer {
	sub, err := fs.Sub(pluginFiles, "plugin")
	if err != nil {
		panic(err) // the embed pattern names plugin/
	}
	return installer{files: sub, look: exec.LookPath, run: runCmd}
}

func runCmd(ctx context.Context, exe, dir string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil && stdout.Len() == 0 {
		if line := lastLine(stderr.Bytes()); line != "" {
			err = fmt.Errorf("%w: %s", err, line)
		}
	}
	return stdout.Bytes(), err
}

// result is the one JSON object a `--json` plugin command prints as the last
// line of stdout.
type result struct {
	Outcome     string `json:"outcome"`
	Message     string `json:"message"`
	FailureCode string `json:"failureCode"`
}

// listed is one row of `plugin list --json`.
type listed struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	ProjectPath string `json:"projectPath"`
}

// market is one row of `plugin marketplace list --json`.
type market struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// session is one install or status run: the CLI, its environment and the
// folder commands run in.
type session struct {
	in  installer
	exe string
	env []string
	dir string
}

// do runs a `--json` plugin command and reads its result. A failure whose
// code is already_in_goal_state is not an error (already enabled, already
// disabled).
func (s session) do(ctx context.Context, dir string, args ...string) (result, error) {
	out, err := s.in.run(ctx, s.exe, dir, s.env, append(append([]string{"plugin"}, args...), "--json")...)
	var r result
	if line := lastLine(out); line != "" {
		if jerr := json.Unmarshal([]byte(line), &r); jerr != nil && err == nil {
			err = fmt.Errorf("unreadable output: %s", line)
		}
	}
	if r.FailureCode == "already_in_goal_state" {
		return r, nil
	}
	switch {
	case err == nil && r.Outcome != "" && r.Outcome != "ok":
		err = errors.New(r.Message)
	case err != nil && r.Message != "":
		err = fmt.Errorf("%w: %s", err, r.Message)
	}
	if err != nil {
		return r, fmt.Errorf("%s plugin %s: %w", Name, strings.Join(args, " "), err)
	}
	return r, nil
}

// list runs a list command (`plugin list` or `plugin marketplace list`)
// into v.
func (s session) list(ctx context.Context, v any, args ...string) error {
	out, err := s.in.run(ctx, s.exe, s.dir, s.env, append(append([]string{"plugin"}, args...), "--json")...)
	if err != nil {
		return fmt.Errorf("%s plugin %s: %w", Name, strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), v); err != nil {
		return fmt.Errorf("%s plugin %s: unreadable output: %w", Name, strings.Join(args, " "), err)
	}
	return nil
}

// open finds the CLI; ok false when it is not installed.
func (in installer) open(o agent.PluginOptions) (session, bool) {
	exe, err := in.look(Name)
	if err != nil {
		return session{}, false
	}
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	return session{in: in, exe: exe, env: env, dir: o.Dir}, true
}

func (in installer) install(ctx context.Context, o agent.PluginOptions) (agent.PluginReport, error) {
	rep := agent.PluginReport{Where: configDir(o.Env)}
	key := o.Dir + "\x00" + o.Version + "\x00" + rep.Where
	if o.Once && in.done != nil {
		if _, ok := in.done.Load(key); ok {
			return rep, nil
		}
	}
	rep, err := in.installAll(ctx, o, rep)
	if err == nil && rep.Skipped == "" && in.done != nil {
		in.done.Store(key, true)
	}
	return rep, err
}

func (in installer) installAll(ctx context.Context, o agent.PluginOptions, rep agent.PluginReport) (agent.PluginReport, error) {
	if ver := in.version(o.Version); isDevVersion(ver) && userSettings(o.Env) {
		return rep, fmt.Errorf("development build %s does not install into the user's Claude Code settings (%s): set %s to a scratch folder (#522)", ver, rep.Where, configDirEnv)
	}
	s, ok := in.open(o)
	if !ok {
		rep.Skipped = "Claude Code not found (no " + Name + " on PATH)"
		return rep, nil
	}
	ver, err := in.unpack(o.Dir, o.Version)
	if err != nil {
		return rep, err
	}
	var markets []market
	if err := s.list(ctx, &markets, "marketplace", "list"); err != nil {
		return rep, err
	}
	registered := false
	for _, m := range markets {
		if m.Name != marketName {
			continue
		}
		if m.Source == "directory" && samePath(m.Path, o.Dir) {
			registered = true
			break
		}
		// Ours at another folder (the config folder moved): point it here.
		if _, err := s.do(ctx, s.dir, "marketplace", "remove", marketName, "--scope", "user"); err != nil {
			return rep, err
		}
	}
	if !registered {
		if _, err := s.do(ctx, s.dir, "marketplace", "add", o.Dir, "--scope", "user"); err != nil {
			return rep, err
		}
		rep.Changes = append(rep.Changes, "marketplace "+marketName+" at "+o.Dir)
	}
	var plugins []listed
	if err := s.list(ctx, &plugins, "list"); err != nil {
		return rep, err
	}
	var mine *listed
	for i, p := range plugins {
		if p.ID == pluginID && p.Scope == "user" {
			mine = &plugins[i]
		}
	}
	switch {
	case mine == nil:
		if _, err := s.do(ctx, s.dir, "install", pluginID, "--scope", "user"); err != nil {
			return rep, err
		}
		rep.Changes = append(rep.Changes, "installed "+pluginID+" "+ver)
	case mine.Version != ver:
		if registered {
			// The marketplace file now names the new version's folder.
			if _, err := s.do(ctx, s.dir, "marketplace", "update", marketName); err != nil {
				return rep, err
			}
		}
		if _, err := s.do(ctx, s.dir, "update", pluginID, "--scope", "user"); err != nil {
			return rep, err
		}
		rep.Changes = append(rep.Changes, "updated "+pluginID+" "+mine.Version+" → "+ver)
	}
	if mine != nil && !mine.Enabled {
		if _, err := s.do(ctx, s.dir, "enable", pluginID, "--scope", "user"); err != nil {
			return rep, err
		}
		rep.Changes = append(rep.Changes, "enabled "+pluginID)
	}
	for _, p := range plugins {
		if !legacy(p.ID) {
			continue
		}
		dir := s.dir
		switch p.Scope {
		case "user":
			if !p.Enabled {
				continue
			}
		case "local", "project":
			if p.ProjectPath == "" || !projectEnables(p.ProjectPath, p.Scope, p.ID) {
				continue
			}
			dir = p.ProjectPath
		default:
			continue
		}
		r, err := s.do(ctx, dir, "disable", p.ID, "--scope", p.Scope)
		if err != nil {
			return rep, err
		}
		if r.FailureCode == "" {
			where := p.Scope
			if p.ProjectPath != "" {
				where += " " + p.ProjectPath
			}
			rep.Changes = append(rep.Changes, "disabled "+p.ID+" ("+where+")")
		}
	}
	return rep, nil
}

func (in installer) status(ctx context.Context, o agent.PluginOptions) (agent.PluginState, error) {
	st := agent.PluginState{Agent: Name, Where: configDir(o.Env), Want: in.version(o.Version), Legacy: []agent.LegacyPlugin{}}
	s, ok := in.open(o)
	if !ok {
		return st, nil
	}
	st.Available = true
	// Before the first install the folder does not exist (#479): list from
	// the current folder, which says the plugin is not installed.
	if _, err := os.Stat(s.dir); err != nil {
		s.dir = ""
	}
	var plugins []listed
	if err := s.list(ctx, &plugins, "list"); err != nil {
		return st, err
	}
	for _, p := range plugins {
		switch {
		case p.ID == pluginID && p.Scope == "user":
			st.Installed, st.Enabled = p.Version, p.Enabled
		case legacy(p.ID):
			enabled := p.Enabled
			if p.Scope == "local" || p.Scope == "project" {
				enabled = p.ProjectPath != "" && projectEnables(p.ProjectPath, p.Scope, p.ID)
			}
			st.Legacy = append(st.Legacy, agent.LegacyPlugin{ID: p.ID, Scope: p.Scope, Project: p.ProjectPath, Version: p.Version, Enabled: enabled})
		}
	}
	return st, nil
}

// legacy: a switchboard plugin from another marketplace.
func legacy(id string) bool {
	name, mk, ok := strings.Cut(id, "@")
	return ok && name == pluginName && mk != marketName
}

// projectEnables reads whether a project's settings file of scope turns id
// on (local: .claude/settings.local.json, project: .claude/settings.json).
// A missing folder or file reads as no.
func projectEnables(project, scope, id string) bool {
	file := "settings.json"
	if scope == "local" {
		file = "settings.local.json"
	}
	b, err := os.ReadFile(filepath.Join(project, ".claude", file))
	if err != nil {
		return false
	}
	var v struct {
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
	}
	return json.Unmarshal(b, &v) == nil && v.EnabledPlugins[id]
}

// configDir is the user settings folder the commands change:
// CLAUDE_CONFIG_DIR in env, else ~/.claude.
func configDir(env []string) string {
	if env == nil {
		env = os.Environ()
	}
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], configDirEnv+"="); ok && v != "" {
			return v
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".claude")
	}
	return "~/.claude"
}

// isDevVersion reports a development build's plugin version (version).
func isDevVersion(ver string) bool {
	return strings.HasPrefix(ver, "0.0.0-dev")
}

// userSettings reports whether env leaves the commands on the user's own
// Claude Code settings: CLAUDE_CONFIG_DIR unset, empty or naming ~/.claude.
// A development build installs only into another folder (#522): tests and
// local stacks run under a scratch CLAUDE_CONFIG_DIR, so a build from a
// worktree never replaces the plugin the user's sessions load.
func userSettings(env []string) bool {
	h, err := os.UserHomeDir()
	if err != nil {
		return true
	}
	a, b := filepath.Clean(configDir(env)), filepath.Clean(filepath.Join(h, ".claude"))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

var semver = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)

// version is the plugin version for a binary version: the release tag
// without its v, or for a development build 0.0.0-dev.<content hash>, so a
// changed build still reads as a new version.
func (in installer) version(binary string) string {
	if m := semver.FindStringSubmatch(binary); m != nil {
		return m[1]
	}
	h := sha256.New()
	_ = fs.WalkDir(in.files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(in.files, p)
		if err != nil {
			return err
		}
		h.Write([]byte(p + "\x00" + strconv.Itoa(len(b)) + "\x00"))
		h.Write(b)
		return nil
	})
	return "0.0.0-dev." + hex.EncodeToString(h.Sum(nil))[:8]
}

// unpack writes the plugin to dir/<version>/ (once: a finished folder is
// kept, so a session started from it keeps its files) with the manifest's
// version stamped, and points dir's marketplace file at it.
func (in installer) unpack(dir, binary string) (string, error) {
	ver := in.version(binary)
	dest := filepath.Join(dir, ver)
	if _, err := os.Stat(filepath.Join(dest, ".claude-plugin", "plugin.json")); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(dir, ".unpack-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		if err := in.write(tmp, ver); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, dest); err != nil {
			if _, serr := os.Stat(filepath.Join(dest, ".claude-plugin", "plugin.json")); serr != nil {
				return "", err
			} // another process unpacked it first
		}
	}
	m, err := json.MarshalIndent(map[string]any{
		"name":    marketName,
		"owner":   map[string]string{"name": "switchboard"},
		"plugins": []map[string]string{{"name": pluginName, "source": "./" + ver, "version": ver}},
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return ver, writeIfChanged(filepath.Join(dir, ".claude-plugin", "marketplace.json"), append(m, '\n'))
}

// write copies the embedded files to dst, the manifest with version ver.
func (in installer) write(dst, ver string) error {
	return fs.WalkDir(in.files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(in.files, p)
		if err != nil {
			return err
		}
		if p == ".claude-plugin/plugin.json" {
			if b, err = stamp(b, ver); err != nil {
				return err
			}
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// stamp sets the manifest's version, keeping its other keys.
func stamp(manifest []byte, ver string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("plugin manifest: %w", err)
	}
	v, _ := json.Marshal(ver)
	m["version"] = v
	out, err := json.MarshalIndent(m, "", "  ")
	return append(out, '\n'), err
}

func writeIfChanged(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
