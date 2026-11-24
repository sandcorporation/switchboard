package mail

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/requests"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// The ledger of what is left (#470). An answer that ends a directive says
// what it left undone; each thing is an open row of left_items until the
// directive's sender raises it as an issue, passes it on in a new
// directive, marks it done or drops it (#553), or an admin does.
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
	items []string
	none  bool
}

func claimOf(items []string, none bool) leftClaim {
	c := leftClaim{none: none}
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
	if ending && len(c.items) == 0 && !c.none {
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
// items, else a none row (kept once per directive). problem has refused an
// answer that says neither (#471), so no new unrecorded row is made; the
// ones from before stay readable and endable.
func recordLeft(tx *sql.Tx, directive int64, role string, c leftClaim, now time.Time) error {
	at := now.UTC().Format(time.RFC3339Nano)
	if len(c.items) > 0 {
		return insertItems(tx, directive, role, c.items, at)
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO left_items(directive_id,kind,recorded_by,recorded_at,state) VALUES(?,?,?,?,?)`, directive, wire.LeftKindNone, role, at, wire.LeftNoneSet)
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
//	POST /api/left               left add: LeftAdd, items found after the
//	                             caller ended the directive (its recipient
//	                             or sender)
//	POST /api/left/{id}/issue    LeftEnd.URL: raised as an issue
//	POST /api/left/{id}/pass     LeftEnd.Directive: passed on in that
//	                             directive, sent by the item's sender
//	POST /api/left/{id}/done     done (#553)
//	POST /api/left/{id}/drop     LeftEnd.Reason: dropped, and why (#553)
//	POST /api/left/{id}/request  LeftEnd.Request: attached to that owner's
//	                             request (#564); the directive's sender or a
//	                             boss role, either seeing the request
//
// The role (base) that sent the item's directive ends it (issue, pass,
// done, drop), and so does a boss role over that directive (#570): a global
// boss for any item, a group boss for items of directives inside its groups
// (bossOver). A boss passes the item on in a directive the item's own
// sender sent, as the admin does; ended_by records who ended it. The
// dashboard admin ends items with AdminLeftAPI.
func (s *Service) LeftAPI(w http.ResponseWriter, r *http.Request, p identity.Principal, rest string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rest == "" {
		s.addLeft(w, r, p)
		return
	}
	item, action, in, ok := s.leftTarget(w, r, rest)
	if !ok {
		return
	}
	if action == "request" {
		d, err := s.Get(item.Directive)
		if !s.ids.IsBoss(p) && (err != nil || identity.BaseOf(s.db, d.Sender) != p.Base) {
			http.Error(w, "only the role that sent the directive or a boss role may attach what it left to a request", http.StatusForbidden)
			return
		}
		if err := requests.Linkable(s.db, s.ids, p, in.Request); err != nil {
			http.Error(w, "request: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.attachLeft(w, item.ID, in.Request)
		return
	}
	if action != "issue" && action != "pass" && action != "done" && action != "drop" {
		http.NotFound(w, r)
		return
	}
	d, err := s.Get(item.Directive)
	if err != nil {
		http.Error(w, "only the role that sent the directive or a boss role over it may end what it left", http.StatusForbidden)
		return
	}
	sender := identity.BaseOf(s.db, d.Sender)
	base := sender
	switch {
	case sender == p.Base, s.bossOver(p, d):
		// A boss passes the item on, as the dashboard admin does, in a
		// directive the item's own sender sent (#570).
	case s.recorderOver(p, item, d):
		// The role that wrote the item on a report passes it on in a
		// directive it sent.
		base = p.Base
	default:
		http.Error(w, "only the role that sent the directive, the role that wrote the item on a report it got, or a boss role over it may end what it left", http.StatusForbidden)
		return
	}
	s.endBy(w, item, action, in, base, p.Role)
}

// recorderOver reports whether p, the role (base) that wrote item, may end
// it though d was not sent by p (#570): d is new mail (a report, not a
// reply) that p got and ended, and p had sent the first directive between
// the two roles on the same work — the same task, or the same request
// chain (#564) — before d. Judged on each read, so items written before
// this rule are covered too. The worker that sent d keeps ending them as
// its sender; it never gains another role's items this way, since the
// first directive between them on that work is not its own.
func (s *Service) recorderOver(p identity.Principal, item wire.LeftItem, d wire.Message) bool {
	if identity.BaseOf(s.db, item.RecordedBy) != p.Base {
		return false
	}
	other := identity.BaseOf(s.db, d.Sender)
	if other == "" || other == p.Base {
		return false
	}
	var rid int64
	_ = s.db.QueryRow(requests.ChainSQL+`SELECT rid FROM chain WHERE id=?`, d.ID).Scan(&rid)
	if d.TaskID == "" && rid == 0 {
		return false
	}
	rows, err := s.db.Query(requests.ChainSQL+`SELECT p.sender,p.recipient FROM messages p LEFT JOIN chain cp ON cp.id=p.id
		WHERE p.id<? AND p.reply_to IS NULL AND ((?<>'' AND COALESCE(p.task_id,'')=?) OR (?<>0 AND cp.rid=?)) ORDER BY p.id`,
		d.ID, d.TaskID, d.TaskID, rid, rid)
	if err != nil {
		return false
	}
	// Read every row before resolving names: the store has one connection.
	var pairs [][2]string
	for rows.Next() {
		var sender, recipient string
		if rows.Scan(&sender, &recipient) != nil {
			rows.Close()
			return false
		}
		pairs = append(pairs, [2]string{sender, recipient})
	}
	rows.Close()
	for _, m := range pairs {
		// New mail (no reply_to) from a role is a directive (isDirectiveMail).
		from, to := identity.BaseOf(s.db, m[0]), identity.BaseOf(s.db, m[1])
		if from == p.Base && to == other {
			return true
		}
		if from == other && to == p.Base {
			return false
		}
	}
	return false
}

// bossOver reports whether p may end what directive d left though it did
// not send d (#570): a global boss always, a group boss when both d's
// sender and recipient are in the groups it is boss of (or its own).
func (s *Service) bossOver(p identity.Principal, d wire.Message) bool {
	see, ok := s.ids.BossScope(p)
	return ok && see(d.Sender) && see(d.Recipient)
}

// AdminLeftAPI serves POST /api/admin/left/{id}/{issue,pass,done,drop,request}
// for a signed-in dashboard admin (#470, #553, #564): issue, pass and done as
// the directive's sender would, drop with LeftEnd.Reason (required), request
// attaches the item to LeftEnd.Request. The admin
// routes also take a boss role's token (boss write, #399): admin is false
// then and all are refused; a role ends items with its CLI.
func (s *Service) AdminLeftAPI(w http.ResponseWriter, r *http.Request, admin bool, user, rest string) {
	if !admin {
		http.Error(w, "the ledger's dashboard actions are for a signed-in admin", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	item, action, in, ok := s.leftTarget(w, r, rest)
	if !ok {
		return
	}
	by := "admin:" + user
	switch action {
	case "request":
		var n int
		if in.Request <= 0 || s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE id=?`, in.Request).Scan(&n) != nil || n == 0 {
			http.Error(w, "request: no such request", http.StatusBadRequest)
			return
		}
		s.attachLeft(w, item.ID, in.Request)
	case "drop", "done":
		s.endBy(w, item, action, in, "", by)
	case "issue", "pass":
		d, err := s.Get(item.Directive)
		if err != nil {
			http.Error(w, "no such directive", http.StatusNotFound)
			return
		}
		s.endBy(w, item, action, in, identity.BaseOf(s.db, d.Sender), by)
	default:
		http.NotFound(w, r)
	}
}

// leftTarget reads "{id}/{action}" and the LeftEnd body; it answers the
// error itself when ok is false.
func (s *Service) leftTarget(w http.ResponseWriter, r *http.Request, rest string) (item wire.LeftItem, action string, in wire.LeftEnd, ok bool) {
	idPart, action, _ := strings.Cut(rest, "/")
	id, err := wire.ParseID(idPart)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return item, action, in, false
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return item, action, in, false
	}
	if item, err = s.leftItem(id); err != nil {
		http.Error(w, "no such item", http.StatusNotFound)
		return item, action, in, false
	}
	return item, action, in, true
}

