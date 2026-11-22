package node_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/node/fakeagent"
)

// folderWithSettings is a session folder whose .claude/settings.local.json
// names its own profile, as most folders moved to a node do (#462).
func folderWithSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := `{"env":{"SWITCHBOARD_PROFILE":"folder-profile","FOLDER_ONLY":"kept"}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// envOf is the value of name in environ, the last one winning.
func envOf(environ []string, name string) string {
	v := ""
	for _, kv := range environ {
		if k, val, ok := strings.Cut(kv, "="); ok && k == name {
			v = val
		}
	}
	return v
}

// settingsArgs is how many --settings argv carries.
func settingsArgs(argv []string) int {
	n := 0
	for _, a := range argv {
		if name, _, _ := strings.Cut(a, "="); name == "--settings" {
			n++
		}
	}
	return n
}

// A hosted session keeps the profile the node hands it over the one its
// folder's settings name (#462): the node's identity goes into the env of
// the session's one --settings, next to the permission skip and the status
// line, and the agent applies that over the folder's settings.
func TestHostedIdentityBeatsTheFolderSettings(t *testing.T) {
	dir := folderWithSettings(t)
	cfg := node.Config{Agent: "/opt/agent/claude", Switchboard: "/opt/sb/switchboard", StatusDir: "/var/sb/status", Endpoint: "http://mail.example", Machine: "pc-1"}
	r := rec("a")
	r.Directory, r.Profile, r.Label, r.SkipPermissions = dir, "hosted-a", "front", true
	argv := cfg.Argv(r, false)
	cmd := "/opt/sb/switchboard node statusline --adapter claude --out /var/sb/status/a.json"
	want := []string{"/opt/agent/claude", "--session-id", "conv-a", "--dangerously-skip-permissions", "--settings",
		`{"skipDangerousModePermissionPrompt":true,"statusLine":{"type":"command","command":"` + cmd + `"},"env":{"SWITCHBOARD_PROFILE":"hosted-a","SWITCHBOARD_ENDPOINT":"http://mail.example","SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_LABEL":"front"}}`,
		"--model", "opus"}
	if !slices.Equal(argv, want) || settingsArgs(argv) != 1 {
		t.Fatalf("argv\n%q\nwant\n%q", argv, want)
	}

	env := cfg.ChildEnv(r)
	got := fakeagent.SettingsEnv(env, argv[1:], dir)
	if p := envOf(got, "SWITCHBOARD_PROFILE"); p != "hosted-a" {
		t.Fatalf("profile in the session %q, want the node's", p)
	}
	if v := envOf(got, "FOLDER_ONLY"); v != "kept" {
		t.Fatalf("the folder's other env %q, want kept", v)
	}
	// Without the pin the folder's profile wins: the failure #462 fixed.
	if p := envOf(fakeagent.SettingsEnv(env, []string{"--session-id", "conv-a"}, dir), "SWITCHBOARD_PROFILE"); p != "folder-profile" {
		t.Fatalf("unpinned profile %q: the fake no longer plays the folder's settings", p)
	}
}

// A record whose args carry their own --settings keeps it (the agent reads
// only the last one), with the node's identity joined into its env.
func TestHostedIdentityJoinsTheRecordsSettings(t *testing.T) {
	dir := folderWithSettings(t)
	file := `{"model":"x","env":{"SWITCHBOARD_PROFILE":"file-profile","FILE_ONLY":"1"}}`
	if err := os.WriteFile(filepath.Join(dir, "own.json"), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := node.Config{Agent: "/opt/agent/claude", Switchboard: "/opt/sb/switchboard", StatusDir: "/var/sb/status", Machine: "pc-1"}
	for _, c := range []struct {
		name string
		args []string
		want []string
	}{
		{"inline", []string{"--settings", `{"env":{"SWITCHBOARD_PROFILE":"x","OWN":"1"},"theme":"dark"}`},
			[]string{"--settings", `{"env":{"OWN":"1","SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_PROFILE":"hosted-a"},"theme":"dark"}`}},
		{"inline with =", []string{"--settings={}"},
			[]string{`--settings={"env":{"SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_PROFILE":"hosted-a"}}`}},
		{"a file in the folder", []string{"--settings", "own.json"},
			[]string{"--settings", `{"env":{"FILE_ONLY":"1","SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_PROFILE":"hosted-a"},"model":"x"}`}},
		{"a file that is not there stays", []string{"--settings", "missing.json"},
			[]string{"--settings", "missing.json"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := rec("a")
			r.Directory, r.Profile, r.SkipPermissions, r.Args = dir, "hosted-a", true, c.args
			argv := cfg.Argv(r, false)
			// The permission skip's own --settings stays before the record's,
			// which the agent reads instead (as before #462).
			want := slices.Concat([]string{"/opt/agent/claude", "--session-id", "conv-a"}, claudeSkip, c.want)
			if !slices.Equal(argv, want) {
				t.Fatalf("argv\n%q\nwant\n%q", argv, want)
			}
			if c.name == "a file that is not there stays" {
				return
			}
			if p := envOf(fakeagent.SettingsEnv(cfg.ChildEnv(r), argv[1:], dir), "SWITCHBOARD_PROFILE"); p != "hosted-a" {
				t.Fatalf("profile in the session %q", p)
			}
		})
	}
}
