// Package node is node mode for terminal hosting (#173): it holds the
// records the host assigned to this machine and reconciles the terminal
// backend against them. The node is the source of truth for what is running;
// it keeps nothing durable, so a new Node over the same backend adopts what
// is already there.
//
// The package imports config, wire and term only; it never imports hosting.
// Records arrive as wire.HostedSession (#184).
package node

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"github.com/sandcorporation/switchboard/internal/agent"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/term"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Restart policies.
const (
	RestartAlways    = "always"
	RestartOnFailure = "on-failure"
	RestartNever     = "never"
)

// Desired states.
const (
	DesiredRunning = "running"
	DesiredStopped = "stopped"
)

// Observed states.
const (
	ObservedStarting = "starting"
	ObservedRunning  = "running"
	ObservedStopped  = "stopped"
	// ObservedBackingOff waits for State.NextAttemptAt before relaunching.
	ObservedBackingOff = "backing off"
	// ObservedCrashLoop makes no more attempts until Start or Restart.
	ObservedCrashLoop = "crashloop"
	// ObservedResumeFailed waits for the host's new conversation id.
	ObservedResumeFailed = "resume failed"
	// ObservedWaiting is a record that should run while the node already
	// runs Config.Max sessions; it starts when one stops (#190).
	ObservedWaiting = "waiting"
	// ObservedRelaunching is a session whose terminal holder was replaced,
	// until it is launched again with resume.
	ObservedRelaunching = "relaunching"
	// ObservedBackendDown is every record while the terminal holder
	// (keeper, tmux server) does not answer and cannot be started again:
	// the node cannot see or launch any session until it does (#464).
	ObservedBackendDown = "backend down"
)

// Restart rules (#188).
const (
	// BackoffBase is the wait before the first relaunch after an exit; it
	// doubles per consecutive short run up to BackoffCap.
	BackoffBase = 2 * time.Second
	BackoffCap  = 5 * time.Minute
	// LongRun is the run length that resets the backoff and does not count
	// towards a crash loop.
	LongRun = 10 * time.Minute
	// CrashLoopRuns short runs within CrashLoopWindow make a crash loop.
	CrashLoopRuns   = 5
	CrashLoopWindow = 30 * time.Minute
	// ResumeFailRuns resumed runs in a row that the adapter counts as failed
	// (for the default: non-zero within ResumeFailRun) mean the
	// conversation is gone.
	ResumeFailRuns = 2
	ResumeFailRun  = 15 * time.Second
)

// RestartBackoff is the wait before relaunching after the n-th consecutive
// short run (n >= 1): 2 s, 4 s, 8 s ... capped at 5 min.
func RestartBackoff(n int) time.Duration {
	d := BackoffBase
	for i := 1; i < n && d < BackoffCap; i++ {
		d *= 2
	}
	return min(d, BackoffCap)
}

// NamePrefix starts every terminal name the node owns: "sb-<record id>".
const NamePrefix = "sb-"

// StopGrace is how long a stopped session gets before it is killed.
const StopGrace = 10 * time.Second

// Record is one hosted session the host assigned to this node, the wire
// shape of the host's hosted_sessions row; the node reads its launch fields
// and ignores Runtime.
type Record = wire.HostedSession

// TerminalName is the backend session name for a record id.
func TerminalName(id string) string { return NamePrefix + id }

// State is what the node observes for one record, reported to the host.
type State struct {
	ID       string
	Observed string
	PID      int
	// Run counts launches of the record, continuing from the run the host
	// last saw (#348); an adopted session the host never saw counts as 1.
	Run       int
	StartedAt time.Time
	// LastExit is the exit code of the last finished run, nil before one.
	LastExit *int
	// Launched is true once this node has launched or adopted the record's
	// conversation AgentSessionID, so the host can persist it.
	Launched       bool
	AgentSessionID string
	// NextAttemptAt is when a backing-off record is relaunched.
	NextAttemptAt time.Time
	// ResumeFailed marks a conversation that replaced one whose --resume
	// failed.
	ResumeFailed bool
}

