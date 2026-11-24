package wire

import (
	"errors"
	"strings"
	"time"
)

// Request status (#501), derived on every read, never stored.
const (
	RequestWaiting  = "waiting"  // a linked panel question is unanswered
	RequestDone     = "done"     // the boss role marked it done and nothing came after; not checked yet
	RequestProgress = "progress" // anything else
	RequestChecked  = "checked"  // done, and a dashboard login checked it
)

// RequestOpen is the list filter (?status=open) for every request that is
// not checked.
const RequestOpen = "open"

// RequestStatuses lists the statuses in the order the dashboard groups them.
var RequestStatuses = []string{RequestWaiting, RequestDone, RequestProgress, RequestChecked}

// Request limits.
const (
	MaxRequestTitle  = 200
	MaxRequestText   = 64 << 10
	MaxRequestResult = 500
)

// RequestNew is the body of POST /api/requests (switchboard request add):
// the owner's words as given and a short title for the list.
type RequestNew struct {
	Title          string `json:"title"`
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// Check trims the title and refuses an empty title or text, a title over
// its limit or with a line break, and a text over its limit.
func (q *RequestNew) Check() error {
	q.Title = strings.TrimSpace(q.Title)
	switch {
	case q.Title == "":
		return errors.New("title required")
	case len(q.Title) > MaxRequestTitle || strings.ContainsAny(q.Title, "\r\n"):
		return errors.New("title must be one line of at most 200 bytes")
	}
	return checkRequestText(q.Text)
}

func checkRequestText(t string) error {
	switch {
	case strings.TrimSpace(t) == "":
		return errors.New("text required")
	case len(t) > MaxRequestText:
		return errors.New("text must be at most 64 KiB")
	}
	return nil
}

// RequestNoteNew is the body of POST /api/requests/{id}/notes (switchboard
// request note): more words of the owner on the same request.
type RequestNoteNew struct {
	Text string `json:"text"`
}

// Check refuses an empty text or one over its limit.
func (q *RequestNoteNew) Check() error { return checkRequestText(q.Text) }

// RequestDoneNew is the body of POST /api/requests/{id}/done (switchboard
// request done): the result in one line.
type RequestDoneNew struct {
	Result string `json:"result"`
}

// Check trims the result and refuses an empty one, one over its limit or
// one with a line break.
func (q *RequestDoneNew) Check() error {
	q.Result = strings.TrimSpace(q.Result)
	switch {
	case q.Result == "":
		return errors.New("result required")
	case len(q.Result) > MaxRequestResult || strings.ContainsAny(q.Result, "\r\n"):
		return errors.New("result must be one line of at most 500 bytes")
	}
	return nil
}

// RequestNews is the latest linked report, panel question or mail: its
// kind ("report", "question" or "mail", the search kinds), its id as its own
// command takes it, its title (a report's title, a question's first header,
// a mail's subject) and when it came.
type RequestNews struct {
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Request is one of the owner's requests to a boss role. Status is derived:
// waiting while a linked panel question is unanswered, done once the boss
// role marked it done and nothing was linked or noted after, checked once a
// dashboard login checked it after that, progress otherwise. The counts are
// the linked items; Last is the latest linked report, question or mail.
type Request struct {
	ID            int64        `json:"id"`
	Author        string       `json:"author"`
	Title         string       `json:"title"`
	Text          string       `json:"text"`
	CreatedAt     time.Time    `json:"created_at"`
	Status        string       `json:"status"`
	Result        string       `json:"result,omitempty"`
	DoneAt        *time.Time   `json:"done_at,omitempty"`
	CheckedAt     *time.Time   `json:"checked_at,omitempty"`
	CheckedBy     string       `json:"checked_by,omitempty"`
	Notes         int          `json:"notes"`
	Reports       int          `json:"reports"`
	Questions     int          `json:"questions"`
	OpenQuestions int          `json:"open_questions"`
	Mail          int          `json:"mail"`
	Last          *RequestNews `json:"last,omitempty"`
	// Remaining is how much work is left under the request (#564): its
	// unanswered directive mail, open panel questions and open left items.
	Remaining int `json:"remaining"`
}

// RequestList is GET /api/requests and /api/admin/requests, newest first.
type RequestList struct {
	Requests []Request `json:"requests"`
}

// RequestNote is one later word of the owner.
type RequestNote struct {
	ID        int64     `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// RequestQuestion is a linked panel question: its first header and
// question, its status (pending, answered, withdrawn) and the chosen
// answers as one line per question.
type RequestQuestion struct {
	ID         string     `json:"id"`
	Asker      string     `json:"asker"`
	Header     string     `json:"header"`
	Question   string     `json:"question"`
	Status     string     `json:"status"`
	Answers    []string   `json:"answers"`
	CreatedAt  time.Time  `json:"created_at"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
}

// RequestReport is a linked report.
type RequestReport struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// RequestMail is a linked mail: one sent with the request's number, or one
// below it on the request chain (#564).
type RequestMail struct {
	ID        int64     `json:"id"`
	Sender    string    `json:"sender"`
	Recipient string    `json:"recipient"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

// RequestDetail is GET /api/requests/{id}: the request and its linked
// items, oldest first.
type RequestDetail struct {
	Request
	NoteList     []RequestNote     `json:"note_list"`
	QuestionList []RequestQuestion `json:"question_list"`
	ReportList   []RequestReport   `json:"report_list"`
	MailList     []RequestMail     `json:"mail_list"`
	// RemainingList is the work left (#564), oldest first; Finished counts
	// what has ended.
	RemainingList []RequestOpenItem `json:"remaining_list"`
	Finished      RequestFinished   `json:"finished"`
}

// Kinds of a RequestOpenItem (#564).
const (
	RequestOpenMail     = "mail"     // directive mail its recipient has not ended
	RequestOpenQuestion = "question" // a panel question not answered
	RequestOpenLeft     = "left"     // a left item not ended
)

// RequestOpenItem is one piece of work left under a request: its kind, its
// ID as its own command takes it, its title (the mail's subject, the
// question's first header, the left item's text), who holds it (the mail's
// recipient, the question's asker, the role that recorded the left item),
// since when, and the command that ends it.
type RequestOpenItem struct {
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Who   string    `json:"who,omitempty"`
	At    time.Time `json:"at"`
	Close string    `json:"close"`
}

// RequestFinished counts the ended work under a request (#564): reports,
// directive mail that ended, left items that ended.
type RequestFinished struct {
	Reports int `json:"reports"`
	Mail    int `json:"mail"`
	Left    int `json:"left"`
}
