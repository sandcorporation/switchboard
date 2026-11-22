package hosting

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// limitService is a clocked service with node pc registered.
func limitService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	s, now := clockService(t)
	if err := s.RegisterLocalNode("pc", "sbn_pc"); err != nil {
		t.Fatal(err)
	}
	return s, now
}

func add(t *testing.T, s *Service, dir, group, desired string) wire.HostedSession {
	t.Helper()
	c := record()
	c.Directory, c.Group, c.Desired = dir, group, desired
	h, err := s.Create(c)
	if err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	return h
}

func pct(n int) *int { return &n }

func addGroup(t *testing.T, s *Service, name string, threshold *int) {
	t.Helper()
	if _, err := s.AddGroup(wire.WorkGroup{Name: name, Threshold: threshold}); err != nil {
		t.Fatalf("add group %s: %v", name, err)
	}
}

// A session has no group by default; a group must exist, and one a
// session is in cannot be removed (#435).
func TestGroupIsCheckedAndKept(t *testing.T) {
	s, _ := limitService(t)
	h := add(t, s, "/w/a", "", "")
	if h.Group != "" || !h.AcceptingWork {
		t.Fatalf("defaults: group %q accepting %v", h.Group, h.AcceptingWork)
	}
	var invalid InvalidError
	slow := "slow"
	if _, err := s.Update(h.ID, wire.HostedSessionChange{Group: &slow}); !errors.As(err, &invalid) {
		t.Fatalf("unknown group: %v", err)
	}
	addGroup(t, s, "slow", pct(20))
	if h, err := s.Update(h.ID, wire.HostedSessionChange{Group: &slow}); err != nil || h.Group != "slow" {
		t.Fatalf("update group: %+v %v", h, err)
	}
	if got, _ := s.Get(h.ID); got.Group != "slow" {
		t.Fatalf("stored group %q", got.Group)
	}
	if _, err := s.RemoveGroup("slow"); !errors.Is(err, ErrGroupInUse) {
		t.Fatalf("remove a group in use: %v", err)
	}
	if _, err := s.AddGroup(wire.WorkGroup{Name: "slow"}); !errors.Is(err, ErrGroupExists) {
		t.Fatalf("second slow: %v", err)
	}
	for _, bad := range []wire.WorkGroup{{Name: "a b"}, {Name: ""}, {Name: "fast", Threshold: pct(101)}, {Name: "fast", Threshold: pct(-1)}} {
		if _, err := s.AddGroup(bad); !errors.As(err, &invalid) {
			t.Fatalf("add %+v: %v", bad, err)
		}
	}
	none := ""
	if _, err := s.Update(h.ID, wire.HostedSessionChange{Group: &none}); err != nil {
		t.Fatal(err)
	}
	if g, err := s.RemoveGroup("slow"); err != nil || g.Name != "slow" {
		t.Fatalf("remove: %+v %v", g, err)
	}
	if _, err := s.Group("slow"); !errors.Is(err, ErrNoGroup) {
		t.Fatalf("removed group: %v", err)
	}
}

func TestFifthRunningSessionOnANodeIsRefused(t *testing.T) {
	s, _ := limitService(t)
	var ids []string
	for i := range 4 {
		ids = append(ids, add(t, s, "/w/"+strconv.Itoa(i), "", "").ID)
	}
	c := record()
	c.Directory = "/w/5"
	var capErr CapacityError
	if _, err := s.Create(c); !errors.As(err, &capErr) || capErr.Max != 4 || !strings.Contains(err.Error(), "SWITCHBOARD_HOSTING_MAX") {
		t.Fatalf("fifth create: %v", err)
	}
	// A stopped record is not refused, but starting it is.
	stopped := add(t, s, "/w/5", "", DesiredStopped)
	if _, err := s.SetDesired(stopped.ID, DesiredRunning); !errors.As(err, &capErr) {
		t.Fatalf("start beyond cap: %v", err)
	}
	if _, err := s.Restart(stopped.ID); !errors.As(err, &capErr) {
		t.Fatalf("restart of a stopped record beyond cap: %v", err)
	}
	if _, err := s.NewConversation(stopped.ID); !errors.As(err, &capErr) {
		t.Fatalf("new conversation of a stopped record beyond cap: %v", err)
	}
	// Starting one that already runs (clears a crash loop) is not refused.
	if _, err := s.SetDesired(ids[0], DesiredRunning); err != nil {
		t.Fatalf("start of a running record: %v", err)
	}
	// Stop one, and the stopped record may start.
	if _, err := s.SetDesired(ids[1], DesiredStopped); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDesired(stopped.ID, DesiredRunning); err != nil {
		t.Fatalf("start within cap: %v", err)
	}
	// The cap is per node and follows the node's hello.
	if err := s.nodeSeen("pc", wire.NodeHello{Max: 6}); err != nil {
		t.Fatal(err)
	}
	add(t, s, "/w/6", "", "")
	if max, _ := s.NodeMax("pc"); max != 6 {
		t.Fatalf("node max %d, want 6", max)
	}
}

