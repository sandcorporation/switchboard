package node_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/node/fakeagent"
)

// stubAdapter is an agent made up for these tests: "--new ID" starts a
// conversation, "--again ID" resumes it, "--yolo" skips permissions, and a
// resume fails when it exits 9.
type stubAdapter struct{}

func (stubAdapter) Name() string       { return "stub" }
func (stubAdapter) Executable() string { return "stubcli" }
func (stubAdapter) LaunchArgs(id string, resume bool) []string {
	if resume {
		return []string{"--again", id}
	}
	return []string{"--new", id}
}
func (stubAdapter) PermissionSkipArgs([]string) []string { return []string{"--yolo"} }
func (stubAdapter) RefusedArgs() map[string]string {
	return map[string]string{"--again": "the node resumes"}
}
func (stubAdapter) RoleArgs() map[string]*regexp.Regexp { return nil }
func (stubAdapter) SessionMarkers() []string            { return []string{"STUB_SESSION"} }
func (stubAdapter) ResumeFailed(code int, _ time.Duration) bool {
	return code == 9
}
func (stubAdapter) DetectLimit([]string, time.Time) (agent.LimitHit, bool) {
	return agent.LimitHit{}, false
}
func (stubAdapter) Words() []string { return []string{"stubcli"} }

func init() { agent.MustRegister(stubAdapter{}) }

// A record that names no adapter, or the node's default by name, launches
// exactly as before adapters existed (#434).
func TestRecordWithoutAdapterLaunchesAsBefore(t *testing.T) {
	cfg := node.Config{Agent: "/opt/agent/claude"}
	r := rec("a")
	r.SkipPermissions = true
	want := cfg.Argv(r, false)
	r.Adapter = agent.Default().Name()
	if got := cfg.Argv(r, false); !slices.Equal(got, want) {
		t.Fatalf("named default %q, want %q", got, want)
	}
	cfg.DefaultAdapter, cfg.Adapters = agent.Default().Name(), map[string]string{agent.Default().Name(): "/opt/agent/claude", "stub": "/opt/stub/stubcli"}
	r.Adapter = ""
	if got := cfg.Argv(r, true); !slices.Equal(got[:2], []string{"/opt/agent/claude", "--resume"}) {
		t.Fatalf("resume %q", got)
	}
}

// A record that chooses an adapter launches that adapter's CLI with its
// arguments, its permission skip and without the flags it refuses.
func TestRecordAdapterChoosesCLIAndArguments(t *testing.T) {
	r := newRig(t)
	r.cfg.DefaultAdapter = agent.Default().Name()
	r.cfg.Adapters = map[string]string{agent.Default().Name(): "/opt/agent/claude", "stub": "/opt/stub/stubcli"}
	a := rec("a")
	a.Adapter, a.SkipPermissions, a.Args = "stub", true, []string{"--again", "x", "--fast"}
	n := r.node(a)
	r.reconcile(n)
	r.exitAfter(n, time.Minute, 0)
	r.waitOut(n)
	got := r.launches()
	want := [][]string{
		{"/opt/stub/stubcli", "--new", "conv-a", "--yolo", "x", "--fast"},
		{"/opt/stub/stubcli", "--again", "conv-a", "--yolo", "x", "--fast"},
	}
	if len(got) != 2 || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Fatalf("launches %q, want %q", got, want)
	}
}

// The adapter decides what a failed resume is: two resumed runs exiting 9
// (the stub's failure) make the record wait for a new conversation.
func TestRecordAdapterJudgesResumeFailure(t *testing.T) {
	r := newRig(t)
	r.cfg.DefaultAdapter = agent.Default().Name()
	r.cfg.Adapters = map[string]string{agent.Default().Name(): "/opt/agent/claude", "stub": "/opt/stub/stubcli"}
	a := rec("a")
	a.Adapter = "stub"
	n := r.node(a)
	r.reconcile(n)
	r.exitAfter(n, time.Minute, 0)
	r.waitOut(n)
	r.exitAfter(n, time.Hour, 9) // long, but the stub says failed
	r.waitOut(n)
	r.exitAfter(n, time.Hour, 9)
	if s := r.state(n, "a"); s.Observed != node.ObservedResumeFailed {
		t.Fatalf("state %q, want %q", s.Observed, node.ObservedResumeFailed)
	}
}

// A record naming an adapter this node does not have is refused, not
// launched with another agent.
func TestRecordAdapterNotInstalledIsRefused(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct{ adapter, want string }{
		{"stub", "stub is not installed on this node"},
		{"nosuch", "unknown adapter"},
	} {
		a := rec("a")
		a.Adapter = c.adapter
		n := r.node(a)
		if err := n.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: err %v, want %q", c.adapter, err, c.want)
		}
		if got := r.launches(); len(got) != 0 {
			t.Fatalf("%s: launched %q", c.adapter, got)
		}
	}
}

