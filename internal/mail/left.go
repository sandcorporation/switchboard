package mail

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// The ledger of what is left (#470). An answer that ends a directive says
// what it left undone; each thing is an open row of left_items until the
// directive's sender raises it as an issue or passes it on in a new
// directive, or an admin drops it.
//
// The answers that end a directive:
//   - reply N --complete (and send --complete N) by N's recipient,
//   - complete N by N's recipient while N has not ended,
//   - close N by N's sender while N has not ended (close <answer ID> ends
//     the directive it answers), the closer says what is left.
//
// Acknowledgements, progress replies and the complete of mail that is not a
// directive end nothing.

const (
	leftItemMax  = 2000 // characters of one item
	leftItemsMax = 50   // items in one request
)

// leftClaim is what an ending answer said about what it left.
type leftClaim struct {
	items  []string
	none   bool
	marked bool // the client records what is left (config.LeftHeaderName)
}

func claimOf(r *http.Request, items []string, none bool) leftClaim {
	c := leftClaim{none: none, marked: r.Header.Get(config.HeaderPrefix+config.LeftHeaderName) != ""}
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" {
			c.items = append(c.items, it)
		}
	}
	return c
}

// leftHowTo is the one line a refused ending answer shows.
const leftHowTo = `say what is left: --left "ITEM" (repeatable) or --left-none`

// problem is why the claim cannot be taken, "" when it can. ending: the
// request ends a directive.
func (c leftClaim) problem(ending bool, directive int64) string {
	switch {
	case len(c.items) > 0 && c.none:
		return "use --left or --left-none, not both"
	case len(c.items) > leftItemsMax:
		return fmt.Sprintf("at most %d --left items", leftItemsMax)
	}
	for _, it := range c.items {
		if len([]rune(it)) > leftItemMax {
			return fmt.Sprintf("a --left item is at most %d characters", leftItemMax)
		}
	}
	if ending && c.marked && len(c.items) == 0 && !c.none {
		return fmt.Sprintf("this answer ends directive #%d: %s", directive, leftHowTo)
	}
	return ""
}

// isDirectiveMail: mail that is a directive (IsDirective) and not system mail.
func isDirectiveMail(m wire.Message) bool {
	return !identity.IsSystemSender(m.Sender) && (FlowMail{ReplyTo: m.ReplyTo, Directive: m.Directive}).IsDirective()
}

// ended reports whether directive id has ended (completed by its recipient
// or closed).
func (s *Service) ended(id int64) bool {
	st, ok := s.directiveStateOf(id)
	return ok && st.Ended()
}

// recordLeft stores what an ending answer by role to directive said: its
// items, else a none row, else (an older CLI said nothing) an open
// unrecorded row. none and unrecorded rows are kept once per directive.
func recordLeft(tx *sql.Tx, directive int64, role string, c leftClaim, now time.Time) error {
	at := now.UTC().Format(time.RFC3339Nano)
	if len(c.items) > 0 {
		return insertItems(tx, directive, role, c.items, at)
	}
	kind, state := wire.LeftKindUnrecorded, wire.LeftOpen
	if c.none {
		kind, state = wire.LeftKindNone, wire.LeftNoneSet
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO left_items(directive_id,kind,recorded_by,recorded_at,state) VALUES(?,?,?,?,?)`, directive, kind, role, at, state)
	return err
}

func insertItems(tx *sql.Tx, directive int64, role string, items []string, at string) error {
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO left_items(directive_id,kind,text,recorded_by,recorded_at,state) VALUES(?,?,?,?,?,?)`, directive, wire.LeftKindItem, it, role, at, wire.LeftOpen); err != nil {
			return err
		}
	}
	return nil
}

// leftNote reads the optional LeftNote body of complete and close. An empty
// body is no note.
func leftNote(w http.ResponseWriter, r *http.Request) (wire.LeftNote, bool) {
	var n wire.LeftNote
	if r.Body == nil {
		return n, true
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&n)
	if err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return n, false
	}
	return n, true
}

// LeftAPI serves the role side of the ledger (#470), path after /api/left:
//
//	POST /api/left    left add: LeftAdd, items found after the caller ended
//	                  the directive (its recipient or sender)
func (s *Service) LeftAPI(w http.ResponseWriter, r *http.Request, p identity.Principal, rest string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rest != "" {
		http.NotFound(w, r)
		return
	}
	s.addLeft(w, r, p)
}

// addLeft serves POST /api/left.
func (s *Service) addLeft(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in wire.LeftAdd
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	c := claimOf(r, in.Left, false)
	if len(c.items) == 0 {
		http.Error(w, "left: at least one item", http.StatusBadRequest)
		return
	}
	if msg := c.problem(false, 0); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	d, err := s.Get(CurrentID(s.db, in.Directive))
	if err != nil || !isDirectiveMail(d) {
		http.Error(w, "directive: no such directive", http.StatusNotFound)
		return
	}
	if d.Recipient != p.Role && d.Sender != p.Role {
		http.Error(w, "only the directive's recipient or sender may add what it left", http.StatusForbidden)
		return
	}
	if !s.ended(d.ID) {
		http.Error(w, fmt.Sprintf("directive #%d has not ended: say what is left when you end it", d.ID), http.StatusConflict)
		return
	}
	var ids []int64
	err = store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		ids = nil
		at := s.now().UTC().Format(time.RFC3339Nano)
		for _, it := range c.items {
			res, err := tx.Exec(`INSERT INTO left_items(directive_id,kind,text,recorded_by,recorded_at,state) VALUES(?,?,?,?,?,?)`, d.ID, wire.LeftKindItem, it, p.Role, at, wire.LeftOpen)
			if err != nil {
				return err
			}
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		http.Error(w, "could not store", http.StatusInternalServerError)
		return
	}
	out := wire.LeftList{Items: []wire.LeftItem{}}
	for _, id := range ids {
		if it, err := s.leftItem(id); err == nil {
			out.Items = append(out.Items, it)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, out)
}

const leftCols = `l.id,l.directive_id,l.kind,l.text,l.recorded_by,l.recorded_at,l.state,l.ended_by,l.ended_at,l.value,COALESCE(m.sender,''),COALESCE(m.subject,'')`

func scanLeft(sc interface{ Scan(...any) error }) (wire.LeftItem, error) {
	var it wire.LeftItem
	var rec, end string
	if err := sc.Scan(&it.ID, &it.Directive, &it.Kind, &it.Text, &it.RecordedBy, &rec, &it.State, &it.EndedBy, &end, &it.Value, &it.DirectiveFrom, &it.DirectiveSubject); err != nil {
		return it, err
	}
	it.RecordedAt, _ = time.Parse(time.RFC3339Nano, rec)
	if end != "" {
		if t, err := time.Parse(time.RFC3339Nano, end); err == nil {
			it.EndedAt = &t
		}
	}
	return it, nil
}

func (s *Service) leftItem(id int64) (wire.LeftItem, error) {
	return scanLeft(s.db.QueryRow(`SELECT `+leftCols+` FROM left_items l LEFT JOIN messages m ON m.id=l.directive_id WHERE l.id=?`, id))
}

// OpenLeft is every open item and unrecorded row, oldest first: the 남긴 것
// rows of 진행 대기.
func OpenLeft(q *sql.DB) ([]wire.LeftItem, error) {
	rows, err := q.Query(`SELECT `+leftCols+` FROM left_items l LEFT JOIN messages m ON m.id=l.directive_id WHERE l.state=? ORDER BY l.id`, wire.LeftOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.LeftItem{}
	for rows.Next() {
		it, err := scanLeft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
