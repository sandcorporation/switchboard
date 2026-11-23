package wire

// API watches (#528): the server GETs an address on a period and mails a
// role when a rule's condition starts to hold or a new item shows up.

// Watch condition kinds.
const (
	WatchValue = "value" // a value at a JSON path compared with an operator
	WatchNew   = "new"   // keys of a list's items not seen before
)

// WatchOps are the value condition's operators, in the order forms show
// them: equal, not equal, greater, less, present, absent, contains.
var WatchOps = []string{"eq", "ne", "gt", "lt", "exists", "missing", "contains"}

// WatchHeader is one request header. A secret header's value is written
// once and never shown again, not even in part: a read carries Set (a value
// is stored), SetAt and Unreadable instead. A write of a secret header with an empty Value keeps the stored
// one.
type WatchHeader struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	Set    bool   `json:"set,omitempty"`
	SetAt  string `json:"set_at,omitempty"`
	// Unreadable: the server's secrets key no longer opens the stored
	// value; it must be written again (#525).
	Unreadable bool `json:"unreadable,omitempty"`
}

// WatchCond is a rule's condition. Value: Path, Op and Value (unused by
// exists and missing). New: List (the path of the array) and Key (the path
// inside an item that tells items apart). A path is dot separated; a number
// indexes an array ("items.0.status"); an empty path is the whole body.
type WatchCond struct {
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Op    string `json:"op,omitempty"`
	Value string `json:"value,omitempty"`
	List  string `json:"list,omitempty"`
	Key   string `json:"key,omitempty"`
}

// WatchRule is what a dashboard login writes. Subject is the mail subject
// template ({path} takes a value: from the body for a value rule, from the
// item for a new-item rule); empty uses the default. Window is how many
// minutes the same subject is folded into one mail; FailAfter how many
// failed fetches in a row tell the role.
type WatchRule struct {
	Name      string        `json:"name"`
	To        string        `json:"to"`
	URL       string        `json:"url"`
	Headers   []WatchHeader `json:"headers"`
	Every     int           `json:"every_min"`
	Cond      WatchCond     `json:"cond"`
	Subject   string        `json:"subject"`
	Urgent    bool          `json:"urgent"`
	Window    int           `json:"window_min"`
	FailAfter int           `json:"fail_after"`
	Enabled   bool          `json:"enabled"`
}

// Watch is a stored rule with the state of its last run.
type Watch struct {
	ID int64 `json:"id"`
	WatchRule
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	LastAt     string `json:"last_at,omitempty"`
	LastStatus int    `json:"last_status,omitempty"`
	LastOK     bool   `json:"last_ok"`
	LastResult string `json:"last_result,omitempty"`
	Matched    bool   `json:"matched"`
	Fails      int    `json:"fails"`
	Failing    bool   `json:"failing"`
	Seen       int    `json:"seen"`
}

// WatchTestRequest is POST /api/admin/watches/test: a rule to fetch once.
// ID, when set, lends the stored secret header values to headers written
// without one, and the stored seen keys to a new-item rule.
type WatchTestRequest struct {
	ID int64 `json:"id,omitempty"`
	WatchRule
}

// WatchTest is a test fetch's result. Nothing is stored and no mail is
// sent. Matched and Value are a value rule's; New (keys not seen before) and
// Items a new-item rule's — a rule that never ran counts every item as its
// baseline, so New is 0.
type WatchTest struct {
	Status  int    `json:"status"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Matched bool   `json:"matched"`
	Value   string `json:"value,omitempty"`
	New     int    `json:"new"`
	Items   int    `json:"items"`
	Subject string `json:"subject,omitempty"`
}

// WatchSummary is one row of GET /api/watches (`switchboard watch list`):
// no address, header or condition.
type WatchSummary struct {
	Name       string `json:"name"`
	To         string `json:"to"`
	Every      int    `json:"every_min"`
	Enabled    bool   `json:"enabled"`
	LastAt     string `json:"last_at,omitempty"`
	LastOK     bool   `json:"last_ok"`
	LastResult string `json:"last_result,omitempty"`
}
