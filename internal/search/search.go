// Package search finds text in what the server keeps (#481): mail (subject
// and body), reports (title and body), panel questions (question text,
// options and the chosen answer) and left items (their text). A pattern is a
// substring or, with Regex, a Go regexp (RE2: linear time); both ignore case
// unless Case is set. Results come newest first, a page at a time with a
// cursor, under a time limit per request.
//
// The search scans the rows (no index): at the sizes the server keeps it is
// faster than an FTS5 trigram index once the rows are read, and a regexp or a
// pattern shorter than three characters could not use that index anyway
// (bench_test.go measures both). Who may see what is passed in as a function
// over role names; the package does not import identity.
//
// The pattern is never logged.
package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Defaults.
const (
	DefaultLimit  = 50
	MaxLimit      = 200
	DefaultBudget = 2 * time.Second
	LineRunes     = 160
	// checkEvery is how many rows pass between looks at the clock.
	checkEvery = 64
)

// ErrBadQuery is a query the caller must fix (400).
var ErrBadQuery = errors.New("bad search query")

// Query is one search. Kinds empty means every kind; Role matches a
// message's sender or recipient, a report's author, a question's asker or a
// left item's recorder and its directive's two sides (a slot or label
// address counts for its base role); Thread keeps mail of that thread, the
// reports linking it and the left items of its directives (questions have
// no thread); Since and Until bound the time (zero: open); After is a
// cursor from a previous page.
type Query struct {
	Pattern string
	Regex   bool
	Case    bool
	Kinds   []string
	Role    string
	Thread  int64
	Since   time.Time
	Until   time.Time
	Limit   int
	After   string
}

// See reports whether a role (a mailbox name, a base role or a label
// address) is in the caller's view; nil sees everything.
type See func(role string) bool

// Service searches one database.
type Service struct {
	db     *sql.DB
	Budget time.Duration // time limit per request; DefaultBudget when zero
	clock  func() time.Time
}

// New builds the service.
func New(db *sql.DB) *Service { return &Service{db: db, clock: time.Now} }

// hit is a result with its sort key.
type hit struct {
	wire.SearchHit
	rank int   // the kind's place in wire.SearchKinds
	key  int64 // the id within the kind (a question's seq)
}

// newer reports whether a comes before b in the result order: newest
// first, then by kind, then by the higher id.
func newer(a, b hit) bool {
	if !a.At.Equal(b.At) {
		return a.At.After(b.At)
	}
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	return a.key > b.key
}

func cursorOf(h hit) string { return fmt.Sprintf("%d.%d.%d", h.At.UnixNano(), h.rank, h.key) }

func parseCursor(s string) (hit, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return hit{}, fmt.Errorf("%w: cursor", ErrBadQuery)
	}
	var n [3]int64
	for i, p := range parts {
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return hit{}, fmt.Errorf("%w: cursor", ErrBadQuery)
		}
		n[i] = v
	}
	return hit{SearchHit: wire.SearchHit{At: time.Unix(0, n[0])}, rank: int(n[1]), key: n[2]}, nil
}

// compile turns the pattern into a regexp: a literal (quoted) unless Regex,
// case-insensitive unless Case.
func compile(q Query) (*regexp.Regexp, error) {
	if strings.TrimSpace(q.Pattern) == "" {
		return nil, fmt.Errorf("%w: empty pattern", ErrBadQuery)
	}
	src := q.Pattern
	if !q.Regex {
		src = regexp.QuoteMeta(src)
	}
	if !q.Case {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadQuery, err)
	}
	return re, nil
}

// scan is one request's running state: the matcher, the filters and the
// clock.
type scan struct {
	q        Query
	re       *regexp.Regexp
	lower    string // a case-insensitive word, lowered: the rows' quick check
	uncased  bool   // lower has no cased letter (Hangul, digits): no lowering
	see      See
	after    *hit
	limit    int
	deadline time.Time
	clock    func() time.Time
	rows     int
	cut      bool
}

