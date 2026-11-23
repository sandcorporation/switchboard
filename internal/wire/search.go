package wire

import "time"

// Search kinds (#481): what `GET /api/search` looks through.
const (
	SearchMail     = "mail"
	SearchReport   = "report"
	SearchQuestion = "question"
	SearchLeft     = "left"
	SearchRequest  = "request" // the owner's requests (#501): title, text and notes
)

// SearchKinds lists the kinds in their tie order (newest first, then this
// order).
var SearchKinds = []string{SearchMail, SearchReport, SearchQuestion, SearchLeft, SearchRequest}

// SearchHit is one match. ID is the record's id as its own command takes it
// (a message number, a report number, a question id, a left item number);
// Open is that command. From and To are the sender and recipient of mail,
// the author of a report, the asker of a question and the role that
// recorded a left item (To is then the directive's sender). Line is the
// matched line, cut around the match to about 160 characters, as text.
type SearchHit struct {
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	Title     string    `json:"title,omitempty"`
	Line      string    `json:"line"`
	Thread    int64     `json:"thread,omitempty"`
	Directive int64     `json:"directive,omitempty"`
	Open      string    `json:"open"`
}

// SearchResult is one page, newest first. Next is the cursor of the
// following page ("" when there is none); Cut is true when the time limit
// stopped the search before it was done (Hits are what it found until
// then). Scope is "all", or the group prefix a role's results are limited
// to.
type SearchResult struct {
	Hits  []SearchHit `json:"hits"`
	Next  string      `json:"next,omitempty"`
	Cut   bool        `json:"cut"`
	Scope string      `json:"scope"`
}
