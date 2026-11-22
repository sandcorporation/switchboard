package node

import (
	"context"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"

	"github.com/sandcorporation/switchboard/internal/term"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// LimitScan is how often the node reads its sessions' screens for the
// agent's usage-limit message (#190), as the session's adapter reads it.
const LimitScan = 5 * time.Second

// LimitHit is the newest usage-limit message found on a screen.
type LimitHit = agent.LimitHit

// DetectLimit looks for the default adapter's usage-limit message in a
// screen's rows and takes the lowest one, the newest; at is when the screen
// was read, the reference for a reset time without a date.
func DetectLimit(rows []string, at time.Time) (LimitHit, bool) {
	return agent.Default().DetectLimit(rows, at)
}

// ParseReset reads the reset time out of a usage-limit message seen at at
// (agent.ParseReset).
func ParseReset(msg string, at time.Time) time.Time {
	return agent.ParseReset(msg, at)
}

// limitSeen is one session's showing of the message: its fingerprint, how
// many times the screen shows it, the reset it gave, when the node first
// saw it, and whether it is spent (its reset passed). A spent showing stays
// ignored until a fresh one appears: the newest message on the screen has
// other words, or the same words show more times than before (#210). The
// reset is read once, when the showing is first seen, so a spent "resets
// 3pm" never turns into tomorrow's 3pm.
type limitSeen struct {
	text  string
	count int
	reset time.Time
	since time.Time
	spent bool
}

// scanLimits reads the screens of the live assigned sessions, at most every
// LimitScan, and updates what each shows. A session no longer showing the
// message drops its entry; one whose reset time passed is spent. Read
// errors leave the entry as it was. n.mu is held.
func (n *Node) scanLimits(ctx context.Context, byName map[string]term.Info) {
	now := n.cfg.Now()
	if !n.lastScan.IsZero() && now.Sub(n.lastScan) < LimitScan {
		n.expireLimits(now)
		return
	}
	n.lastScan = now
	live := map[string]bool{}
	for _, r := range n.records {
		in, ok := byName[TerminalName(r.ID)]
		if !ok || !in.Alive {
			continue
		}
		live[r.ID] = true
		g, err := n.backend.Screen(ctx, in.Name, 0)
		if err != nil {
			continue
		}
		hit, found := n.cfg.adapterFor(r).DetectLimit(g.ScreenText(), now)
		cur := n.limits[r.ID]
		switch {
		case !found:
			delete(n.limits, r.ID)
		case cur == nil || cur.text != hit.Text || hit.Count > cur.count:
			n.limits[r.ID] = &limitSeen{text: hit.Text, count: hit.Count, reset: hit.Reset, since: now}
		default:
			cur.count = hit.Count
		}
	}
	for id := range n.limits {
		if !live[id] {
			delete(n.limits, id)
		}
	}
	n.expireLimits(now)
}

func (n *Node) expireLimits(now time.Time) {
	for _, l := range n.limits {
		if !l.reset.IsZero() && !now.Before(l.reset) {
			l.spent = true
		}
	}
}

// Limit reports the usage limit as the node's screens show it: limited
// while any session shows a message that is not spent.
func (n *Node) Limit() wire.NodeLimit {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out wire.NodeLimit
	var reset int64
	for _, l := range n.limits {
		if l.spent {
			continue
		}
		out.Limited = true
		out.Since = max(out.Since, l.since.UnixMilli())
		if !l.reset.IsZero() {
			reset = max(reset, l.reset.UnixMilli())
		}
	}
	if out.Limited && reset > 0 {
		out.ResetAt = &reset
	}
	return out
}