// Notice is a crash loop or resume failure the host should tell a person
// about; the node holds it until the condition clears.
type Notice = wire.NodeNotice

// Wire is the state as the host shows it.
func (s State) Wire() wire.HostedRuntime {
	rt := wire.HostedRuntime{State: s.Observed, PID: s.PID, Run: s.Run, LastExit: s.LastExit, ResumeFailed: s.ResumeFailed}
	if !s.NextAttemptAt.IsZero() {
		ms := s.NextAttemptAt.UnixMilli()
		rt.NextAttemptAt = &ms
	}
	if s.Run > 1 {
		rt.Restarts = s.Run - 1
	}
	if !s.StartedAt.IsZero() {
		ms := s.StartedAt.UnixMilli()
		rt.StartedAt = &ms
	}
	return rt
}

// Node reconciles a backend against the assigned records.
type Node struct {
	cfg     Config
	backend term.Backend

	mu      sync.Mutex
	records []Record
	// assigned is true once a list came in: before that records is empty
	// because nothing is known yet, not because every record went.
	assigned bool
	states   map[string]*state
	// relaunchAll clears failures at the next reconcile with records
	// (RestartSessions before the first assign).
	relaunchAll bool
	// active counts live sessions during a reconcile pass, against
	// Config.Max.
	active int
	// limits is the usage-limit message each session's screen shows, by
	// record id; lastScan is when screens were last read (#190).
	limits   map[string]*limitSeen
	lastScan time.Time
}

type state struct {
	State
	// seen is the start time of the run whose exit is already recorded.
	seen time.Time
	// sessionID is the conversation Launched refers to; a record carrying
	// another one is a new conversation.
	sessionID string
	// fresh asks for the running session to be replaced by a launch of
	// the new conversation.
	fresh bool
	// resumed is true when the current run was launched with --resume.
	resumed bool
	// agent is the adapter the current run was launched with (nil before
	// the first launch: the default).
	agent agent.Adapter
	// fails counts consecutive relaunches since the last long run.
	fails int
	// short holds the exit times of short runs within CrashLoopWindow.
	short []time.Time
	// next is when a backing-off record may relaunch.
	next  time.Time
	crash bool
	// resumeFails counts --resume runs in a row that failed fast.
	resumeFails int
	// awaiting is the conversation whose resume failed, until the host
	// assigns a new one.
	awaiting string
	notices  []Notice
	// events is the newest EventKeep events, oldest first (#252); the link
	// sends them on every connection and the host ignores repeats.
	events []wire.HostedEvent
	// stopped is the start time of the run the node stopped because the
	// record was set to stopped: its exit is not a crash.
	stopped time.Time
	// restartGen and startGen are the record's request counters last acted
	// on.
	restartGen, startGen int64
}

// clearFailures forgets backoff and crash loop (Start, Restart).
// adapter is the adapter of the state's current run.
func (s *state) adapter() agent.Adapter {
	if s.agent == nil {
		return agent.Default()
	}
	return s.agent
}

func (s *state) clearFailures() {
	s.fails, s.short, s.next, s.crash, s.resumeFails = 0, nil, time.Time{}, false, 0
	s.NextAttemptAt = time.Time{}
	s.dropNotices(wire.NoticeCrashLoop)
}

func (s *state) dropNotices(kind string) {
	s.notices = slices.DeleteFunc(s.notices, func(n Notice) bool { return n.Kind == kind })
}

// New returns a node over backend. cfg must come from Resolve (or be filled
// the same way in tests).
func New(cfg Config, backend term.Backend) (*Node, error) {
	if cfg.Agent == "" {
		return nil, errors.New("node: no agent CLI resolved")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Max <= 0 {
		cfg.Max = config.DefaultHostingMax
	}
	return &Node{cfg: cfg, backend: backend, states: map[string]*state{}, limits: map[string]*limitSeen{}}, nil
}

// Assign replaces the node's records with the host's full list. The node
// keeps enforcing the last list it got.
func (n *Node) Assign(recs []Record) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.records = append([]Record(nil), recs...)
	n.assigned = true
	ids := map[string]bool{}
	for _, r := range recs {
		ids[r.ID] = true
	}
	for id := range n.states {
		if !ids[id] {
			delete(n.states, id)
		}
	}
	for id := range n.limits {
		if !ids[id] {
			delete(n.limits, id)
		}
	}
}

