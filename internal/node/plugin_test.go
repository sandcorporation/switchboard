package node_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// pluggedAdapter is stubAdapter with a carried plugin (#477): it records the
// installs it is asked for and fails while fail is set.
type pluggedAdapter struct{ stubAdapter }

var plugged struct {
	sync.Mutex
	calls []agent.PluginOptions
	fail  error
}

func (pluggedAdapter) Name() string       { return "plugged" }
func (pluggedAdapter) Executable() string { return "pluggedcli" }
func (pluggedAdapter) Words() []string    { return []string{"pluggedcli"} }
func (pluggedAdapter) InstallPlugin(_ context.Context, o agent.PluginOptions) (agent.PluginReport, error) {
	plugged.Lock()
	defer plugged.Unlock()
	plugged.calls = append(plugged.calls, o)
	if plugged.fail != nil {
		return agent.PluginReport{}, plugged.fail
	}
	return agent.PluginReport{Changes: []string{"installed plugged " + o.Version}, Where: "/cfg"}, nil
}
func (pluggedAdapter) PluginStatus(context.Context, agent.PluginOptions) (agent.PluginState, error) {
	return agent.PluginState{}, nil
}

func init() { agent.MustRegister(pluggedAdapter{}) }

func pluggedRig(t *testing.T) (*rig, *[]string) {
	plugged.Lock()
	plugged.calls, plugged.fail = nil, nil
	plugged.Unlock()
	r := newRig(t)
	r.cfg.DefaultAdapter = agent.Default().Name()
	r.cfg.Adapters = map[string]string{agent.Default().Name(): "/opt/agent/claude", "plugged": "/opt/plugged/pluggedcli", "stub": "/opt/stub/stubcli"}
	r.cfg.Version, r.cfg.PluginDir = "v0.60.0", "/home/u/.config/switchboard/plugins"
	var logs []string
	r.cfg.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	return r, &logs
}

// Before a session of an adapter that carries a plugin launches, the node
// brings the plugin to its version in the settings the session runs with
// (the child's environment), asking the adapter to do it once per version
// and settings folder; an adapter without a plugin is not asked.
func TestLaunchInstallsTheCarriedPluginFirst(t *testing.T) {
	r, logs := pluggedRig(t)
	a := rec("a")
	a.Adapter = "plugged"
	b := rec("b")
	b.Adapter = "stub"
	n := r.node(a, b)
	r.reconcile(n)
	if len(r.launches()) != 2 {
		t.Fatalf("launches %q", r.launches())
	}
	plugged.Lock()
	calls := slices.Clone(plugged.calls)
	plugged.Unlock()
	if len(calls) != 1 {
		t.Fatalf("installs %+v", calls)
	}
	o := calls[0]
	child := r.fake.Launches()[0].Env
	if o.Version != "v0.60.0" || o.Dir != filepath.Join(r.cfg.PluginDir, "plugged") || !o.Once || !slices.Equal(o.Env, child) {
		t.Fatalf("install options %+v (child env %q)", o, child)
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "installed plugged v0.60.0") {
		t.Fatalf("logs %q", *logs)
	}
	for _, e := range n.Events() {
		if e.Kind == wire.HostedEventPluginFailed {
			t.Fatalf("event %+v", e)
		}
	}
}

// A failed install is logged and recorded as a plugin-failed event with its
// reason, and the session launches anyway.
func TestLaunchAfterAFailedPluginInstall(t *testing.T) {
	r, logs := pluggedRig(t)
	plugged.Lock()
	plugged.fail = errors.New("plugin marketplace add: boom")
	plugged.Unlock()
	a := rec("a")
	a.Adapter = "plugged"
	n := r.node(a)
	r.reconcile(n)
	if len(r.launches()) != 1 {
		t.Fatalf("not launched: %q", r.launches())
	}
	var kinds []string
	var reason string
	for _, e := range n.Events() {
		kinds = append(kinds, e.Kind)
		if e.Kind == wire.HostedEventPluginFailed {
			reason = e.Reason
		}
	}
	if !slices.Equal(kinds, []string{wire.HostedEventPluginFailed, wire.HostedEventStart}) || !strings.Contains(reason, "boom") {
		t.Fatalf("events %q, reason %q", kinds, reason)
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "launching anyway") || !strings.Contains((*logs)[0], "boom") {
		t.Fatalf("logs %q", *logs)
	}
}

// SWITCHBOARD_PLUGIN=off leaves the node's PluginDir empty: no install.
func TestLaunchWithoutPluginDirInstallsNothing(t *testing.T) {
	r, _ := pluggedRig(t)
	r.cfg.PluginDir = ""
	a := rec("a")
	a.Adapter = "plugged"
	n := r.node(a)
	r.reconcile(n)
	plugged.Lock()
	defer plugged.Unlock()
	if len(r.launches()) != 1 || len(plugged.calls) != 0 {
		t.Fatalf("launches %q, installs %+v", r.launches(), plugged.calls)
	}
}

var _ agent.PluginInstaller = pluggedAdapter{}
