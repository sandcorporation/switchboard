package wire

import "time"

// RemindKind is one row of the reminder settings (#586), GET
// /api/admin/settings "remind": a kind (mail, left, request), who its
// reminders go to, its time (After "" and AfterSeconds 0 when off) and
// since, when that was set, Overdue the open items quiet for the time, and
// for requests Waiting, those waiting on the owner (no mail). PUT
// /api/admin/settings/remind/{kind} takes RemindSet.
type RemindKind struct {
	Kind         string     `json:"kind"`
	To           string     `json:"to"`
	On           bool       `json:"on"`
	After        string     `json:"after"`
	AfterSeconds int64      `json:"after_seconds"`
	Since        *time.Time `json:"since"`
	Overdue      int        `json:"overdue"`
	Waiting      int        `json:"waiting"`
}

// RemindSet is the body of PUT /api/admin/settings/remind/{kind}: a
// duration such as 2h, "" or "0" off.
type RemindSet struct {
	After string `json:"after"`
}