// owns reports whether id is one of the assigned records.
func (n *Node) owns(id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, r := range n.records {
		if r.ID == id {
			return true
		}
	}
	return false
}

// States reports the observed state of every assigned record, in
// assignment order.
func (n *Node) States() []State {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]State, 0, len(n.records))
	for _, r := range n.records {
		s := n.stateOf(r)
		st := s.State
		st.AgentSessionID = s.sessionID
		if st.LastExit != nil {
			c := *st.LastExit
			st.LastExit = &c
		}
		out = append(out, st)
	}
	return out
}

func (n *Node) stateOf(r Record) *state {
	s, ok := n.states[r.ID]
	if !ok {
		s = &state{State: State{ID: r.ID, Observed: ObservedStopped, Launched: r.Launched},
			sessionID: r.AgentSessionID, restartGen: r.RestartGen, startGen: r.StartGen}
		n.states[r.ID] = s
	}
	if r.AgentSessionID != s.sessionID && r.Launched && s.Launched {
		// The host followed the conversation the running session moved to
		// inside its terminal (for example /clear): the process already
		// holds it, so it keeps running and the next launch resumes it.
		s.sessionID = r.AgentSessionID
	} else if r.AgentSessionID != s.sessionID {
		// A new conversation: from the host after a resume failure, or
		// asked for (New conversation). The running session, if any, holds
		// the old one.
		fellBack := s.awaiting != ""
		s.sessionID, s.Launched, s.fresh = r.AgentSessionID, r.Launched, true
		s.awaiting, s.resumeFails = "", 0
		s.dropNotices(wire.NoticeResumeFailed)
		s.ResumeFailed = fellBack
		if fellBack {
			s.next, s.NextAttemptAt = time.Time{}, time.Time{}
		} else {
			s.clearFailures()
		}
	} else if r.Launched {
		s.Launched = true
	}
	return s
}

// Notices reports the notices every assigned record holds, in assignment
// order.
// EventKeep is how many events a node holds per session for the host.
const EventKeep = 32

// event records one event of s at now, with the restart count as it is.
func (n *Node) event(s *state, kind, actor string, exit *int) {
	e := wire.HostedEvent{ID: s.ID, At: n.cfg.Now().UnixMilli(), Kind: kind, Restarts: max(0, s.Run-1), Actor: actor}
	if exit != nil {
		code := *exit
		e.ExitCode = &code
	}
	s.events = append(s.events, e)
	if len(s.events) > EventKeep {
		s.events = slices.Delete(s.events, 0, len(s.events)-EventKeep)
	}
}

// pluginTimeout bounds one plugin install before a launch.
const pluginTimeout = 2 * time.Minute

// installPlugin brings the carried plugin of r's adapter to this binary's
// version in the agent settings the session runs with (env, the child's:
// its settings folder variable decides where), once per version and
// settings folder (#477). A failure is logged and recorded as a
// plugin-failed event with its reason; the session launches anyway.
func (n *Node) installPlugin(ctx context.Context, r Record, s *state, env []string) {
	a := n.cfg.adapterFor(r)
	p, ok := a.(agent.PluginInstaller)
	if !ok || n.cfg.PluginDir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, pluginTimeout)
	defer cancel()
	rep, err := p.InstallPlugin(ctx, agent.PluginOptions{Version: n.cfg.Version, Dir: filepath.Join(n.cfg.PluginDir, a.Name()), Env: env, Once: true})
	switch {
	case err != nil:
		reason := err.Error()
		if len(reason) > 300 {
			reason = reason[:300] + "…"
		}
		n.logf("node: %s: %s plugin install failed, launching anyway: %v", TerminalName(r.ID), a.Name(), err)
		n.event(s, wire.HostedEventPluginFailed, wire.HostedActorNode, nil)
		s.events[len(s.events)-1].Reason = reason
	case len(rep.Changes) > 0:
		n.logf("node: %s plugin: %s (changed %s)", a.Name(), strings.Join(rep.Changes, "; "), rep.Where)
	}
}