// stop counts a row and reports whether the time limit has passed.
func (s *scan) stop() bool {
	if s.cut {
		return true
	}
	if s.rows%checkEvery == 0 && !s.clock().Before(s.deadline) {
		s.cut = true
	}
	s.rows++
	return s.cut
}

func (s *scan) inTime(t time.Time) bool {
	if !s.q.Since.IsZero() && t.Before(s.q.Since) {
		return false
	}
	if !s.q.Until.IsZero() && !t.Before(s.q.Until) {
		return false
	}
	return true
}

func (s *scan) beyond(h hit) bool { return s.after == nil || newer(*s.after, h) }

func (s *scan) visible(roles ...string) bool {
	if s.see == nil {
		return true
	}
	for _, r := range roles {
		if r != "" && s.see(r) {
			return true
		}
	}
	return false
}

func (s *scan) roleMatch(roles ...string) bool {
	if s.q.Role == "" {
		return true
	}
	for _, r := range roles {
		if r != "" && (r == s.q.Role || BaseOf(r) == s.q.Role) {
			return true
		}
	}
	return false
}

// match finds the pattern in the first field that holds it and returns
// that field's matched line.
func (s *scan) match(fields ...string) (string, bool) {
	for _, f := range fields {
		if s.lower != "" {
			if s.uncased && !strings.Contains(f, s.lower) || !s.uncased && !strings.Contains(strings.ToLower(f), s.lower) {
				continue
			}
		}
		if loc := s.re.FindStringIndex(f); loc != nil {
			return Line(f, loc[0], loc[1]), true
		}
	}
	return "", false
}

// BaseOf is the base role of a mailbox name: a slot "<base>-N" → base, a
// label address "label:<base>/<label>" → base, else the name.
func BaseOf(name string) string {
	if rest, ok := strings.CutPrefix(name, "label:"); ok {
		if i := strings.LastIndexByte(rest, '/'); i > 0 {
			return rest[:i]
		}
		return rest
	}
	if base, _, ok := wire.SplitSlot(name); ok {
		return base
	}
	return name
}

// Line returns the line of text holding [start,end), cut to about LineRunes
// characters around the match, with "…" where it was cut. Tabs and carriage
// returns become spaces.
func Line(text string, start, end int) string {
	ls := strings.LastIndexByte(text[:start], '\n') + 1
	le := len(text)
	if i := strings.IndexByte(text[end:], '\n'); i >= 0 {
		le = end + i
	}
	if end > le { // a match across lines: keep its first line
		end = le
	}
	before, match, after := []rune(text[ls:start]), []rune(text[start:end]), []rune(text[end:le])
	if len(match) >= LineRunes {
		return clean(string(match[:LineRunes-1])) + "…"
	}
	room := LineRunes - len(match)
	nb := min(len(before), room/2)
	na := min(len(after), room-nb)
	nb = min(len(before), room-na)
	var b strings.Builder
	if nb < len(before) {
		b.WriteString("…")
	}
	b.WriteString(string(before[len(before)-nb:]))
	b.WriteString(string(match))
	b.WriteString(string(after[:na]))
	if na < len(after) {
		b.WriteString("…")
	}
	return clean(b.String())
}

func clean(s string) string {
	s = strings.NewReplacer("\t", " ", "\r", " ").Replace(s)
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	return s
}