func TestCapRefusalIsAConflictOverTheAPI(t *testing.T) {
	s, _ := limitService(t)
	for i := range 4 {
		add(t, s, "/w/"+strconv.Itoa(i), "", "")
	}
	h := NewHub(s, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/admin/terminals", strings.NewReader(`{"node":"pc","base":"team/worker","profile":"p","directory":"/w/9"}`))
	h.AdminTerminalsAPI(w, r, "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "cap of 4") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func accepting(t *testing.T, s *Service) map[string]bool {
	t.Helper()
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, h := range list {
		out[h.Label] = h.AcceptingWork
	}
	return out
}

// The operator's switch is 0% left for every account (#435): a session
// whose group has a threshold above it takes no new work; one without a
// group, or of a group without a threshold, still does.
func TestOperatorLimitStopsNewWorkByGroupThreshold(t *testing.T) {
	s, _ := limitService(t)
	addGroup(t, s, "fast", nil)
	addGroup(t, s, "slow", pct(20))
	addGroup(t, s, "zero", pct(0))
	for i, group := range []string{"", "fast", "slow", "zero"} {
		c := record()
		c.Directory, c.Group, c.Label = "/w/"+strconv.Itoa(i), group, "g-"+group
		if _, err := s.Create(c); err != nil {
			t.Fatal(err)
		}
	}
	all := map[string]bool{"g-": true, "g-fast": true, "g-slow": true, "g-zero": true}
	if got := accepting(t, s); !mapsEqual(got, all) {
		t.Fatalf("before the limit: %v", got)
	}
	l, err := s.SetLimit(true)
	if err != nil || !l.Limited || l.Source != LimitOperator {
		t.Fatalf("set: %+v %v", l, err)
	}
	want := map[string]bool{"g-": true, "g-fast": true, "g-slow": false, "g-zero": true}
	if got := accepting(t, s); !mapsEqual(got, want) {
		t.Fatalf("limited: %v", got)
	}
	// One threshold moves the whole group.
	if _, err := s.SetGroupThreshold("slow", nil); err != nil {
		t.Fatal(err)
	}
	if got := accepting(t, s); !mapsEqual(got, all) {
		t.Fatalf("slow without a threshold: %v", got)
	}
	if _, err := s.SetGroupThreshold("slow", pct(20)); err != nil {
		t.Fatal(err)
	}
	if l, err = s.SetLimit(false); err != nil || l.Limited || l.ClearedAt == nil {
		t.Fatalf("clear: %+v %v", l, err)
	}
	if got := accepting(t, s); !mapsEqual(got, all) {
		t.Fatalf("after the limit: %v", got)
	}
}

func mapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func TestScreenLimitClearsAtResetAndOnOperatorOff(t *testing.T) {
	s, now := limitService(t)
	reset := now.Add(time.Hour).UnixMilli()
	if err := s.setNodeLimit("pc", wire.NodeLimit{Limited: true, Since: now.UnixMilli(), ResetAt: &reset}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Limit()
	if !l.Limited || l.Source != LimitScreen || l.ResetAt == nil || *l.ResetAt != reset {
		t.Fatalf("screen limit %+v", l)
	}
	*now = now.Add(time.Hour)
	if l, _ := s.Limit(); l.Limited {
		t.Fatalf("limited after the reset time: %+v", l)
	}

	// Operator off clears a showing that began before it.
	*now = now.Add(time.Minute)
	if err := s.setNodeLimit("pc", wire.NodeLimit{Limited: true, Since: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	if l, _ := s.Limit(); !l.Limited || l.ResetAt != nil {
		t.Fatalf("limit without reset %+v", l)
	}
	if _, err := s.SetLimit(false); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.Limit(); l.Limited {
		t.Fatalf("limited after operator off: %+v", l)
	}
}

func TestHubPublishesLimitClearedOnce(t *testing.T) {
	s, now := limitService(t)
	var kinds []string
	h := NewHub(s, func(k ...string) { kinds = append(kinds, k...) })
	if err := h.Sync(); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		n := 0
		for _, k := range kinds {
			if k == LimitClearedKind {
				n++
			}
		}
		return n
	}
	if _, err := h.SetLimit(true); err != nil {
		t.Fatal(err)
	}
	if count() != 0 || !slices.Contains(kinds, "terminals") {
		t.Fatalf("on: %v", kinds)
	}
	if _, err := h.SetLimit(false); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := h.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if count() != 1 {
		t.Fatalf("operator off published %v", kinds)
	}

	// A node's screen limit clears at its reset time: once more.
	reset := now.Add(time.Minute).UnixMilli()
	h.receive(&nodeConn{name: "pc"}, wire.NodeFrame{Type: wire.NodeStateType, Limit: &wire.NodeLimit{Limited: true, Since: now.Add(time.Second).UnixMilli(), ResetAt: &reset}})
	*now = now.Add(2 * time.Second)
	if l, _ := s.Limit(); !l.Limited {
		t.Fatalf("node report did not limit: %+v", l)
	}
	*now = now.Add(time.Minute)
	for range 3 {
		if err := h.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if count() != 2 {
		t.Fatalf("reset time published %v", kinds)
	}
	if l, _ := s.Limit(); l.ClearedAt == nil || *l.ClearedAt != now.UnixMilli() {
		t.Fatalf("cleared at %+v", l)
	}
}

func TestLimitAPI(t *testing.T) {
	s, _ := limitService(t)
	h := NewHub(s, nil)
	put := httptest.NewRecorder()
	h.AdminTerminalsAPI(put, httptest.NewRequest(http.MethodPut, "/api/admin/terminals/limit", strings.NewReader(`{"limited":true}`)), "limit")
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"limited":true`) || !strings.Contains(put.Body.String(), `"source":"operator"`) {
		t.Fatalf("put %d %s", put.Code, put.Body)
	}
	get := httptest.NewRecorder()
	h.AdminTerminalsAPI(get, httptest.NewRequest(http.MethodGet, "/api/admin/terminals/limit", nil), "limit")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"limited":true`) {
		t.Fatalf("get %d %s", get.Code, get.Body)
	}
}

func TestResumeOrderPutsQueuedWorkFirst(t *testing.T) {
	type item struct {
		id     string
		queued int64
		at     int64
	}
	items := []item{{"new2", 0, 50}, {"q2", 30, 20}, {"new1", 0, 40}, {"q1", 10, 30}}
	ResumeOrder(items, func(i item) WorkKey { return WorkKey{QueuedAt: i.queued, ArrivedAt: i.at} })
	var got []string
	for _, i := range items {
		got = append(got, i.id)
	}
	if strings.Join(got, " ") != "q1 q2 new1 new2" {
		t.Fatalf("order %v", got)
	}
}

// Concurrent starts and adds at cap-1 on one node: the count and the write
// are one statement, so exactly one is let through and the rest are
// refused with the cap (409 over the API) (#210).
func TestConcurrentStartsAtCapMinusOneLetExactlyOneThrough(t *testing.T) {
	for round := range 20 {
		s, _ := limitService(t)
		for i := range 3 {
			add(t, s, "/w/run"+strconv.Itoa(i), "", "")
		}
		const racers = 6
		var stopped []string
		for i := range racers {
			stopped = append(stopped, add(t, s, "/w/stop"+strconv.Itoa(i), "", DesiredStopped).ID)
		}
		errs := make(chan error, 2*racers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, id := range stopped {
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := s.SetDesired(id, DesiredRunning)
				errs <- err
			}()
			go func() {
				defer wg.Done()
				<-start
				c := record()
				c.Directory = "/w/new" + strconv.Itoa(i)
				_, err := s.Create(c)
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		ok := 0
		for err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.As(err, new(CapacityError)):
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		var running int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM hosted_sessions WHERE node='pc' AND desired=?`, DesiredRunning).Scan(&running); err != nil {
			t.Fatal(err)
		}
		if ok != 1 || running != 4 {
			t.Fatalf("round %d: %d let through, %d running; want 1 and 4", round, ok, running)
		}
	}
}

func TestHubHandsLastRunToAReattachingNode(t *testing.T) {
	s, _ := limitService(t)
	h := NewHub(s, func(...string) {})
	rec := add(t, s, "/work/a", "", "")
	if err := h.Sync(); err != nil {
		t.Fatal(err)
	}
	first := &nodeConn{name: "pc", out: make(chan wire.NodeFrame, nodeQueue), cancel: func() {}}
	h.attach(first)
	h.receive(first, wire.NodeFrame{Type: wire.NodeStateType, States: []wire.HostedNodeState{{ID: rec.ID, Runtime: wire.HostedRuntime{State: "running", Run: 4}}}})

	// The node process restarts and attaches again.
	second := &nodeConn{name: "pc", out: make(chan wire.NodeFrame, nodeQueue), cancel: func() {}}
	h.attach(second)
	f := <-second.out
	if f.Type != wire.NodeAssign || len(f.Sessions) != 1 || f.Sessions[0].LastRun != 4 {
		t.Fatalf("assign %+v", f)
	}
}