func (n *Node) logf(format string, args ...any) {
	if n.cfg.Logf != nil {
		n.cfg.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// exitEvent records the end of a run that started at started with code:
// a crash or a clean exit, unless the node stopped it itself.
func (n *Node) exitEvent(s *state, started time.Time, code int) {
	if started.Equal(s.stopped) {
		return
	}
	kind := wire.HostedEventCrash
	if code == 0 {
		kind = wire.HostedEventExit
	}
	n.event(s, kind, "", &code)
}

// Events are the events the node holds for its assigned records, each
// record's oldest first.
func (n *Node) Events() []wire.HostedEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []wire.HostedEvent
	for _, r := range n.records {
		if s, ok := n.states[r.ID]; ok {
			out = append(out, s.events...)
		}
	}
	return out
}

func (n *Node) Notices() []Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []Notice
	for _, r := range n.records {
		if s, ok := n.states[r.ID]; ok {
			out = append(out, s.notices...)
		}
	}
	return out
}

// RestartSessions stops every session the node owns (grace StopGrace) and
// forgets it, so the next reconcile launches each record that should run
// with resume (switchboard node --restart-sessions). Backoff and crash
// loops are cleared.
func (n *Node) RestartSessions(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	infos, err := n.backend.List(ctx)
	if err != nil {
		return fmt.Errorf("node: list sessions: %w", err)
	}
	var errs []error
	for _, in := range infos {
		if !strings.HasPrefix(in.Name, NamePrefix) {
			continue
		}
		errs = append(errs, n.drop(ctx, in))
	}
	n.relaunchAll = true
	for _, r := range n.records {
		if s := n.stateOf(r); r.Desired != DesiredStopped {
			s.Observed, s.PID = ObservedRelaunching, 0
		}
	}
	return errors.Join(errs...)
}

// Run reconciles every interval until ctx is done.
func (n *Node) Run(ctx context.Context, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		_ = n.Reconcile(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Reconcile makes one pass: it stops and forgets backend sessions with no
// record, adopts live ones, handles exits by restart policy, launches
// records that should run and stops records that should not. Errors from
// individual sessions are joined; the pass still visits every record.
func (n *Node) Reconcile(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	infos, err := n.backend.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			// Say so instead of leaving the last states (running) in
			// place: a record started now cannot launch (#464).
			for _, r := range n.records {
				st := n.stateOf(r)
				st.Observed, st.PID, st.NextAttemptAt = ObservedBackendDown, 0, time.Time{}
			}
		}
		return fmt.Errorf("node: list sessions: %w", err)
	}
	byName := map[string]term.Info{}
	for _, in := range infos {
		byName[in.Name] = in
	}
	if n.relaunchAll && len(n.records) > 0 {
		n.relaunchAll = false
		for _, r := range n.records {
			n.stateOf(r).clearFailures()
		}
	}
	var errs []error
	assigned := map[string]bool{}
	for _, r := range n.records {
		assigned[TerminalName(r.ID)] = true
	}
	n.active = 0
	for _, in := range infos {
		if assigned[in.Name] && in.Alive {
			n.active++
		}
	}
	errs = append(errs, n.dropOrphans(ctx, infos, assigned)...)
	// Oldest-created first, so waiting records start in that order.
	order := slices.Clone(n.records)
	slices.SortStableFunc(order, func(a, b Record) int { return cmp.Compare(a.CreatedAt, b.CreatedAt) })
	for _, r := range order {
		in, ok := byName[TerminalName(r.ID)]
		errs = append(errs, n.reconcile(ctx, r, in, ok))
	}
	n.scanLimits(ctx, byName)
	return errors.Join(errs...)
}