// endBy raises item as an issue (in.URL), passes it on (in.Directive, a
// directive sent by base, not the item's own), marks it done or drops it
// (in.Reason, required), for by.
func (s *Service) endBy(w http.ResponseWriter, item wire.LeftItem, action string, in wire.LeftEnd, base, by string) {
	switch action {
	case "done":
		s.endLeft(w, item.ID, wire.LeftDone, by, "")
	case "drop":
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			http.Error(w, "reason: why it is dropped", http.StatusBadRequest)
			return
		}
		if len([]rune(reason)) > leftItemMax {
			http.Error(w, fmt.Sprintf("reason: at most %d characters", leftItemMax), http.StatusBadRequest)
			return
		}
		s.endLeft(w, item.ID, wire.LeftDropped, by, reason)
	case "issue":
		u, e := url.Parse(strings.TrimSpace(in.URL))
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.Error(w, "url: an http(s) issue URL", http.StatusBadRequest)
			return
		}
		s.endLeft(w, item.ID, wire.LeftIssue, by, u.String())
	case "pass":
		m, e := s.Get(CurrentID(s.db, in.Directive))
		if in.Directive <= 0 || e != nil || !s.isDirective(m) || identity.BaseOf(s.db, m.Sender) != base || m.ID == item.Directive {
			http.Error(w, "directive: a directive sent by the same role as the item's, other than that one", http.StatusBadRequest)
			return
		}
		s.endLeft(w, item.ID, wire.LeftPassed, by, fmt.Sprint(m.ID))
	}
}

