package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/term/termtest"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// fixtureAdapter is a made-up agent for these tests: "--begin ID" starts a
// conversation and "--again ID" resumes it.
type fixtureAdapter struct{}

func (fixtureAdapter) Name() string       { return "fixture" }
func (fixtureAdapter) Executable() string { return "fixturecli" }
func (fixtureAdapter) LaunchArgs(id string, resume bool) []string {
	if resume {
		return []string{"--again", id}
	}
	return []string{"--begin", id}
}
func (fixtureAdapter) PermissionSkipArgs([]string) []string { return nil }
func (fixtureAdapter) RefusedArgs() map[string]string {
	return map[string]string{"--again": "the node resumes"}
}
func (fixtureAdapter) RoleArgs() map[string]*regexp.Regexp {
	return map[string]*regexp.Regexp{"--tone": regexp.MustCompile(`^[a-z]+$`)}
}
func (fixtureAdapter) SessionMarkers() []string                      { return nil }
func (fixtureAdapter) ResumeFailed(code int, ran time.Duration) bool { return false }
func (fixtureAdapter) DetectLimit([]string, time.Time) (agent.LimitHit, bool) {
	return agent.LimitHit{}, false
}
func (fixtureAdapter) Words() []string { return []string{"fixturecli"} }

func init() { agent.MustRegister(fixtureAdapter{}) }