// DropUnassigned stops and forgets every hosted session the last assigned
// list no longer names, and nothing else: no launch, no restart. Before the
// first assign it does nothing. up calls it on its way out so a record
// removed just before it stopped does not leave its session running until
// the next start (#361); ctx bounds the stops.
func (n *Node) DropUnassigned(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.assigned {
		return nil
	}
	infos, err := n.backend.List(ctx)
	if err != nil {
		return fmt.Errorf("node: list sessions: %w", err)
	}
	assigned := map[string]bool{}
	for _, r := range n.records {
		assigned[TerminalName(r.ID)] = true
	}
	return errors.Join(n.dropOrphans(ctx, infos, assigned)...)
}

// dropOrphans drops every hosted session in infos not in assigned.
func (n *Node) dropOrphans(ctx context.Context, infos []term.Info, assigned map[string]bool) []error {
	var errs []error
	for _, in := range infos {
		if !strings.HasPrefix(in.Name, NamePrefix) || assigned[in.Name] {
			continue
		}
		errs = append(errs, n.drop(ctx, in))
	}
	return errs
}

// drop stops and forgets a backend session no record owns.
func (n *Node) drop(ctx context.Context, in term.Info) error {
	if in.Alive {
		if err := n.backend.Stop(ctx, in.Name, StopGrace); err != nil {
			return fmt.Errorf("node: stop orphan %s: %w", in.Name, err)
		}
	}
	if err := n.backend.Forget(ctx, in.Name); err != nil && !errors.Is(err, term.ErrNoSession) {
		return fmt.Errorf("node: forget orphan %s: %w", in.Name, err)
	}
	return nil
}

func (n *Node) reconcile(ctx context.Context, r Record, in term.Info, exists bool) error {
	s := n.stateOf(r)
	name := TerminalName(r.ID)
	running := r.Desired != DesiredStopped
	now := n.cfg.Now()
	replace := s.fresh
	kind := wire.HostedEventStart // what a replace launch is
	if r.StartGen != s.startGen {
		// Start: clear failures; a session that ended (by policy too) is
		// launched again at once.
		s.startGen = r.StartGen
		s.clearFailures()
		replace = replace || (exists && !in.Alive)
	}
	if r.RestartGen != s.restartGen {
		s.restartGen, replace = r.RestartGen, true
		s.clearFailures()
		kind = wire.HostedEventRestart
	}
	if replace {
		// Restart, Start after an exit or a new conversation: stop what
		// runs, start at once.
		if exists && !s.fresh {
			s.Launched = true
		}
		s.fresh = false
		if exists {
			if err := n.remove(ctx, name, in.Alive); err != nil {
				return err
			}
			if in.Alive {
				n.active--
			}
			if !in.Alive && !in.StartedAt.Equal(s.seen) {
				code := in.ExitCode
				s.LastExit, s.seen = &code, in.StartedAt
				n.exitEvent(s, in.StartedAt, code)
			}
		}
		if !running {
			if exists && in.Alive {
				n.event(s, wire.HostedEventStop, wire.HostedActorAdmin, nil)
			}
			s.Observed, s.PID = ObservedStopped, 0
			return nil
		}
		return n.start(ctx, r, s, kind, wire.HostedActorAdmin)
	}
	if exists {
		// A session in the backend means the record was launched before,
		// by this node or an earlier one.
		s.Launched = true
	}
	switch {
	case exists && in.Alive && running:
		s.Observed, s.PID, s.StartedAt = ObservedRunning, in.PID, in.StartedAt
		if s.Run == 0 {
			// A session this node process did not launch: its keeper or
			// tmux server kept it across a node restart.
			s.Run = max(1, r.LastRun)
			n.event(s, wire.HostedEventAdopt, wire.HostedActorNode, nil)
		}
		return nil
	case exists && in.Alive:
		if err := n.backend.Stop(ctx, name, StopGrace); err != nil {
			return fmt.Errorf("node: stop %s: %w", name, err)
		}
		n.active--
		s.Observed, s.PID = ObservedStopped, 0
		s.stopped = in.StartedAt
		n.event(s, wire.HostedEventStop, wire.HostedActorAdmin, nil)
		return nil
	case exists:
		if !in.StartedAt.Equal(s.seen) {
			code := in.ExitCode
			s.LastExit, s.seen = &code, in.StartedAt
			n.exitEvent(s, in.StartedAt, code)
			if running && restarts(r.Restart, code) {
				n.exited(s, code, now.Sub(in.StartedAt), now)
			}
		}
		s.PID = 0
		if !running || !restarts(r.Restart, in.ExitCode) {
			s.Observed = ObservedStopped
			return nil
		}
		if n.held(s, now) {
			return nil
		}
		if err := n.backend.Forget(ctx, name); err != nil && !errors.Is(err, term.ErrNoSession) {
			return fmt.Errorf("node: forget %s: %w", name, err)
		}
		return n.start(ctx, r, s, wire.HostedEventRestart, wire.HostedActorPolicy)
	case running:
		// Never launched here, or its holder (keeper, tmux server) is gone:
		// launch with resume when the conversation exists.
		if n.held(s, now) {
			return nil
		}
		return n.start(ctx, r, s, wire.HostedEventStart, wire.HostedActorNode)
	default:
		s.Observed, s.PID = ObservedStopped, 0
		return nil
	}
}