// Search runs q for a caller who sees what see allows. scope is reported
// back as the result's Scope ("all" when see is nil).
func (sv *Service) Search(ctx context.Context, q Query, see See, scope string) (wire.SearchResult, error) {
	re, err := compile(q)
	if err != nil {
		return wire.SearchResult{}, err
	}
	kinds := q.Kinds
	if len(kinds) == 0 {
		kinds = wire.SearchKinds
	}
	for _, k := range kinds {
		if !slices.Contains(wire.SearchKinds, k) {
			return wire.SearchResult{}, fmt.Errorf("%w: kind %q (mail, report, question, left)", ErrBadQuery, k)
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	budget := sv.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	s := &scan{q: q, re: re, see: see, limit: limit, deadline: sv.clock().Add(budget), clock: sv.clock}
	if !q.Regex && !q.Case {
		// A (?i) regexp has no fast literal search; lowering the row first
		// is several times quicker. The regexp still places the match.
		s.lower = strings.ToLower(q.Pattern)
		s.uncased = s.lower == strings.ToUpper(q.Pattern)
	}
	if q.After != "" {
		c, err := parseCursor(q.After)
		if err != nil {
			return wire.SearchResult{}, err
		}
		s.after = &c
	}
	var all []hit
	for _, k := range wire.SearchKinds {
		if !slices.Contains(kinds, k) {
			continue
		}
		var got []hit
		switch k {
		case wire.SearchMail:
			got, err = sv.mail(ctx, s)
		case wire.SearchReport:
			got, err = sv.reports(ctx, s)
		case wire.SearchQuestion:
			got, err = sv.questions(ctx, s)
		case wire.SearchLeft:
			got, err = sv.left(ctx, s)
		}
		if err != nil {
			return wire.SearchResult{}, err
		}
		all = append(all, got...)
		if s.cut {
			break
		}
	}
	slices.SortFunc(all, func(a, b hit) int {
		if newer(a, b) {
			return -1
		}
		if newer(b, a) {
			return 1
		}
		return 0
	})
	res := wire.SearchResult{Hits: []wire.SearchHit{}, Cut: s.cut, Scope: scope}
	if scope == "" {
		res.Scope = "all"
	}
	for i, h := range all {
		if i == limit {
			if !s.cut {
				res.Next = cursorOf(all[i-1])
			}
			break
		}
		res.Hits = append(res.Hits, h.SearchHit)
	}
	return res, nil
}

// take adds h when it is past the cursor and reports whether the kind has
// enough rows (limit+1: one more tells there is a next page).
func (s *scan) take(out *[]hit, h hit) bool {
	if s.beyond(h) {
		*out = append(*out, h)
	}
	return len(*out) > s.limit
}

func parseText(v string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (sv *Service) mail(ctx context.Context, s *scan) ([]hit, error) {
	q, args := `SELECT id,sender,recipient,subject,body,created_at,COALESCE(thread_id,0) FROM messages`, []any{}
	if s.q.Thread > 0 {
		q += ` WHERE thread_id=? OR id=?`
		args = append(args, s.q.Thread, s.q.Thread)
	}
	rows, err := sv.db.QueryContext(ctx, q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hit
	for rows.Next() {
		if s.stop() {
			break
		}
		var id, thread int64
		var from, to, subject, body, at string
		if err := rows.Scan(&id, &from, &to, &subject, &body, &at, &thread); err != nil {
			return nil, err
		}
		t := parseText(at)
		if !s.inTime(t) || !s.visible(from, to) || !s.roleMatch(from, to) {
			continue
		}
		line, ok := s.match(subject, body)
		if !ok {
			continue
		}
		n := strconv.FormatInt(id, 10)
		if thread == 0 {
			thread = id
		}
		h := hit{SearchHit: wire.SearchHit{Kind: wire.SearchMail, ID: n, At: t, From: from, To: to, Title: subject, Line: line, Thread: thread, Open: "read " + n}, rank: 0, key: id}
		if s.take(&out, h) {
			break
		}
	}
	return out, rows.Err()
}

func (sv *Service) reports(ctx context.Context, s *scan) ([]hit, error) {
	q, args := `SELECT id,author,title,body,created_at FROM reports`, []any{}
	if s.q.Thread > 0 {
		q += ` WHERE id IN (SELECT rm.report_id FROM report_mail rm LEFT JOIN messages m ON m.id=rm.message_id WHERE rm.message_id=? OR m.thread_id=?)`
		args = append(args, s.q.Thread, s.q.Thread)
	}
	rows, err := sv.db.QueryContext(ctx, q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hit
	for rows.Next() {
		if s.stop() {
			break
		}
		var id int64
		var author, title, body, at string
		if err := rows.Scan(&id, &author, &title, &body, &at); err != nil {
			return nil, err
		}
		t := parseText(at)
		if !s.inTime(t) || !s.visible(author) || !s.roleMatch(author) {
			continue
		}
		line, ok := s.match(title, body)
		if !ok {
			continue
		}
		n := strconv.FormatInt(id, 10)
		h := hit{SearchHit: wire.SearchHit{Kind: wire.SearchReport, ID: n, At: t, From: author, Title: title, Line: line, Open: "report show R" + n}, rank: 1, key: id}
		if s.take(&out, h) {
			break
		}
	}
	return out, rows.Err()
}

func (sv *Service) questions(ctx context.Context, s *scan) ([]hit, error) {
	if s.q.Thread > 0 { // questions have no thread
		return nil, nil
	}
	rows, err := sv.db.QueryContext(ctx, `SELECT id,asker,questions,answers,created_at,seq FROM questions ORDER BY seq DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hit
	for rows.Next() {
		if s.stop() {
			break
		}
		var id, asker, qs, as string
		var created, seq int64
		if err := rows.Scan(&id, &asker, &qs, &as, &created, &seq); err != nil {
			return nil, err
		}
		t := time.UnixMilli(created).UTC()
		if !s.inTime(t) || !s.visible(asker) || !s.roleMatch(asker) {
			continue
		}
		var items []wire.QuestionItem
		if json.Unmarshal([]byte(qs), &items) != nil {
			continue
		}
		var answers []wire.QuestionAnswer
		if as != "" {
			_ = json.Unmarshal([]byte(as), &answers)
		}
		var fields []string
		for _, it := range items {
			fields = append(fields, it.Question, it.Header)
			for _, o := range it.Options {
				fields = append(fields, o.Label, o.Description)
			}
		}
		for _, a := range answers {
			fields = append(fields, a.Selected...)
			fields = append(fields, a.Other)
		}
		line, ok := s.match(fields...)
		if !ok {
			continue
		}
		title := ""
		if len(items) > 0 {
			title = items[0].Question
		}
		h := hit{SearchHit: wire.SearchHit{Kind: wire.SearchQuestion, ID: id, At: t, From: asker, Title: title, Line: line, Open: "question show " + id}, rank: 2, key: seq}
		if s.take(&out, h) {
			break
		}
	}
	return out, rows.Err()
}

func (sv *Service) left(ctx context.Context, s *scan) ([]hit, error) {
	q, args := `SELECT l.id,l.directive_id,l.text,l.recorded_by,l.recorded_at,COALESCE(m.sender,''),COALESCE(m.recipient,''),COALESCE(m.thread_id,m.id,0)
		FROM left_items l LEFT JOIN messages m ON m.id=l.directive_id WHERE l.text<>''`, []any{}
	if s.q.Thread > 0 {
		q += ` AND COALESCE(m.thread_id,m.id)=?`
		args = append(args, s.q.Thread)
	}
	rows, err := sv.db.QueryContext(ctx, q+` ORDER BY l.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hit
	for rows.Next() {
		if s.stop() {
			break
		}
		var id, directive, thread int64
		var text, by, at, from, to string
		if err := rows.Scan(&id, &directive, &text, &by, &at, &from, &to, &thread); err != nil {
			return nil, err
		}
		t := parseText(at)
		if !s.inTime(t) || !s.visible(from, to) || !s.roleMatch(by, from, to) {
			continue
		}
		line, ok := s.match(text)
		if !ok {
			continue
		}
		d := strconv.FormatInt(directive, 10)
		h := hit{SearchHit: wire.SearchHit{Kind: wire.SearchLeft, ID: strconv.FormatInt(id, 10), At: t, From: by, To: from, Line: line, Thread: thread, Directive: directive, Open: "read " + d}, rank: 3, key: id}
		if s.take(&out, h) {
			break
		}
	}
	return out, rows.Err()
}