// attachAdapterNode attaches node "pc" with the default adapter and the
// fixture adapter installed, reporting both in its hello.
func attachAdapterNode(t *testing.T, base, token string, backend *termtest.Fake) {
	t.Helper()
	def := agent.Default().Name()
	cfg := node.Config{Agent: "/opt/agent/" + agent.Default().Executable(), Machine: "pc-1", Endpoint: base, NodeToken: token,
		DefaultAdapter: def, Adapters: map[string]string{def: "/opt/agent/" + agent.Default().Executable(), "fixture": "/opt/fixture/fixturecli"}}
	n, err := node.New(cfg, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	l := &node.Link{Node: n, Host: base, Token: token, Interval: 20 * time.Millisecond,
		Hello: wire.NodeHello{Name: "pc", Machine: "pc-1", OS: "linux", Version: "test", Backend: "fake", Adapters: cfg.InstalledAdapters(), DefaultAdapter: def},
		Logf:  func(string, ...any) {}}
	go func() { _ = l.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func argvOf(f *termtest.Fake, name string) []string {
	for _, l := range f.Launches() {
		if l.Name == name {
			return l.Argv
		}
	}
	return nil
}

// A session that chooses an adapter launches with that adapter's CLI and
// arguments; one that does not launches exactly as before (#434). The node
// reports its adapters, and the host refuses one the node does not have.
func TestHostedSessionChoosesItsAdapter(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	fake := termtest.New()
	attachAdapterNode(t, base, token, fake)
	eventually(t, "the node's adapters", func() bool {
		var nodes []wire.NodeView
		got := adminCall(t, base, "GET", "/api/admin/nodes", cookie, false, "")
		return json.Unmarshal([]byte(got.body), &nodes) == nil && len(nodes) == 1 && slices.Contains(nodes[0].Adapters, "fixture") && nodes[0].DefaultAdapter == agent.Default().Name()
	})

	if got := adminCall(t, base, "POST", "/api/admin/terminals", cookie, true, `{"node":"pc","base":"a","profile":"p","directory":"/w/x","adapter":"nosuch"}`); got.code != http.StatusBadRequest || !strings.Contains(got.body, "adapter: nosuch is not installed on node pc") {
		t.Fatalf("unknown adapter = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "POST", "/api/admin/terminals", cookie, true, `{"node":"pc","base":"a","profile":"p","directory":"/w/x","adapter":"fixture","args":["--again","x"]}`); got.code != http.StatusBadRequest || !strings.Contains(got.body, "args: --again is not allowed") {
		t.Fatalf("adapter's refused flag = %d %s", got.code, got.body)
	}
	chosen := createRecord(t, base, cookie, `{"node":"pc","base":"a","profile":"p","directory":"/w/fixture","adapter":"fixture","args":["--tone","calm"]}`)
	plain := createRecord(t, base, cookie, `{"node":"pc","base":"a","profile":"p","directory":"/w/plain","args":["--model","opus"]}`)
	if chosen.Adapter != "fixture" || plain.Adapter != "" {
		t.Fatalf("adapters %q %q", chosen.Adapter, plain.Adapter)
	}
	eventually(t, "both launched", func() bool { return len(fake.Launches()) == 2 })
	if got, want := argvOf(fake, "sb-"+chosen.ID), []string{"/opt/fixture/fixturecli", "--begin", chosen.AgentSessionID, "--tone", "calm"}; !slices.Equal(got, want) {
		t.Errorf("chosen argv %q, want %q", got, want)
	}
	if got := argvOf(fake, "sb-"+plain.ID); len(got) < 3 || got[0] != "/opt/agent/"+agent.Default().Executable() || got[1] != "--session-id" || got[2] != plain.AgentSessionID {
		t.Errorf("plain argv %q", got)
	}

	// The adapter can be changed; "" goes back to the node's default.
	if got := adminCall(t, base, "PUT", "/api/admin/terminals/"+chosen.ID, cookie, true, `{"adapter":""}`); got.code != http.StatusOK || strings.Contains(got.body, `"adapter"`) {
		t.Fatalf("clear adapter = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "PUT", "/api/admin/terminals/"+plain.ID, cookie, true, `{"adapter":"nosuch"}`); got.code != http.StatusBadRequest {
		t.Fatalf("change to unknown adapter = %d %s", got.code, got.body)
	}
}

// v047 shapes are the request bodies `switchboard` v0.47.0 sends, every
// field as that CLI marshals it. The host decodes strictly (unknown fields
// are refused), so these must keep being accepted as they are.
type v047Create struct {
	Node            string            `json:"node"`
	Base            string            `json:"base"`
	Profile         string            `json:"profile"`
	Directory       string            `json:"directory"`
	Label           string            `json:"label"`
	Parent          string            `json:"parent"`
	Args            []string          `json:"args"`
	Env             map[string]string `json:"env"`
	Restart         string            `json:"restart"`
	Desired         string            `json:"desired"`
	Class           string            `json:"class"`
	SkipPermissions *bool             `json:"skip_permissions,omitempty"`
}

type v047Change struct {
	Label           *string            `json:"label,omitempty"`
	Parent          *string            `json:"parent,omitempty"`
	Args            *[]string          `json:"args,omitempty"`
	Env             *map[string]string `json:"env,omitempty"`
	Restart         *string            `json:"restart,omitempty"`
	Desired         *string            `json:"desired,omitempty"`
	Class           *string            `json:"class,omitempty"`
	SkipPermissions *bool              `json:"skip_permissions,omitempty"`
}

type v047Grant struct {
	Role      string   `json:"role"`
	Bases     []string `json:"bases"`
	Nodes     []string `json:"nodes"`
	DirRoots  []string `json:"dir_roots"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The bodies of a v0.47.0 CLI still work (#434): create, change and grant,
// and the record launches as before.
func TestOldCLIBodiesStillAccepted(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	off := false
	got := adminCall(t, base, "POST", "/api/admin/terminals", cookie, true, mustJSON(t, v047Create{
		Node: "pc", Base: "a", Profile: "team-a", Directory: "/work/old", Label: "old", Args: []string{"--model", "opus"},
		Env: map[string]string{}, Restart: "always", Desired: "running", Class: "execute", SkipPermissions: &off,
	}))
	var h wire.HostedSession
	if got.code != http.StatusCreated || json.Unmarshal([]byte(got.body), &h) != nil || h.Adapter != "" {
		t.Fatalf("v0.47.0 create = %d %s", got.code, got.body)
	}
	label := "older"
	if got := adminCall(t, base, "PUT", "/api/admin/terminals/"+h.ID, cookie, true, mustJSON(t, v047Change{Label: &label})); got.code != http.StatusOK {
		t.Fatalf("v0.47.0 change = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "POST", "/api/admin/host-grants", cookie, true, mustJSON(t, v047Grant{Role: "a", Bases: []string{"b"}, Nodes: []string{"pc"}, DirRoots: []string{"/work"}})); got.code != http.StatusOK {
		t.Fatalf("v0.47.0 grant = %d %s", got.code, got.body)
	}
	fake := termtest.New()
	attachNode(t, base, token, fake) // a node of before: no adapters in its hello
	eventually(t, "launched", func() bool { return len(fake.Launches()) == 1 })
	if l := launchArgs(fake); l[0] != "sb-"+h.ID+" --session-id "+h.AgentSessionID {
		t.Fatalf("launch %q", l)
	}
}

// A grant may limit the adapters a role chooses; a record without one is
// held to the node's default.
func TestRoleGrantLimitsAdapters(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	attachAdapterNode(t, base, token, termtest.New())
	eventually(t, "the node's adapters", func() bool {
		return strings.Contains(adminCall(t, base, "GET", "/api/admin/nodes", cookie, false, "").body, `"fixture"`)
	})
	got := adminCall(t, base, "POST", "/api/admin/host-grants", cookie, true, `{"role":"a","bases":["b"],"nodes":["pc"],"dir_roots":["/work"],"adapters":["fixture"]}`)
	var g wire.HostGrant
	if got.code != 200 || json.Unmarshal([]byte(got.body), &g) != nil || !slices.Equal(g.Adapters, []string{"fixture"}) {
		t.Fatalf("grant = %d %s", got.code, got.body)
	}
	asA := func(body string) httpReply {
		return hostRoleCall(t, base, "POST", "/api/host/terminals", "secret-a", "a", body)
	}
	if got := asA(`{"node":"pc","base":"b","profile":"team-b","directory":"/work/x"}`); got.code != 403 || !strings.Contains(got.body, "adapter: "+agent.Default().Name()+" is not in the grant of a") {
		t.Fatalf("default adapter = %d %s", got.code, got.body)
	}
	if got := asA(`{"node":"pc","base":"b","profile":"team-b","directory":"/work/x","adapter":"fixture","args":["--model","x"]}`); got.code != 403 || !strings.Contains(got.body, "--model is not allowed") {
		t.Fatalf("other adapter's flag = %d %s", got.code, got.body)
	}
	if got := asA(`{"node":"pc","base":"b","profile":"team-b","directory":"/work/x","adapter":"fixture","args":["--tone","calm"]}`); got.code != 201 {
		t.Fatalf("granted adapter = %d %s", got.code, got.body)
	}
}

// `host add --adapter` stores the adapter; one the node reported it lacks is
// refused (#434).
func TestHostCLIAddsWithAdapter(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO roles(role,token,created_at) VALUES('team/worker','h','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO nodes(name,token_hash,created_at,adapters) VALUES('pc','h',1,'["fixture"]')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	add := []string{"add", "--node", "pc", "--base", "team/worker", "--profile", "team-worker", "--dir", "/work/app", "--adapter", "fixture", "--json"}
	code, out, stderr := hostCLI(t, dbPath, add...)
	if code != 0 {
		t.Fatalf("add = %d %s", code, stderr)
	}
	var h wire.HostedSession
	if err := json.Unmarshal([]byte(out), &h); err != nil || h.Adapter != "fixture" {
		t.Fatalf("add --json = %s %v", out, err)
	}
	add[8], add[10] = "/work/other", "nosuch"
	if code, _, stderr = hostCLI(t, dbPath, add...); code != 1 || !strings.Contains(stderr, "adapter: nosuch is not installed on node pc") {
		t.Errorf("unknown adapter = %d %s", code, stderr)
	}
}