// Resolve reports the adapters whose CLI it finds; the default adapter is
// the one SWITCHBOARD_HOSTING_AGENT (or the default executable) resolves to.
func TestResolveReportsInstalledAdapters(t *testing.T) {
	def := agent.Default()
	cfg, err := node.Resolve(host(nil, map[string]string{def.Executable(): "/usr/bin/" + def.Executable(), "stubcli": "/usr/bin/stubcli"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.InstalledAdapters(); !slices.Equal(got, []string{def.Name(), "stub"}) || cfg.DefaultAdapterName() != def.Name() {
		t.Fatalf("installed %q default %q", got, cfg.DefaultAdapterName())
	}
	cfg, err = node.Resolve(host(map[string]string{"SWITCHBOARD_HOSTING_AGENT": "stubcli"}, map[string]string{"stubcli": "/usr/bin/stubcli"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.InstalledAdapters(); !slices.Equal(got, []string{"stub"}) || cfg.DefaultAdapterName() != "stub" {
		t.Fatalf("installed %q default %q", got, cfg.DefaultAdapterName())
	}
	// An agent CLI no adapter knows keeps today's behaviour: the default
	// adapter's arguments with that CLI.
	cfg, err = node.Resolve(host(map[string]string{"SWITCHBOARD_HOSTING_AGENT": "other"}, map[string]string{"other": "/x/other"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Argv(rec("a"), false); got[0] != "/x/other" || cfg.DefaultAdapterName() != def.Name() {
		t.Fatalf("argv %q default %q", got, cfg.DefaultAdapterName())
	}
}

// A configured adapter (SWITCHBOARD_HOSTING_ADAPTERS) launches the fake
// agent: a first launch creates the conversation with its new_args, the next
// resumes it with its resume_args, and its limit pattern reads the screen.
func TestConfiguredAdapterRunsFakeAgent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "adapters.json")
	body := `{"adapters":[{"name":"fakegen","executable":"fakegen-cli","new_args":["--session-id","{id}"],"resume_args":["--resume","{id}"],"limit_patterns":["(?i)limit reached"]}]}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	def := agent.Default()
	h := host(map[string]string{"SWITCHBOARD_HOSTING_ADAPTERS": file}, map[string]string{def.Executable(): "/usr/bin/" + def.Executable(), "fakegen-cli": os.Args[0]})
	cfg, err := node.Resolve(h)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.InstalledAdapters(), "fakegen") {
		t.Fatalf("installed %q", cfg.InstalledAdapters())
	}
	if _, err := node.Resolve(h); err != nil { // loading the same file again is fine
		t.Fatal(err)
	}

	r := newRig(t)
	r.cfg.DefaultAdapter, r.cfg.Adapters = cfg.DefaultAdapter, cfg.Adapters
	r.cfg.Environ = append(config.WithoutOwnEnv(os.Environ()), fakeagent.Env(dir)...)
	a := rec("a")
	a.Adapter, a.Args = "fakegen", nil
	n := r.node(a)
	r.reconcile(n)
	if err := r.fake.Exit("sb-a", 0); err != nil {
		t.Fatal(err)
	}
	r.relaunch(n)
	launches := r.fake.Launches()
	if len(launches) != 2 || launches[0].Argv[1] != "--session-id" || launches[1].Argv[1] != "--resume" {
		t.Fatalf("launches %+v", launches)
	}
	for i, spec := range launches {
		cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
		cmd.Env, cmd.Dir = spec.Env, dir
		cmd.Stdin = strings.NewReader("hi\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("launch %d %q: %v\n%s", i, spec.Argv, err, out)
		}
		if !strings.Contains(string(out), "ready conv-a") {
			t.Fatalf("launch %d output:\n%s", i, out)
		}
	}

	if err := r.fake.Output("sb-a", []byte(fakeagent.LimitMessage("3pm")+"\r\n")); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(node.LimitScan)
	r.reconcile(n)
	if l := n.Limit(); !l.Limited || l.ResetAt == nil {
		t.Fatalf("limit %+v", l)
	}

	// A file naming a built-in adapter is refused at node start.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"adapters":[{"name":"`+def.Name()+`","executable":"x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := node.Resolve(host(map[string]string{"SWITCHBOARD_HOSTING_ADAPTERS": bad}, map[string]string{def.Executable(): "/x"})); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("built-in name: %v", err)
	}
}
