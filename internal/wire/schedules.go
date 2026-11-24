package wire

import (
	"errors"
	"strings"
	"time"
)

// Schedule kinds (#288, #292): cron fires on a five-field expression in the
// zone recorded at creation; once fires at one time.
const (
	ScheduleCron = "cron"
	ScheduleOnce = "once"
)

// Schedule states. active: it fires (or holds missed fires to deliver);
// paused: it does not fire, the fire times that pass are dropped and its
// expiry clock stands still (#404); ended: a once schedule delivered its
// fire; expired: it reached expires_at. A removed schedule is deleted.
const (
	ScheduleActive  = "active"
	SchedulePaused  = "paused"
	ScheduleEnded   = "ended"
	ScheduleExpired = "expired"
)

// What resuming a once schedule whose time passed while it was paused does
// with that fire (ScheduleResume.Passed).
const (
	PassedFire = "fire" // deliver it now
	PassedDrop = "drop" // drop it: the schedule ends
)

// SchedulePause is the body of POST /api/schedules/ID/pause (switchboard
// schedule pause): an optional reason, shown with who paused.
type SchedulePause struct {
	Reason string `json:"reason,omitempty"`
}

// MaxPauseReason caps a pause reason.
const MaxPauseReason = 500

// Check validates the body.
func (q *SchedulePause) Check() error {
	q.Reason = strings.TrimSpace(q.Reason)
	if len(q.Reason) > MaxPauseReason {
		return errors.New("reason is too long")
	}
	return nil
}

// ScheduleResume is the body of POST /api/schedules/ID/resume. Passed
// (PassedFire or PassedDrop) is required when a once schedule's time passed
// while it was paused, and ignored otherwise.
type ScheduleResume struct {
	Passed string `json:"passed,omitempty"`
}

// Check validates the body.
func (q *ScheduleResume) Check() error {
	switch q.Passed {
	case "", PassedFire, PassedDrop:
		return nil
	}
	return errors.New("passed: fire or drop")
}

// ScheduleActor is who paused a schedule: a role, or an admin by name.
type ScheduleActor struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin,omitempty"`
}

// Label is the actor as the dashboard and the CLI show it: 관리자 <name> or
// 역할 <role>.
func (a ScheduleActor) Label() string {
	if a.Admin {
		return "관리자 " + a.Name
	}
	return "역할 " + a.Name
}

// MaxSchedulePrompt caps the prompt a fire delivers.
const MaxSchedulePrompt = 16 << 10

