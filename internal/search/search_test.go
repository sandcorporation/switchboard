package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func open(t testing.TB) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func exec(t testing.TB, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func addMail(t testing.TB, db *sql.DB, from, to, subject, body string, at time.Time, thread int64) int64 {
	var th any
	if thread > 0 {
		th = thread
	}
	return exec(t, db, `INSERT INTO messages(sender,recipient,subject,body,created_at,thread_id) VALUES(?,?,?,?,?,?)`, from, to, subject, body, at.Format(time.RFC3339Nano), th)
}

func addReport(t testing.TB, db *sql.DB, author, title, body string, at time.Time, links ...int64) int64 {
	id := exec(t, db, `INSERT INTO reports(author,title,body,created_at) VALUES(?,?,?,?)`, author, title, body, at.Format(time.RFC3339Nano))
	for _, m := range links {
		exec(t, db, `INSERT INTO report_mail(report_id,message_id) VALUES(?,?)`, id, m)
	}
	return id
}

var qseq int64

func addQuestion(t testing.TB, db *sql.DB, asker, question string, options []string, chosen string, at time.Time) string {
	qseq++
	item := wire.QuestionItem{Header: "h", Question: question}
	for _, o := range options {
		item.Options = append(item.Options, wire.QuestionOption{Label: o})
	}
	qs, _ := json.Marshal([]wire.QuestionItem{item})
	as := ""
	if chosen != "" {
		b, _ := json.Marshal([]wire.QuestionAnswer{{Selected: []string{chosen}}})
		as = string(b)
	}
	id := fmt.Sprintf("q-%d", qseq)
	exec(t, db, `INSERT INTO questions(id,asker,asker_base,questions,answers,created_at,seq) VALUES(?,?,?,?,?,?,?)`, id, asker, BaseOf(asker), string(qs), as, at.UnixMilli(), qseq)
	return id
}

func addLeft(t testing.TB, db *sql.DB, directive int64, by, text string, at time.Time) int64 {
	return exec(t, db, `INSERT INTO left_items(directive_id,kind,text,recorded_by,recorded_at,state) VALUES(?,?,?,?,?,?)`, directive, "item", text, by, at.Format(time.RFC3339Nano), "open")
}

func search(t testing.TB, sv *Service, q Query, see See) wire.SearchResult {
	t.Helper()
	res, err := sv.Search(context.Background(), q, see, "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func kinds(res wire.SearchResult) string {
	var k []string
	for _, h := range res.Hits {
		k = append(k, h.Kind+":"+h.ID)
	}
	return strings.Join(k, " ")
}

func TestSearchFindsEveryKindNewestFirst(t *testing.T) {
	db := open(t)
	m := addMail(t, db, "team/boss", "team/worker-1", "배포 순서", "첫 줄\n캐시 TTL 은 300초로 정했다\n끝", t0, 0)
	addMail(t, db, "team/boss", "team/worker", "다른 일", "상관없음", t0.Add(time.Minute), 0)
	r := addReport(t, db, "team/boss", "주간 보고", "TTL 을 바꿨다", t0.Add(2*time.Minute), m)
	q := addQuestion(t, db, "team/worker-1", "캐시를 어떻게 둘까?", []string{"ttl 300", "ttl 60"}, "ttl 300", t0.Add(3*time.Minute))
	l := addLeft(t, db, m, "team/worker-1", "TTL 문서 고치기", t0.Add(4*time.Minute))
	sv := New(db)

	res := search(t, sv, Query{Pattern: "ttl"}, nil)
	want := fmt.Sprintf("left:%d question:%s report:%d mail:%d", l, q, r, m)
	if got := kinds(res); got != want {
		t.Fatalf("hits = %q, want %q", got, want)
	}
	if res.Scope != "all" || res.Cut || res.Next != "" {
		t.Fatalf("result = %+v", res)
	}
	mail := res.Hits[3]
	if mail.Line != "캐시 TTL 은 300초로 정했다" || mail.From != "team/boss" || mail.To != "team/worker-1" || mail.Open != fmt.Sprintf("read %d", m) || mail.Thread != m {
		t.Fatalf("mail hit = %+v", mail)
	}
	if h := res.Hits[2]; h.Open != fmt.Sprintf("report show R%d", r) || h.From != "team/boss" {
		t.Fatalf("report hit = %+v", h)
	}
	if h := res.Hits[1]; h.Open != "question show "+q || h.Line != "ttl 300" || h.Title != "캐시를 어떻게 둘까?" {
		t.Fatalf("question hit = %+v", h)
	}
	if h := res.Hits[0]; h.Open != fmt.Sprintf("read %d", m) || h.Directive != m || h.From != "team/worker-1" || h.To != "team/boss" {
		t.Fatalf("left hit = %+v", h)
	}

	// Hangul, and only the kinds asked for.
	res = search(t, sv, Query{Pattern: "캐시", Kinds: []string{wire.SearchMail, wire.SearchQuestion}}, nil)
	if got := kinds(res); got != fmt.Sprintf("question:%s mail:%d", q, m) {
		t.Fatalf("hits = %q", got)
	}
	// Text in no searched field is not found.
	res = search(t, sv, Query{Pattern: "선택", Kinds: []string{wire.SearchQuestion}}, nil)
	if len(res.Hits) != 0 {
		t.Fatalf("hits = %q", kinds(res))
	}
}

func TestSearchLiteralRegexAndCase(t *testing.T) {
	db := open(t)
	a := addMail(t, db, "team/boss", "team/worker", "a", "value a.c here", t0, 0)
	b := addMail(t, db, "team/boss", "team/worker", "b", "value ABC here", t0.Add(time.Second), 0)
	sv := New(db)
	for _, c := range []struct {
		q    Query
		want string
	}{
		{Query{Pattern: "a.c"}, fmt.Sprintf("mail:%d", a)},
		{Query{Pattern: "a.c", Regex: true}, fmt.Sprintf("mail:%d mail:%d", b, a)},
		{Query{Pattern: "a.c", Regex: true, Case: true}, fmt.Sprintf("mail:%d", a)},
		{Query{Pattern: "abc"}, fmt.Sprintf("mail:%d", b)},
		{Query{Pattern: "abc", Case: true}, ""},
		{Query{Pattern: `^value A\w+`, Regex: true, Case: true}, fmt.Sprintf("mail:%d", b)},
	} {
		if got := kinds(search(t, sv, c.q, nil)); got != c.want {
			t.Errorf("%+v: hits = %q, want %q", c.q, got, c.want)
		}
	}
	for _, q := range []Query{{Pattern: "(", Regex: true}, {Pattern: "  "}, {Pattern: "x", Kinds: []string{"chat"}}, {Pattern: "x", After: "nope"}} {
		if _, err := sv.Search(context.Background(), q, nil, ""); !errors.Is(err, ErrBadQuery) {
			t.Errorf("%+v: err = %v, want ErrBadQuery", q, err)
		}
	}
	if _, err := sv.Search(context.Background(), Query{Pattern: "(", Regex: true}, nil, ""); err == nil || strings.Contains(err.Error(), "(?i)") || !strings.Contains(err.Error(), "missing closing )") {
		t.Errorf("bad regexp error = %v", err)
	}
}

// A group role sees only its group: other services' rows are neither in the
// hits nor in the count, whatever kind they are.
func TestSearchScopeHidesOtherServices(t *testing.T) {
	db := open(t)
	mine := addMail(t, db, "team/boss", "team/worker-2", "secret plan", "x", t0, 0)
	other := addMail(t, db, "crew/boss", "crew/worker", "secret plan", "x", t0, 0)
	addReport(t, db, "crew/boss", "secret report", "x", t0)
	addQuestion(t, db, "crew/worker", "secret?", nil, "", t0)
	addLeft(t, db, other, "crew/worker", "secret item", t0)
	toMine := addMail(t, db, "crew/worker", "label:team/worker/481", "secret ask", "x", t0.Add(time.Second), 0)
	see := func(role string) bool { return strings.HasPrefix(BaseOf(role), "team/") }
	res := search(t, New(db), Query{Pattern: "secret"}, see)
	if got := kinds(res); got != fmt.Sprintf("mail:%d mail:%d", toMine, mine) {
		t.Fatalf("hits = %q", got)
	}
	if res := search(t, New(db), Query{Pattern: "secret"}, nil); len(res.Hits) != 6 {
		t.Fatalf("unscoped hits = %q", kinds(res))
	}
}

func TestSearchPagesWithCursor(t *testing.T) {
	db := open(t)
	for i := range 120 {
		addMail(t, db, "team/boss", "team/worker", "n", fmt.Sprintf("needle %d", i), t0.Add(time.Duration(i)*time.Second), 0)
	}
	for i := range 3 { // same time as mail: kind order breaks the tie
		addLeft(t, db, 1, "team/worker", fmt.Sprintf("needle left %d", i), t0.Add(time.Duration(10*i)*time.Second))
	}
	sv := New(db)
	seen := map[string]bool{}
	var pages []int
	var prev time.Time
	after := ""
	for {
		res := search(t, sv, Query{Pattern: "needle", After: after}, nil)
		pages = append(pages, len(res.Hits))
		for _, h := range res.Hits {
			if seen[h.Kind+h.ID] {
				t.Fatalf("%s %s twice", h.Kind, h.ID)
			}
			seen[h.Kind+h.ID] = true
			if !prev.IsZero() && h.At.After(prev) {
				t.Fatalf("%s %s out of order", h.Kind, h.ID)
			}
			prev = h.At
		}
		if res.Next == "" {
			break
		}
		after = res.Next
	}
	if fmt.Sprint(pages) != "[50 50 23]" || len(seen) != 123 {
		t.Fatalf("pages = %v, seen %d", pages, len(seen))
	}
	if res := search(t, sv, Query{Pattern: "needle", Limit: 5}, nil); len(res.Hits) != 5 || res.Next == "" {
		t.Fatalf("limit 5: %d hits, next %q", len(res.Hits), res.Next)
	}
}

func TestSearchFilters(t *testing.T) {
	db := open(t)
	root := addMail(t, db, "team/boss", "team/worker-3", "go", "word", t0, 0)
	reply := addMail(t, db, "team/worker-3", "team/boss", "re: go", "word", t0.Add(time.Hour), root)
	late := addMail(t, db, "team/boss", "team/other", "x", "word", t0.Add(48*time.Hour), 0)
	rep := addReport(t, db, "team/boss", "r", "word", t0.Add(2*time.Hour), root)
	addReport(t, db, "team/boss", "r2", "word", t0.Add(2*time.Hour))
	left := addLeft(t, db, reply, "team/worker-3", "word", t0.Add(3*time.Hour))
	addQuestion(t, db, "team/worker", "word?", nil, "", t0.Add(time.Hour))
	sv := New(db)
	day := t0.Truncate(24 * time.Hour)
	for _, c := range []struct {
		name string
		q    Query
		want string
	}{
		{"thread", Query{Thread: root}, fmt.Sprintf("left:%d report:%d mail:%d mail:%d", left, rep, reply, root)},
		{"role by base", Query{Role: "team/worker", Kinds: []string{wire.SearchMail}}, fmt.Sprintf("mail:%d mail:%d", reply, root)},
		{"role exact slot", Query{Role: "team/other", Kinds: []string{wire.SearchMail}}, fmt.Sprintf("mail:%d", late)},
		{"since until", Query{Since: t0.Add(30 * time.Minute), Until: t0.Add(90 * time.Minute), Kinds: []string{wire.SearchMail}}, fmt.Sprintf("mail:%d", reply)},
		{"day", Query{Since: day.Add(24 * time.Hour), Kinds: []string{wire.SearchMail}}, fmt.Sprintf("mail:%d", late)},
	} {
		c.q.Pattern = "word"
		if got := kinds(search(t, sv, c.q, nil)); got != c.want {
			t.Errorf("%s: hits = %q, want %q", c.name, got, c.want)
		}
	}
}

// The time limit stops the scan and returns what was found, marked cut,
// without a cursor.
func TestSearchTimeLimitCuts(t *testing.T) {
	db := open(t)
	for i := range 300 {
		addMail(t, db, "team/boss", "team/worker", "n", "needle", t0.Add(time.Duration(i)*time.Second), 0)
	}
	now := t0
	sv := New(db)
	sv.Budget = time.Second
	sv.clock = func() time.Time { now = now.Add(300 * time.Millisecond); return now }
	res := search(t, sv, Query{Pattern: "needle", Limit: 200}, nil)
	if !res.Cut || res.Next != "" || len(res.Hits) == 0 || len(res.Hits) >= 200 {
		t.Fatalf("cut %v next %q hits %d", res.Cut, res.Next, len(res.Hits))
	}
}

func TestLineCutsAroundTheMatch(t *testing.T) {
	long := strings.Repeat("가", 300) + "MATCH" + strings.Repeat("나", 300)
	i := strings.Index(long, "MATCH")
	got := Line("before\n"+long+"\nafter", i+7, i+7+5)
	if n := utf8.RuneCountInString(got); n > LineRunes+2 || !strings.Contains(got, "MATCH") || !strings.HasPrefix(got, "…가") || !strings.HasSuffix(got, "나…") {
		t.Fatalf("line (%d runes) = %q", n, got)
	}
	if got := Line("x\tab\r", 2, 3); got != "x ab " {
		t.Fatalf("line = %q", got)
	}
	if got := Line("ab", 0, 1); got != "ab" {
		t.Fatalf("line = %q", got)
	}
}

func TestParseQuery(t *testing.T) {
	q, err := ParseQuery(map[string][]string{"q": {"x"}, "regex": {"1"}, "kind": {"mail", "left"}, "thread": {"7"}, "since": {"2026-11-01"}, "until": {"2026-11-02"}, "limit": {"10"}})
	if err != nil || !q.Regex || q.Case || len(q.Kinds) != 2 || q.Thread != 7 || q.Limit != 10 ||
		!q.Since.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !q.Until.Equal(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("query = %+v, %v", q, err)
	}
	for _, bad := range []map[string][]string{{"thread": {"x"}}, {"limit": {"0"}}, {"since": {"yesterday"}}} {
		if _, err := ParseQuery(bad); !errors.Is(err, ErrBadQuery) {
			t.Errorf("%v: err = %v", bad, err)
		}
	}
}
