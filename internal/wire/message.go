// Package wire holds the JSON shapes of the HTTP API, which the server writes
// and the client reads, and the checks both sides make on them.
package wire

import (
	"strconv"
	"strings"
	"time"
)

type Message struct {
	ID          int64      `json:"id"`
	Sender      string     `json:"sender"`
	Recipient   string     `json:"recipient"`
	Subject     string     `json:"subject"`
	Body        string     `json:"body"`
	CreatedAt   time.Time  `json:"created_at"`
	TaskID      string     `json:"task_id,omitempty"`
	ThreadID    int64      `json:"thread_id,omitempty"`
	ReplyTo     int64      `json:"reply_to,omitempty"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`               // "normal" or "urgent"
	AddressedTo string     `json:"addressed_to,omitempty"` // label address the sender used (see labels.go)
	HeldID      int64      `json:"held_id,omitempty"`      // ID while held, when the message moved to a session
	HeldAt      *time.Time `json:"held_at,omitempty"`      // when the message was held for a label
	DeliveredAt *time.Time `json:"delivered_at,omitempty"` // when held mail moved to a session
	Directive   bool       `json:"directive,omitempty"`    // a reply that carries new work (reply --directive); plain sends are directives anyway
	NoReply     bool       `json:"no_reply,omitempty"`     // needs no answer (send/reply --no-reply): never an open directive
	ViewOnly    bool       `json:"view_only,omitempty"`    // read/inbox --of response only: a boss role is viewing another mailbox, state untouched (#365)
	NoSession   bool       `json:"no_session,omitempty"`   // set on the send response only: no session of the recipient role is attached (#300)
	// RecipientState: set on the send and reply responses only, the
	// receiving mailbox's state (#354).
	RecipientState *RecipientState `json:"recipient_state,omitempty"`
	Done           bool            `json:"done,omitempty"`            // reply --complete (--done): the reply is the final word and marks the replied-to mail complete (#269, #327)
	Complete       []int64         `json:"complete,omitempty"`        // send --complete: mail the sender received to mark complete with the send (#327); request only
	Left           []string        `json:"left,omitempty"`            // request only (#470): what the answer ending a directive left undone (--left, repeatable)
	LeftNone       bool            `json:"left_none,omitempty"`       // request only (#470): the answer ending a directive left nothing undone (--left-none)
	DirectiveState string          `json:"directive_state,omitempty"` // reply response: the replied-to directive's state, open or done (#269)
	IdempotencyKey string          `json:"-"`
	// AnswersDirective: this mail answers a directive the reader sent, and
	// the directive's state (#275). Only the directive's sender sees it.
	AnswersDirective *DirectiveRef `json:"answers_directive,omitempty"`
}

// RecipientState is the receiving mailbox's state in a send or reply
// response (#354).
type RecipientState struct {
	Mailbox string `json:"mailbox"`
	Live    bool   `json:"live"`              // a session reads the mailbox now
	EndedS  int64  `json:"ended_s,omitempty"` // not live: seconds since its last session's lease ended (0: none ever, or just now)
	Held    bool   `json:"held,omitempty"`    // the mail is held for that session and goes to it when it comes back
	// LiveSlots: mail to a base whose slot 1 has no session lists the live
	// numbered mailboxes of that base ("<base>-2", ...).
	LiveSlots []string `json:"live_slots,omitempty"`
}

// ErrAmbiguousRecipient is the error code of a send or reply a role addressed
// to a base name while more than one slot of that base is live (#364).
const ErrAmbiguousRecipient = "ambiguous_recipient"

// AmbiguousRecipient is the 409 body of such a send or reply: the live slots
// to choose from. "<base>-1" names slot 1 and is sent.
type AmbiguousRecipient struct {
	Error     string     `json:"error"` // ErrAmbiguousRecipient
	Message   string     `json:"message"`
	Recipient string     `json:"recipient"`
	LiveSlots []LiveSlot `json:"live_slots"`
}

// LiveSlot is one live slot of an AmbiguousRecipient: its mailbox name
// ("<base>" for slot 1 is given as "<base>-1"), machine, folder and label.
type LiveSlot struct {
	Role      string `json:"role"`
	Machine   string `json:"machine"`
	Directory string `json:"directory"`
	Label     string `json:"label,omitempty"`
}

// Directive states on the wire: open while the answered rule's Open holds
// (mail.AnsweredRule), else done.
const (
	DirectiveOpen = "open"
	DirectiveDone = "done"
)

// DirectiveRef names a directive and its state: answers_directive on a
// message, a mail event or a watch entry (#275).
type DirectiveRef struct {
	ID    int64  `json:"id"`
	State string `json:"state"` // DirectiveOpen or DirectiveDone
}

func ValidRole(role string) bool {
	if len(role) < 1 || len(role) > 128 {
		return false
	}
	for _, c := range role {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._/-", c)) {
			return false
		}
	}
	return true
}

func ParseID(x string) (int64, error) { return strconv.ParseInt(x, 10, 64) }

// Closed is the answer of POST /api/messages/{id}/close and
// /api/admin/messages/{id}/close: the directive needs no answer any more.
type Closed struct {
	ID     int64 `json:"id"`
	Closed bool  `json:"closed"`
	// Completed is the answer closed by its ID (close <answer ID>, #327):
	// the caller's copy of it is complete too.
	Completed int64 `json:"completed,omitempty"`
	// Withdrawn: the recipient had not read the directive, so closing took
	// it back from the recipient's mailbox (#354).
	Withdrawn bool `json:"withdrawn,omitempty"`
}

// Asked is the ID the close was asked for: the answer when an answer closed
// its directive (Completed), else the directive (ID).
func (c Closed) Asked() int64 {
	if c.Completed != 0 {
		return c.Completed
	}
	return c.ID
}

// Withdrawn is the answer of POST /api/messages/{id}/withdraw (#354).
type Withdrawn struct {
	ID        int64 `json:"id"`
	Withdrawn bool  `json:"withdrawn"`
}
