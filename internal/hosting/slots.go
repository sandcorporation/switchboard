package hosting

import (
	"log"
	"regexp"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Slot tie-in (#189).
//
// A hosted session's identity is its slot: the switchboard plugin inside it
// registers (base, machine, directory, session id) exactly as a session
// launched by hand does, and gets its old number back because machine and
// directory stay the same across relaunches. The hub never registers or
// changes slots; it reads them through SlotOf to show the role with its
// number and presence, and to follow the conversation: when the plugin
// reports a conversation other than the record's (a /clear inside the
// terminal), the record takes it, so the next relaunch resumes it.
//
// Binding (#442). Sessions of one role launched by hand can hold slots in
// the same folder, so the folder alone does not say which slot is the
// record's. A run binds to the slot that reports the record's own
// conversation (the node launches it with --session-id or --resume of that
// id), leased and heard from in this run; until then nothing is followed.
// Once bound, the run follows that slot's role only. A new run binds anew.
// A conversation seen in the folder during the run that was not the
// record's (another session's, seen before binding or on another slot) is
// never followed: two processes that register the same plain folder share
// one slot row, whose session id then alternates between them.

// Slot is what SlotOf reports of a slot.
type Slot struct {
	Role      string // the numbered role, e.g. team/worker-1
	Directory string // as registered, possibly "<dir> #<8 hex>"
	SessionID string // the conversation the plugin last reported
	// LeaseUntil is when the lease ends; Seen is the last register or
	// heartbeat. Live is LeaseUntil in the future.
	LeaseUntil time.Time
	Seen       time.Time
	Live       bool
}

// SlotOf lists the slots of base held at machine in directory, the
// plugin's session-marked forms "<directory> #<8 hex>" included, leased or
// not. The server builds it on presence.
type SlotOf func(base, machine, directory string) ([]Slot, error)

// SetSlotOf gives the hub its slot reader. Without one the list carries no
// slot and no conversation is followed.
func (h *Hub) SetSlotOf(fn SlotOf) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.slotOf = fn
}

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidSessionID reports whether sid is an agent conversation id (a UUID).
func ValidSessionID(sid string) bool { return sessionIDPattern.MatchString(sid) }

// slotBinding is what the hub knows of a record's run and the folder's
// slots: the role it is bound to ("" until bound) and the conversations of
// other sessions seen there.
type slotBinding struct {
	run     int64 // the node's run number (HostedRuntime.Run); runSeen can move within a run
	role    string
	foreign map[string]bool
}

// followCandidate is a launched record whose current run an attached node
// reported, with the host time that run was first reported; only a
// running one is followed, any binds (the plugin can register while the
// node still reports the run starting).
type followCandidate struct {
	rec     wire.HostedSession
	run     int64
	running bool
}

// follow binds running records to their slots and adopts the conversations
// bound sessions moved to inside their terminals. A record binds to the
// leased slot that reports its own conversation (observe). A bound record
// follows its slot when that slot is leased, was heard from at or after
// the host first saw the current run (so a slot the previous process left
// behind never counts), and reports a valid
// conversation id other than the record's. The record keeps launched, so
// its node keeps the process and the next relaunch resumes the new id. It
// reports whether a record changed.
func (h *Hub) follow(recs []wire.HostedSession) bool {
	h.mu.Lock()
	slotOf := h.slotOf
	var cands []followCandidate
	for _, r := range recs {
		rt, reported := h.runtime[r.ID]
		_, attached := h.conns[r.Node]
		run, seen := h.runSeen[r.ID]
		if r.Launched && reported && attached && seen {
			cands = append(cands, followCandidate{rec: r, run: run, running: rt.State == stateRunning})
		}
	}
	h.mu.Unlock()
	if slotOf == nil || len(cands) == 0 {
		return false
	}
	machines, err := h.svc.nodeMachines()
	if err != nil {
		log.Printf("hosting: follow: %v", err)
		return false
	}
	changed := false
	for _, c := range cands {
		r, machine := c.rec, machines[c.rec.Node]
		if machine == "" {
			continue
		}
		slots, err := slotOf(r.Base, machine, r.Directory)
		if err != nil {
			log.Printf("hosting: follow %s: %v", r.ID, err)
			continue
		}
		prev, b := h.observe(r.ID, c.run, r.AgentSessionID, slots)
		if prev.role == "" || !c.running {
			continue // not bound before this pass, or not running: nothing to follow yet
		}
		sl, ok := slotByRole(slots, b.role)
		if !ok || !current(sl, c.run) || sl.SessionID == r.AgentSessionID || !ValidSessionID(sl.SessionID) || b.foreign[sl.SessionID] {
			continue
		}
		adopted, err := h.svc.adoptConversation(r.ID, r.AgentSessionID, sl.SessionID)
		if err != nil {
			log.Printf("hosting: follow %s: %v", r.ID, err)
			continue
		}
		changed = changed || adopted
	}
	return changed
}

// boundLocked is the role record id's current run is bound to, "" when
// none. h.mu is held.
func (h *Hub) boundLocked(id string) string {
	b, ok := h.bound[id]
	rt, reported := h.runtime[id]
	if !ok || !reported || b.run != int64(rt.Run) {
		return ""
	}
	return b.role
}

// current is whether slot sl is leased and was heard from at or after run.
func current(sl Slot, run int64) bool { return sl.Live && sl.Seen.UnixMilli() >= run }

// observe updates record id's binding for run from the folder's slots,
// where the record holds conversation sid: it binds to a leased slot
// reporting sid and notes the other sessions' conversations. Binding does
// not wait for a heartbeat after the run was first reported: the plugin
// can register before the node reports the run, and sid is the record's
// own (the node launched it), so a session can /clear right after. prev is the
// binding before, now after. Nothing is kept when run is no longer the
// record's current run.
func (h *Hub) observe(id string, run int64, sid string, slots []Slot) (prev, now slotBinding) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.runSeen[id]; !ok || r != run {
		return slotBinding{}, slotBinding{}
	}
	prev = h.bindingLocked(id)
	now = h.bindingLocked(id)
	if now.role == "" {
		for _, sl := range slots {
			if sl.Live && sl.SessionID == sid {
				now.role = sl.Role
				break
			}
		}
	}
	for _, sl := range slots {
		if sl.SessionID != "" && sl.SessionID != sid && (now.role == "" || sl.Role != now.role) {
			now.foreign[sl.SessionID] = true
		}
	}
	h.bound[id] = now
	return prev, now
}

