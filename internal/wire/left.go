package wire

import "time"

// Left kinds and states (#470). An item row is one thing an answer ending a
// directive left undone; a none row says nothing was left (one per
// directive); an unrecorded row is an ending answer from an older CLI that
// could not say (one per directive).
const (
	LeftKindItem       = "item"
	LeftKindNone       = "none"
	LeftKindUnrecorded = "unrecorded"

	LeftOpen    = "open"
	LeftIssue   = "issue"   // raised as an issue: Value is its URL
	LeftPassed  = "passed"  // passed on in a new directive: Value is its ID
	LeftDone    = "done"    // done (#553): Value is empty
	LeftDropped = "dropped" // dropped by an admin or the directive's sender: Value is why
	LeftNoneSet = "none"    // the state of a none row
)

// LeftNote is the optional body of POST /api/messages/{id}/complete and
// /close (#470): what the answer ending a directive left undone. Reply and
// send carry the same fields in Message.
type LeftNote struct {
	Left     []string `json:"left,omitempty"`
	LeftNone bool     `json:"left_none,omitempty"`
}

// LeftAdd is the body of POST /api/left (left add N TEXT): items found after
// the caller ended directive Directive.
type LeftAdd struct {
	Directive int64    `json:"directive"`
	Left      []string `json:"left"`
}

// LeftEnd is the body of POST /api/left/{id}/issue (URL), /pass (Directive),
// /done (empty) and /drop (Reason), and of the same /api/admin/left/ paths.
type LeftEnd struct {
	URL       string `json:"url,omitempty"`
	Directive int64  `json:"directive,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// LeftItem is one row of the ledger.
type LeftItem struct {
	ID         int64      `json:"id"`
	Directive  int64      `json:"directive"`
	Kind       string     `json:"kind"`
	Text       string     `json:"text,omitempty"`
	RecordedBy string     `json:"recorded_by"`
	RecordedAt time.Time  `json:"recorded_at"`
	State      string     `json:"state"`
	EndedBy    string     `json:"ended_by,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Value      string     `json:"value,omitempty"`
	// DirectiveFrom and DirectiveSubject are the directive's sender and
	// subject, for the views.
	DirectiveFrom    string `json:"directive_from,omitempty"`
	DirectiveSubject string `json:"directive_subject,omitempty"`
}

// LeftList is the answer of POST /api/left: the rows it stored.
type LeftList struct {
	Items []LeftItem `json:"items"`
}

// LeftRemind is the reminder of open left items (#553): GET
// /api/admin/settings reports it, PUT /api/admin/settings/left-remind takes
// After. After is a duration such as 24h, "" when off; Since is when the
// value was set (the time counted from), nil before the server first used
// it.
type LeftRemind struct {
	On           bool       `json:"on"`
	After        string     `json:"after"`
	AfterSeconds int64      `json:"after_seconds"`
	Since        *time.Time `json:"since"`
}