// exited applies the restart rules to a run of a record that restarts: the
// backoff, the crash loop and the resume failure.
func (n *Node) exited(s *state, code int, ran time.Duration, now time.Time) {
	if s.resumed && s.adapter().ResumeFailed(code, ran) {
		s.resumeFails++
	} else {
		s.resumeFails = 0
	}
	if ran >= LongRun {
		s.fails, s.short = 0, nil
	} else {
		s.short = slices.DeleteFunc(s.short, func(t time.Time) bool { return now.Sub(t) >= CrashLoopWindow })
		s.short = append(s.short, now)
	}
	if len(s.short) >= CrashLoopRuns {
		s.crash, s.next, s.NextAttemptAt = true, time.Time{}, time.Time{}
		s.notices = append(s.notices, Notice{ID: s.ID, Kind: wire.NoticeCrashLoop, Run: s.Run, AgentSessionID: s.sessionID, LastExit: code, Runs: len(s.short)})
		return
	}
	if s.resumeFails >= ResumeFailRuns {
		s.awaiting, s.next, s.NextAttemptAt = s.sessionID, time.Time{}, time.Time{}
		s.notices = append(s.notices, Notice{ID: s.ID, Kind: wire.NoticeResumeFailed, Run: s.Run, AgentSessionID: s.sessionID, LastExit: code, Runs: s.resumeFails})
		return
	}
	s.fails++
	s.next = now.Add(RestartBackoff(s.fails))
}

// held reports whether a record that should run waits instead of launching:
// a crash loop, a resume failure awaiting a new conversation, or backoff.
// It sets the observed state of a waiting record.
func (n *Node) held(s *state, now time.Time) bool {
	switch {
	case s.crash:
		s.Observed, s.NextAttemptAt = ObservedCrashLoop, time.Time{}
	case s.awaiting != "":
		s.Observed, s.NextAttemptAt = ObservedResumeFailed, time.Time{}
	case now.Before(s.next):
		s.Observed, s.NextAttemptAt = ObservedBackingOff, s.next
	default:
		return false
	}
	s.PID = 0
	return true
}

// remove stops (when alive) and forgets a backend session.
func (n *Node) remove(ctx context.Context, name string, alive bool) error {
	if alive {
		if err := n.backend.Stop(ctx, name, StopGrace); err != nil {
			return fmt.Errorf("node: stop %s: %w", name, err)
		}
	}
	if err := n.backend.Forget(ctx, name); err != nil && !errors.Is(err, term.ErrNoSession) {
		return fmt.Errorf("node: forget %s: %w", name, err)
	}
	return nil
}

// restarts applies a restart policy to an exit code.
func restarts(policy string, code int) bool {
	switch policy {
	case RestartNever:
		return false
	case RestartOnFailure:
		return code != 0
	default:
		return true
	}
}

