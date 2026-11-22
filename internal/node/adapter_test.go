package node_test

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/node"
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