// addLeft serves POST /api/left.
func (s *Service) addLeft(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in wire.LeftAdd
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	c := claimOf(in.Left, false)
	if len(c.items) == 0 {
		http.Error(w, "left: at least one item", http.StatusBadRequest)
		return
	}
	if msg := c.problem(false, 0); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	d, err := s.Get(CurrentID(s.db, in.Directive))
	if err != nil || !s.isDirective(d) {
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

// attachLeft attaches item id (not a none row) to request rid and answers
// the row (#564).
func (s *Service) attachLeft(w http.ResponseWriter, id, rid int64) {
	if _, err := s.db.Exec(`UPDATE left_items SET request_id=? WHERE id=? AND kind<>?`, rid, id, wire.LeftKindNone); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	it, err := s.leftItem(id)
	if err != nil {
		http.Error(w, "no such item", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, it)
}

// endLeft moves open item id to state, recording who, when and value, and
// answers the row. A row that is not open (or a none row) answers 409.
func (s *Service) endLeft(w http.ResponseWriter, id int64, state, by, value string) {
	res, err := s.db.Exec(`UPDATE left_items SET state=?,ended_by=?,ended_at=?,value=? WHERE id=? AND state=? AND kind<>?`, state, by, s.now().UTC().Format(time.RFC3339Nano), value, id, wire.LeftOpen, wire.LeftKindNone)
	if err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	it, err := s.leftItem(id)
	if err != nil {
		http.Error(w, "no such item", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if n == 0 {
		w.WriteHeader(http.StatusConflict)
	}
	writeJSON(w, it)
}

const leftCols = `l.id,l.directive_id,l.kind,l.text,l.recorded_by,l.recorded_at,l.state,l.ended_by,l.ended_at,l.value,COALESCE(m.sender,''),COALESCE(m.subject,''),` + requests.LeftRequestExpr + `,COALESCE(rq.title,'')`

// leftFrom reads left items with their directive and request (#564): the
// request they were attached to, else their directive's on the chain.
const leftFrom = ` FROM left_items l LEFT JOIN messages m ON m.id=l.directive_id LEFT JOIN chain c ON c.id=l.directive_id LEFT JOIN requests rq ON rq.id=` + requests.LeftRequestExpr

func scanLeft(sc interface{ Scan(...any) error }) (wire.LeftItem, error) {
	var it wire.LeftItem
	var rec, end string
	if err := sc.Scan(&it.ID, &it.Directive, &it.Kind, &it.Text, &it.RecordedBy, &rec, &it.State, &it.EndedBy, &end, &it.Value, &it.DirectiveFrom, &it.DirectiveSubject, &it.Request, &it.RequestTitle); err != nil {
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
	return scanLeft(s.db.QueryRow(requests.ChainSQL+`SELECT `+leftCols+leftFrom+` WHERE l.id=?`, id))
}

// OpenLeft is every open item and unrecorded row, oldest first: the 남긴 것
// rows of 진행 대기.
func OpenLeft(q *sql.DB) ([]wire.LeftItem, error) {
	rows, err := q.Query(requests.ChainSQL+`SELECT `+leftCols+leftFrom+` WHERE l.state=? ORDER BY l.id`, wire.LeftOpen)
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

// leftElsewhere is why complete N --left is refused when N neither ends a
// directive nor answers one role sent, with the command to give instead,
// its numbers filled in (#483).
func (s *Service) leftElsewhere(role string, m wire.Message) string {
	const head = "--left goes to a directive's ledger, and complete #%d records into none: "
	plain := fmt.Sprintf("complete %d (without --left) marks it complete", m.ID)
	d, ok := s.directiveAbove(m)
	if !ok {
		return fmt.Sprintf(head+"it answers no directive; %s", m.ID, plain)
	}
	ended := s.ended(d.ID)
	switch {
	case d.Sender == role && ended, d.Recipient == role && ended:
		return fmt.Sprintf(head+`directive #%d has ended, so use left add %d "ITEM" (repeatable)`, m.ID, d.ID, d.ID)
	case d.Sender == role:
		return fmt.Sprintf(head+`to end directive #%d, use close %d --left "ITEM" (repeatable) or --left-none`, m.ID, d.ID, d.ID)
	case d.Recipient == role:
		return fmt.Sprintf(head+`to end directive #%d, use reply %d --complete --left "ITEM" or complete %d --left "ITEM" (repeatable)`, m.ID, d.ID, d.ID, d.ID)
	}
	return fmt.Sprintf(head+"directive #%d is %s's to record what it left; %s", m.ID, d.ID, d.Sender, plain)
}

// directiveAbove is the directive m is or the nearest one up its reply
// chain.
func (s *Service) directiveAbove(m wire.Message) (wire.Message, bool) {
	for i := 0; i < maxReplyChain; i++ {
		if s.isDirective(m) {
			return m, true
		}
		if m.ReplyTo == 0 {
			return wire.Message{}, false
		}
		next, err := s.Get(m.ReplyTo)
		if err != nil {
			return wire.Message{}, false
		}
		m = next
	}
	return wire.Message{}, false
}