// start launches a record that should run when the node runs fewer than
// Config.Max sessions; otherwise the record waits (a later pass starts it
// when one stops).
func (n *Node) start(ctx context.Context, r Record, s *state, kind, actor string) error {
	if n.active >= n.cfg.Max {
		s.Observed, s.PID, s.NextAttemptAt = ObservedWaiting, 0, time.Time{}
		return nil
	}
	if err := n.launch(ctx, r, s, kind, actor); err != nil {
		return err
	}
	n.active++
	return nil
}

// launch starts r's session and records it as an event of kind asked by
// actor.
func (n *Node) launch(ctx context.Context, r Record, s *state, kind, actor string) error {
	_, _, adapterErr := n.cfg.agentFor(r)
	if err := errors.Join(adapterErr, checkDirRoot(r), checkRoleProfile(r, profileDirs()), checkRoleArgsFor(r, n.cfg.adapterFor(r))); err != nil {
		s.Observed = ObservedStopped
		if k := len(s.events); k == 0 || s.events[k-1].Kind != wire.HostedEventRefused { // one event per refusal run, not one per pass
			n.event(s, wire.HostedEventRefused, wire.HostedActorNode, nil)
		}
		return fmt.Errorf("node: launch %s refused: %w", TerminalName(r.ID), err)
	}
	spec := term.Spec{
		Name: TerminalName(r.ID),
		Argv: n.cfg.Argv(r, s.Launched),
		Dir:  r.Directory,
		Env:  n.cfg.ChildEnv(r),
		Cols: r.Cols,
		Rows: r.Rows,
	}
	if spec.Cols <= 0 {
		spec.Cols = DefaultCols
	}
	if spec.Rows <= 0 {
		spec.Rows = DefaultRows
	}
	n.installPlugin(ctx, r, s, spec.Env)
	if err := n.backend.Launch(ctx, spec); err != nil {
		s.Observed = ObservedStopped
		return fmt.Errorf("node: launch %s: %w", spec.Name, err)
	}
	s.resumed = s.Launched
	s.agent = n.cfg.adapterFor(r)
	s.Launched = true
	if s.Run == 0 {
		s.Run = r.LastRun
	}
	s.Run++
	s.next, s.NextAttemptAt = time.Time{}, time.Time{}
	s.Observed, s.PID, s.StartedAt = ObservedStarting, 0, n.cfg.Now()
	n.event(s, kind, actor, nil)
	return nil
}

// Restart stops the record's session and launches it again at once with
// resume, without backoff, clearing a crash loop. A record that should not
// run, or is not assigned, is left alone.
func (n *Node) Restart(ctx context.Context, id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	i := slices.IndexFunc(n.records, func(r Record) bool { return r.ID == id })
	if i < 0 || n.records[i].Desired == DesiredStopped {
		return nil
	}
	r := n.records[i]
	s := n.stateOf(r)
	name := TerminalName(id)
	infos, err := n.backend.List(ctx)
	if err != nil {
		return fmt.Errorf("node: list sessions: %w", err)
	}
	for _, in := range infos {
		if in.Name != name {
			continue
		}
		if !s.fresh {
			s.Launched = true
		}
		if err := n.remove(ctx, name, in.Alive); err != nil {
			return err
		}
	}
	s.fresh = false
	s.clearFailures()
	return n.launch(ctx, r, s, wire.HostedEventRestart, wire.HostedActorAdmin)
}

// Input types b into the session of the assigned record id, unchanged (a
// browser viewer with an input ticket). An unassigned id is ignored.
func (n *Node) Input(ctx context.Context, id string, b []byte) error {
	if !n.owns(id) {
		return nil
	}
	return n.backend.Input(ctx, TerminalName(id), b)
}

// Resize changes the terminal size of the assigned record id's session. An
// unassigned id or an empty size is ignored.
func (n *Node) Resize(ctx context.Context, id string, cols, rows int) error {
	if !n.owns(id) || cols <= 0 || rows <= 0 {
		return nil
	}
	return n.backend.Resize(ctx, TerminalName(id), cols, rows)
}

// Default terminal size when a record carries none.
const (
	DefaultCols = 120
	DefaultRows = 40
)