// bindingLocked is a copy of record id's binding for its current run, a
// fresh one when the run is new. h.mu is held.
func (h *Hub) bindingLocked(id string) slotBinding {
	run := int64(h.runtime[id].Run)
	b, ok := h.bound[id]
	if !ok || b.run != run {
		return slotBinding{run: run, foreign: map[string]bool{}}
	}
	f := make(map[string]bool, len(b.foreign))
	for k := range b.foreign {
		f[k] = true
	}
	b.foreign = f
	return b
}

func slotByRole(slots []Slot, role string) (Slot, bool) {
	for _, sl := range slots {
		if sl.Role == role {
			return sl, true
		}
	}
	return Slot{}, false
}

// recordSlot is the slot shown for record r: the one its current run is
// bound to, else one reporting its conversation (a leased one first, then
// the one heard from last). A slot of another session in the same folder is
// never the record's.
func recordSlot(slots []Slot, r wire.HostedSession, bound string) (Slot, bool) {
	if bound != "" {
		return slotByRole(slots, bound)
	}
	var v Slot
	ok := false
	for _, sl := range slots {
		if sl.SessionID != r.AgentSessionID {
			continue
		}
		if !ok || sl.Live && !v.Live || sl.Live == v.Live && sl.Seen.After(v.Seen) {
			v, ok = sl, true
		}
	}
	return v, ok
}

// withSlots fills each record's Slot and Presence (Hub.List).
func (h *Hub) withSlots(recs []wire.HostedSession) {
	h.mu.Lock()
	slotOf := h.slotOf
	runs := make(map[string]int64, len(recs))
	runAt := make(map[string]int64, len(recs))
	for _, r := range recs {
		if run, seen := h.runSeen[r.ID]; seen {
			runAt[r.ID] = run
			if r.Launched {
				runs[r.ID] = run
			}
		}
	}
	h.mu.Unlock()
	if slotOf == nil || len(recs) == 0 {
		return
	}
	now := h.svc.clock()
	machines, err := h.svc.nodeMachines()
	if err != nil {
		log.Printf("hosting: slots: %v", err)
		return
	}
	for i, r := range recs {
		alive := r.Runtime.State == stateRunning
		recs[i].Presence = wire.PresenceOffline
		if alive {
			recs[i].Presence = wire.PresenceProcessAlive
		}
		machine := machines[r.Node]
		if machine == "" {
			continue
		}
		slots, err := slotOf(r.Base, machine, r.Directory)
		if err != nil {
			log.Printf("hosting: slot of %s: %v", r.ID, err)
			continue
		}
		bound := ""
		if run, ok := runs[r.ID]; ok {
			_, b := h.observe(r.ID, run, r.AgentSessionID, slots)
			bound = b.role
		}
		sl, ok := recordSlot(slots, r, bound)
		if !ok {
			recs[i].Detached = detached(alive, time.UnixMilli(runAt[r.ID]), runAt[r.ID] != 0, now)
			continue
		}
		recs[i].Slot = &wire.HostedSlot{Role: sl.Role, Directory: sl.Directory, SessionID: sl.SessionID, LeaseUntil: sl.LeaseUntil.UnixMilli(), Online: sl.Live}
		if sl.Live {
			recs[i].Presence = wire.PresenceOnline
		} else {
			// Last attached at its last heartbeat, or when this run began,
			// whichever is later.
			since := time.UnixMilli(runAt[r.ID])
			if sl.Seen.After(since) {
				since = sl.Seen
			}
			recs[i].Detached = detached(alive, since, true, now)
		}
	}
}

// AttachGrace is how long a running hosted session may go without a live
// slot before it is marked detached (#463). The agent has to start, load its
// plugin and register, and the first response of a fresh session can be
// slow, so the wait is generous: a session that has not attached within
// three minutes of starting (or of its slot's last heartbeat) is not going
// to by itself, for instance when the folder's own settings pick another
// mailbox profile.
const AttachGrace = 3 * time.Minute

// detached reports a running process, known since the given time, with no
// live slot for longer than AttachGrace. It only reads: the session is not
// restarted, stopped or announced.
func detached(running bool, since time.Time, known bool, now time.Time) bool {
	return running && known && now.Sub(since) > AttachGrace
}

// stateRunning is the node's observed state of a live process.
const stateRunning = "running"

// nodeMachines is each known node's machine name (from its hello), by
// node name.
func (s *Service) nodeMachines() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT name,machine FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, machine string
		if err := rows.Scan(&name, &machine); err != nil {
			return nil, err
		}
		out[name] = machine
	}
	return out, rows.Err()
}

// adoptConversation moves a launched record from conversation old to sid,
// keeping it launched; it does nothing (false) when the record no longer
// holds old or is not launched.
func (s *Service) adoptConversation(id, old, sid string) (bool, error) {
	res, err := s.db.Exec(`UPDATE hosted_sessions SET agent_session_id=?,updated_at=? WHERE id=? AND agent_session_id=? AND launched=1`,
		sid, s.clock().UnixMilli(), id, old)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
