// Package requests holds the owner's requests to a boss role (#501): one row
// per request the owner gave, with its full text, a short title, the
// owner's later words (notes), and the reports, panel questions and mail
// linked to it by number (their request_id). The boss role registers a
// request, adds notes and marks it done with a result line; a dashboard
// login checks it. The status is derived on every read, never stored.
//
// The package reads the request_id of reports, questions and messages
// directly; it imports no other domain package but identity, so mail, asks
// and reports may call Linkable.
package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Service serves requests over one database.
type Service struct {
	db         *sql.DB
	now        func() time.Time
	ids        *identity.Service
	mailStates MailStates
}

// New builds the service; mailStates tells which linked mail is an open or
// ended directive (nil: none is).
func New(db *sql.DB, now func() time.Time, ids *identity.Service, mailStates MailStates) *Service {
	return &Service{db: db, now: now, ids: ids, mailStates: mailStates}
}

// ErrUnknown is Linkable's error for a request that does not exist or that
// the caller may not see.
var ErrUnknown = errors.New("unknown request")

// Linkable reports whether p may link a report, a question or a mail to the
// request id: it exists and p may see its author. A nil error means yes.
func Linkable(db *sql.DB, ids *identity.Service, p identity.Principal, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w %d", ErrUnknown, id)
	}
	var author string
	err := db.QueryRow(`SELECT author FROM requests WHERE id=?`, id).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !ids.MaySee(p, author) {
		return fmt.Errorf("%w %d", ErrUnknown, id)
	}
	return err
}

// writeJSON writes v, a value of the API (any: the encoder takes any).
func writeJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

