package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/term/termtest"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// localRig is up's node over a fake backend whose programs take a moment
// to exit after a stop, run by runLocalNode as up runs it.
type localRig struct {
	s      *Server
	base   string
	cookie *http.Cookie
	fake   *termtest.Fake
	cancel context.CancelFunc
	done   chan struct{}
}

func newLocalRig(t *testing.T) *localRig {
	t.Helper()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	fake := termtest.New()
	n, err := node.New(node.Config{Agent: "/opt/agent/claude", Machine: "pc-1", Endpoint: base, NodeToken: token}, fake)
	if err != nil {
		t.Fatal(err)
	}
	l := &node.Link{Node: n, Host: base, Token: token, Interval: 20 * time.Millisecond,
		Hello: wire.NodeHello{Name: "pc", Machine: "pc-1", OS: "linux", Version: "test", Backend: "fake"},
		Logf:  func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	r := &localRig{s: s, base: base, cookie: cookie, fake: fake, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		runLocalNode(ctx, l, "pc", s.hostingService().List, t.Logf)
	}()
	t.Cleanup(r.stop)
	return r
}

func (r *localRig) add(t *testing.T, directory string) wire.HostedSession {
	t.Helper()
	got := adminCall(t, r.base, "POST", "/api/admin/terminals", r.cookie, true, `{"node":"pc","base":"a","profile":"team-a","directory":"`+directory+`"}`)
	var rec wire.HostedSession
	if err := json.Unmarshal([]byte(got.body), &rec); err != nil || got.code != http.StatusCreated {
		t.Fatalf("create = %d %s", got.code, got.body)
	}
	eventually(t, "the session running", func() bool { return terminals(t, r.base, r.cookie)[rec.ID].Runtime.State == "running" })
	return rec
}

// stop is up stopping: ctx ends and up waits for runLocalNode.
func (r *localRig) stop() {
	r.cancel()
	<-r.done
}

// alive reports whether the backend still holds a session for id, and
// whether its program is running.
func (r *localRig) alive(t *testing.T, id string) (held, running bool) {
	t.Helper()
	infos, err := r.fake.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range infos {
		if in.Name == node.TerminalName(id) {
			return true, in.Alive
		}
	}
	return false, false
}

// TestUpStoppedRightAfterARemoveLeavesNoSession is #361: a record removed
// just before up stops has its session stopped and forgotten before up
// exits, while the other records' sessions run on for the next up to adopt.
func TestUpStoppedRightAfterARemoveLeavesNoSession(t *testing.T) {
	t.Parallel()
	r := newLocalRig(t)
	kept, gone := r.add(t, "/work/kept"), r.add(t, "/work/gone")
	// An agent takes a moment to exit after SIGTERM; up stops within it.
	r.fake.SetExitDelay(300 * time.Millisecond)
	if got := adminCall(t, r.base, "DELETE", "/api/admin/terminals/"+gone.ID, r.cookie, true, ""); got.code != http.StatusOK {
		t.Fatalf("delete = %d %s", got.code, got.body)
	}
	eventually(t, "the node stopping the removed session", func() bool { return len(r.fake.Stops()) > 0 })
	r.stop()
	if held, _ := r.alive(t, gone.ID); held {
		t.Errorf("the removed record's session is still held after up stopped")
	}
	if _, running := r.alive(t, kept.ID); !running {
		t.Errorf("the kept record's session stopped with up")
	}
}

// TestUpStoppedBeforeTheAssignArrivesStillDrops: a record removed straight
// in the database, which an assign may not have carried to the node yet, is
// gone from the host's list up reads on its way out.
func TestUpStoppedBeforeTheAssignArrivesStillDrops(t *testing.T) {
	t.Parallel()
	r := newLocalRig(t)
	kept, gone := r.add(t, "/work/kept"), r.add(t, "/work/gone")
	r.fake.SetExitDelay(50 * time.Millisecond)
	if _, err := r.s.hostingService().Delete(gone.ID); err != nil {
		t.Fatal(err)
	}
	r.stop()
	if held, _ := r.alive(t, gone.ID); held {
		t.Errorf("the removed record's session is still held after up stopped")
	}
	if _, running := r.alive(t, kept.ID); !running {
		t.Errorf("the kept record's session stopped with up")
	}
}