// Schedule is a prompt delivered to its owner role at times
// (GET /api/schedules/ID). Missed counts the fires not yet delivered because
// no session of the role was attached (or the previous fire's mail was still
// unread); MissedLastAt is the last of them.
type Schedule struct {
	ID           int64      `json:"id"`
	Role         string     `json:"role"`
	Machine      string     `json:"machine"`
	Kind         string     `json:"kind"`
	Label        string     `json:"label"`
	Prompt       string     `json:"prompt"`
	Cron         string     `json:"cron,omitempty"`
	TZ           string     `json:"tz"`
	State        string     `json:"state"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	NextFireAt   *time.Time `json:"next_fire_at,omitempty"`
	LastFireAt   *time.Time `json:"last_fire_at,omitempty"`
	Missed       int        `json:"missed,omitempty"`
	MissedLastAt *time.Time `json:"missed_last_at,omitempty"`
	// Extends is false for a row created with --no-extend.
	Extends bool `json:"extends"`
	// A paused schedule (#404): when, by whom, why (optional), and Passed
	// when it is a once schedule whose time went by while paused (resume
	// asks for PassedFire or PassedDrop).
	PausedAt    *time.Time     `json:"paused_at,omitempty"`
	PausedBy    *ScheduleActor `json:"paused_by,omitempty"`
	PauseReason string         `json:"pause_reason,omitempty"`
	Passed      bool           `json:"passed,omitempty"`
}

// ScheduleCreate is the body of POST /api/schedules (switchboard schedule
// add) and of the dashboard's POST /api/admin/schedules (#551). A cron
// schedule has Cron and TZ (the machine's zone); a once schedule has At.
type ScheduleCreate struct {
	// Role is the role the schedule is for: the admin path's, a role in the
	// caller's view (#551). On the role path it is the token's role or empty.
	Role           string     `json:"role,omitempty"`
	Kind           string     `json:"kind"`
	Cron           string     `json:"cron,omitempty"`
	At             *time.Time `json:"at,omitempty"`
	TZ             string     `json:"tz"`
	Label          string     `json:"label,omitempty"`
	Prompt         string     `json:"prompt"`
	Machine        string     `json:"machine"`
	ExpiresIn      int64      `json:"expires_in,omitempty"` // seconds; 0 is DefaultWatchExpiry
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	NoExtend       bool       `json:"no_extend,omitempty"` // fixed expiry
}

// Check fills the label from the prompt and refuses a missing or over-long
// field and a kind without its time. The cron expression and the zone are
// checked by the server (internal/cron).
func (q *ScheduleCreate) Check() error {
	q.Label, q.Machine, q.Cron = oneLine(q.Label), strings.TrimSpace(q.Machine), strings.TrimSpace(q.Cron)
	switch {
	case q.Kind == ScheduleCron && q.Cron == "":
		return errors.New("cron schedule needs cron")
	case q.Kind == ScheduleOnce && (q.At == nil || q.At.IsZero()):
		return errors.New("once schedule needs at")
	case q.Kind != ScheduleCron && q.Kind != ScheduleOnce:
		return errors.New("kind must be cron or once")
	case strings.TrimSpace(q.Prompt) == "":
		return errors.New("prompt required")
	case len(q.Prompt) > MaxSchedulePrompt:
		return errors.New("prompt over 16 KB")
	case q.Machine == "":
		return errors.New("machine required")
	case q.ExpiresIn < 0:
		return errors.New("expires_in must not be negative")
	}
	if len(q.Label) > MaxJobLabel {
		return errors.New("label over 200 bytes")
	}
	return nil
}

// ScheduleEdit is the body of PUT /api/schedules/ID and
// /api/admin/schedules/ID (switchboard schedule edit, #516). Kind, with
// Cron or At and TZ as in ScheduleCreate, replaces the time; Prompt and
// Label, when given, replace those. The ID, the fire record (last fire,
// missed count) and the expiry stay.
type ScheduleEdit struct {
	Kind   string     `json:"kind,omitempty"` // "": the time stays
	Cron   string     `json:"cron,omitempty"`
	At     *time.Time `json:"at,omitempty"`
	TZ     string     `json:"tz,omitempty"` // "": the schedule's zone stays
	Prompt *string    `json:"prompt,omitempty"`
	Label  *string    `json:"label,omitempty"` // "": 예약 #ID
}

// Check refuses an edit that changes nothing, a kind without its time and
// a missing or over-long prompt or label.
func (q *ScheduleEdit) Check() error {
	q.Cron, q.TZ = strings.TrimSpace(q.Cron), strings.TrimSpace(q.TZ)
	if q.Label != nil {
		l := oneLine(*q.Label)
		q.Label = &l
	}
	switch {
	case q.Kind == "" && q.Prompt == nil && q.Label == nil:
		return errors.New("nothing to change: give a time, prompt or label")
	case q.Kind == "" && (q.Cron != "" || q.At != nil):
		return errors.New("a new time needs its kind")
	case q.Kind == ScheduleCron && q.Cron == "":
		return errors.New("cron schedule needs cron")
	case q.Kind == ScheduleOnce && (q.At == nil || q.At.IsZero()):
		return errors.New("once schedule needs at")
	case q.Kind != "" && q.Kind != ScheduleCron && q.Kind != ScheduleOnce:
		return errors.New("kind must be cron or once")
	case q.Prompt != nil && strings.TrimSpace(*q.Prompt) == "":
		return errors.New("prompt required")
	case q.Prompt != nil && len(*q.Prompt) > MaxSchedulePrompt:
		return errors.New("prompt over 16 KB")
	case q.Label != nil && len(*q.Label) > MaxJobLabel:
		return errors.New("label over 200 bytes")
	}
	return nil
}