func decode(w http.ResponseWriter, r *http.Request, v interface{ Check() error }) bool {
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	if err := v.Check(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// API serves /api/requests for a boss role (403 otherwise): POST "" adds a
// request, POST {id}/notes adds the owner's later words, POST {id}/wait sets
// or ({"off":true}) clears the 「대표 확인 대기」 mark, POST {id}/done marks
// it done with a result line (409 with the list while work is left under
// it, #564); GET "" lists the requests the role may see
// (?status=open|waiting|done|progress|checked), GET {id} reads one. A retry
// of an add with the same idempotency key (header Idempotency-Key, else the
// body's idempotency_key) returns the first request with 200.
func (s *Service) API(w http.ResponseWriter, r *http.Request, p identity.Principal, rest string) {
	if !s.ids.IsBoss(p) {
		http.Error(w, "requests are for boss roles (SWITCHBOARD_BOSS_ROLES or a group's boss role)", http.StatusForbidden)
		return
	}
	visible := func(author string) bool { return s.ids.MaySee(p, author) }
	if r.Method == http.MethodGet {
		s.serve(w, r, rest, visible)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rest == "" {
		s.add(w, r, p)
		return
	}
	idText, action, _ := strings.Cut(rest, "/")
	id, err := wire.ParseID(idText)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var author string
	err = s.db.QueryRow(`SELECT author FROM requests WHERE id=?`, id).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !visible(author) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	now := stamp(s.now())
	switch action {
	case "notes":
		var q wire.RequestNoteNew
		if !decode(w, r, &q) {
			return
		}
		_, err = s.db.Exec(`INSERT INTO request_notes(request_id,text,created_at) VALUES(?,?,?)`, id, q.Text, now)
	case "done":
		var q wire.RequestDoneNew
		if !decode(w, r, &q) {
			return
		}
		// Work left refuses a new close (#564); a request already done or
		// checked stays as it is.
		d, derr := s.Detail(id)
		if derr != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		if msg := refuseDone(d); msg != "" {
			http.Error(w, msg, http.StatusConflict)
			return
		}
		// Marking done again (after later news reopened it) starts a new check.
		_, err = s.db.Exec(`UPDATE requests SET result=?,done_at=?,checked_at=NULL,checked_by='' WHERE id=?`, q.Result, now, id)
	case "wait":
		// The 「대표 확인 대기」 mark (#586): set or cleared, either is news.
		var q wire.RequestWaitNew
		if !decode(w, r, &q) {
			return
		}
		if q.Off {
			_, err = s.db.Exec(`UPDATE requests SET owner_wait_at=NULL,owner_wait_changed=? WHERE id=? AND owner_wait_at IS NOT NULL`, now, id)
		} else {
			_, err = s.db.Exec(`UPDATE requests SET owner_wait_at=?,owner_wait_changed=? WHERE id=? AND owner_wait_at IS NULL`, now, now, id)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not store", http.StatusInternalServerError)
		return
	}
	s.writeDetail(w, http.StatusOK, id)
}

func (s *Service) add(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var q wire.RequestNew
	if !decode(w, r, &q) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		key = q.IdempotencyKey
	}
	if id, ok := s.byIdem(p.Role, key); ok {
		s.writeDetail(w, http.StatusOK, id)
		return
	}
	var idem any
	if key != "" {
		idem = key
	}
	res, err := s.db.Exec(`INSERT INTO requests(author,title,text,created_at,idem) VALUES(?,?,?,?,?)`, p.Role, q.Title, q.Text, stamp(s.now()), idem)
	if err != nil {
		// A concurrent retry with the same key won the insert.
		if id, ok := s.byIdem(p.Role, key); ok {
			s.writeDetail(w, http.StatusOK, id)
			return
		}
		http.Error(w, "could not store request", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	s.writeDetail(w, http.StatusCreated, id)
}

func (s *Service) byIdem(author, key string) (int64, bool) {
	if key == "" {
		return 0, false
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM requests WHERE author=? AND idem=?`, author, key).Scan(&id)
	return id, err == nil
}

func (s *Service) writeDetail(w http.ResponseWriter, code int, id int64) {
	d, err := s.Detail(id)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
	writeJSON(w, d)
}

// AdminAPI serves /api/admin/requests: GET "" and GET {id} read every
// request; POST {id}/check and {id}/uncheck are a signed-in login's (admin
// true; a boss role reaching the admin routes gets 403): check a done
// request (409 when it is not done) or take a check back.
func (s *Service) AdminAPI(w http.ResponseWriter, r *http.Request, admin bool, user, rest string) {
	if r.Method == http.MethodGet {
		s.serve(w, r, rest, func(string) bool { return true })
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !admin {
		http.Error(w, "forbidden: only a dashboard login checks a request", http.StatusForbidden)
		return
	}
	idText, action, _ := strings.Cut(rest, "/")
	id, err := wire.ParseID(idText)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	d, err := s.Detail(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	switch action {
	case "check":
		if d.Status != wire.RequestDone && d.Status != wire.RequestChecked {
			http.Error(w, "the request is not done", http.StatusConflict)
			return
		}
		if d.Status == wire.RequestDone {
			_, err = s.db.Exec(`UPDATE requests SET checked_at=?,checked_by=? WHERE id=?`, stamp(s.now()), user, id)
		}
	case "uncheck":
		_, err = s.db.Exec(`UPDATE requests SET checked_at=NULL,checked_by='' WHERE id=?`, id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not store", http.StatusInternalServerError)
		return
	}
	s.writeDetail(w, http.StatusOK, id)
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, rest string, visible func(author string) bool) {
	if rest == "" {
		status := r.URL.Query().Get("status")
		if status != "" && status != wire.RequestOpen && !contains(wire.RequestStatuses, status) {
			http.Error(w, "status must be open, waiting, done, progress or checked", http.StatusBadRequest)
			return
		}
		list, err := s.list(r.Context(), visible, status)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, list)
		return
	}
	id, err := wire.ParseID(rest)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	d, err := s.Detail(id)
	if err == nil && !visible(d.Author) {
		err = sql.ErrNoRows
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, d)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// row is a request as stored, with what the status is derived from.
type row struct {
	wire.Request
	done, checked time.Time
	latest        time.Time // the latest note or linked item
	waitChanged   time.Time // when the waiting mark was last set or cleared
}

const requestCols = `id,author,title,text,created_at,result,COALESCE(done_at,''),COALESCE(checked_at,''),checked_by,COALESCE(owner_wait_at,''),COALESCE(owner_wait_changed,'')`

func scanRow(sc interface{ Scan(...any) error }) (*row, error) {
	var rw row
	var created, done, checked, wait, waitChanged string
	if err := sc.Scan(&rw.ID, &rw.Author, &rw.Title, &rw.Text, &created, &rw.Result, &done, &checked, &rw.CheckedBy, &wait, &waitChanged); err != nil {
		return nil, err
	}
	if wait != "" {
		t := parse(wait)
		rw.OwnerWait = &t
	}
	rw.waitChanged = parse(waitChanged)
	rw.CreatedAt = parse(created)
	if done != "" {
		rw.done = parse(done)
		rw.DoneAt = &rw.done
	}
	if checked != "" {
		rw.checked = parse(checked)
		rw.CheckedAt = &rw.checked
	}
	return &rw, nil
}

func parse(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func fromMS(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// items are one request's linked items, oldest first.
type items struct {
	notes     []wire.RequestNote
	questions []wire.RequestQuestion
	reports   []wire.RequestReport
	mail      []wire.RequestMail
	direct    map[int64]bool // mail sent with the request's number
	left      []leftRow
}

// gather reads the notes and linked items of the requests in want (every
// request when want is nil).
func (s *Service) gather(ctx context.Context, want map[int64]*row) (map[int64]*items, error) {
	out := map[int64]*items{}
	get := func(id int64) *items {
		it := out[id]
		if it == nil {
			it = &items{}
			out[id] = it
		}
		return it
	}
	where, args := `request_id IS NOT NULL`, []any{}
	if len(want) == 1 {
		for id := range want {
			where, args = `request_id=?`, []any{id}
		}
	}
	keep := func(id int64) bool { _, ok := want[id]; return want == nil || ok }
	rows, err := s.db.QueryContext(ctx, `SELECT request_id,id,text,created_at FROM request_notes WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rid int64
		var n wire.RequestNote
		var created string
		if err = rows.Scan(&rid, &n.ID, &n.Text, &created); err != nil {
			rows.Close()
			return nil, err
		}
		n.CreatedAt = parse(created)
		if keep(rid) {
			get(rid).notes = append(get(rid).notes, n)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT request_id,id,author,title,created_at FROM reports WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rid int64
		var rp wire.RequestReport
		var created string
		if err = rows.Scan(&rid, &rp.ID, &rp.Author, &rp.Title, &created); err != nil {
			rows.Close()
			return nil, err
		}
		rp.CreatedAt = parse(created)
		if keep(rid) {
			get(rid).reports = append(get(rid).reports, rp)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Mail by the request chain (#564): sent with the number, or below such
	// mail. Only mail sent with the number is news that reopens a done
	// request; the rest is listed and counted.
	rows, err = s.db.QueryContext(ctx, ChainSQL+`SELECT c.rid,m.id,m.sender,m.recipient,m.subject,m.created_at,m.request_id IS NOT NULL FROM chain c JOIN messages m ON m.id=c.id WHERE `+strings.ReplaceAll(where, "request_id", "c.rid")+` ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rid int64
		var m wire.RequestMail
		var created string
		var direct bool
		if err = rows.Scan(&rid, &m.ID, &m.Sender, &m.Recipient, &m.Subject, &created, &direct); err != nil {
			rows.Close()
			return nil, err
		}
		m.CreatedAt = parse(created)
		if keep(rid) {
			it := get(rid)
			it.mail = append(it.mail, m)
			if direct {
				if it.direct == nil {
					it.direct = map[int64]bool{}
				}
				it.direct[m.ID] = true
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT request_id,id,asker,questions,COALESCE(answers,''),status,created_at,COALESCE(answered_at,0) FROM questions WHERE `+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rid, created, answered int64
		var q wire.RequestQuestion
		var qs, as string
		if err = rows.Scan(&rid, &q.ID, &q.Asker, &qs, &as, &q.Status, &created, &answered); err != nil {
			return nil, err
		}
		if !keep(rid) {
			continue
		}
		q.CreatedAt = fromMS(created)
		if answered > 0 {
			t := fromMS(answered)
			q.AnsweredAt = &t
		}
		var list []wire.QuestionItem
		_ = json.Unmarshal([]byte(qs), &list)
		if len(list) > 0 {
			q.Header, q.Question = list[0].Header, list[0].Question
		}
		q.Answers = answerLines(as)
		get(rid).questions = append(get(rid).questions, q)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, s.gatherLeft(ctx, out, keep)
}

// answerLines is one line per answered question: the chosen labels and the
// free text, joined by " · ".
func answerLines(raw string) []string {
	out := []string{}
	var as []wire.QuestionAnswer
	if raw == "" || json.Unmarshal([]byte(raw), &as) != nil {
		return out
	}
	for _, a := range as {
		parts := append([]string{}, a.Selected...)
		if a.Other != "" {
			parts = append(parts, a.Other)
		}
		out = append(out, strings.Join(parts, " · "))
	}
	return out
}

// summarize sets the counts, the latest news and the status.
func summarize(rw *row, it *items) {
	if it == nil {
		it = &items{}
	}
	rw.Notes, rw.Reports, rw.Questions, rw.Mail = len(it.notes), len(it.reports), len(it.questions), len(it.mail)
	rw.OpenQuestions = 0
	var last *wire.RequestNews
	news := func(kind, id, title string, at time.Time) {
		if at.After(rw.latest) {
			rw.latest = at
		}
		if kind != "" && (last == nil || !at.Before(last.At)) {
			last = &wire.RequestNews{Kind: kind, ID: id, Title: title, At: at}
		}
	}
	for _, n := range it.notes {
		news("", "", "", n.CreatedAt)
	}
	for _, rp := range it.reports {
		news(wire.SearchReport, "R"+strconv.FormatInt(rp.ID, 10), rp.Title, rp.CreatedAt)
	}
	for _, m := range it.mail {
		if it.direct[m.ID] {
			news(wire.SearchMail, strconv.FormatInt(m.ID, 10), m.Subject, m.CreatedAt)
		}
	}
	for _, q := range it.questions {
		if q.Status == "pending" {
			rw.OpenQuestions++
		}
		title := q.Header
		if title == "" {
			title = q.Question
		}
		news(wire.SearchQuestion, q.ID, title, q.CreatedAt)
	}
	rw.Last = last
	rw.QuietSince = quietSince(rw, it)
	switch {
	case rw.OpenQuestions > 0 || rw.OwnerWait != nil:
		rw.Status = wire.RequestWaiting
	case rw.DoneAt != nil && !rw.latest.After(rw.done) && rw.CheckedAt != nil:
		rw.Status = wire.RequestChecked
	case rw.DoneAt != nil && !rw.latest.After(rw.done):
		rw.Status = wire.RequestDone
	default:
		rw.Status = wire.RequestProgress
	}
}

// List is every request, newest first.
func (s *Service) List(ctx context.Context) (wire.RequestList, error) {
	return s.list(ctx, func(string) bool { return true }, "")
}

// list is the requests whose author is visible, newest first; status keeps
// one status, or with "open" every one but checked.
func (s *Service) list(ctx context.Context, visible func(author string) bool, status string) (wire.RequestList, error) {
	out := wire.RequestList{Requests: []wire.Request{}}
	rows, err := s.db.QueryContext(ctx, `SELECT `+requestCols+` FROM requests ORDER BY id DESC`)
	if err != nil {
		return out, err
	}
	var all []*row
	for rows.Next() {
		rw, err := scanRow(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		if visible(rw.Author) {
			all = append(all, rw)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	its, err := s.gather(ctx, nil)
	if err != nil {
		return out, err
	}
	open, err := s.states(mailIDs(its), true)
	if err != nil {
		return out, err
	}
	set, err := s.setting(ctx)
	if err != nil {
		return out, err
	}
	for _, rw := range all {
		summarize(rw, its[rw.ID])
		rw.Overdue = s.overdue(rw.Request, set)
		rw.Remaining = openCount(its[rw.ID], open)
		if status == "" || rw.Status == status || status == wire.RequestOpen && rw.Status != wire.RequestChecked {
			out.Requests = append(out.Requests, rw.Request)
		}
	}
	return out, nil
}

// Detail is one request with its notes and linked items; sql.ErrNoRows when
// there is no such request.
func (s *Service) Detail(id int64) (wire.RequestDetail, error) {
	var d wire.RequestDetail
	rw, err := scanRow(s.db.QueryRow(`SELECT `+requestCols+` FROM requests WHERE id=?`, id))
	if err != nil {
		return d, err
	}
	its, err := s.gather(context.Background(), map[int64]*row{id: rw})
	if err != nil {
		return d, err
	}
	it := its[id]
	summarize(rw, it)
	set, err := s.setting(context.Background())
	if err != nil {
		return d, err
	}
	rw.Overdue = s.overdue(rw.Request, set)
	if it == nil {
		it = &items{}
	}
	d.Request = rw.Request
	d.NoteList = nonNil(it.notes)
	d.QuestionList = nonNil(it.questions)
	d.ReportList = nonNil(it.reports)
	d.MailList = nonNil(it.mail)
	states, err := s.states(mailIDs(its), false)
	if err != nil {
		return d, err
	}
	d.RemainingList, d.Finished = remaining(it, states)
	d.Remaining = len(d.RemainingList)
	sort.SliceStable(d.QuestionList, func(i, j int) bool { return d.QuestionList[i].CreatedAt.Before(d.QuestionList[j].CreatedAt) })
	return d, nil
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
