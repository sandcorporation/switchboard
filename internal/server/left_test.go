package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// callNew is callAs from a CLI that says what is left (#470): it carries
// config.LeftHeaderName.
func callNew(s *Server, method, path, role string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	r.Header.Set("Authorization", "Bearer "+tokenFor(role))
	r.Header.Set("X-Switchboard-Role", role)
	r.Header.Set(config.HeaderPrefix+config.LeftHeaderName, "1")
	w := httptest.NewRecorder()
	s.api(w, r)
	return w
}

// leftRows is the ledger as "directive kind state recorded_by text" lines,
// in ID order.
func leftRows(t *testing.T, s *Server) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT directive_id,kind,state,recorded_by,text FROM left_items ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d int64
		var kind, state, by, text string
		if err := rows.Scan(&d, &kind, &state, &by, &text); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.TrimSpace(fmt.Sprintf("%d %s %s %s %s", d, kind, state, by, text)))
	}
	return out
}

func wantRows(t *testing.T, s *Server, want ...string) {
	t.Helper()
	got := leftRows(t, s)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ledger:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func directiveTo(t *testing.T, s *Server, clock *fakeClock, subject string) wire.Message {
	t.Helper()
	return apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": subject})
}

// The three answers that end a directive (#470 T1): reply --complete, the
// recipient's complete and the sender's close. A new CLI must say what is
// left; an older one is taken and leaves an open unrecorded row.
func TestLeftEndingAnswers(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	d1 := directiveTo(t, s, clock, "reply complete")
	d2 := directiveTo(t, s, clock, "complete")
	d3 := directiveTo(t, s, clock, "close")
	d4 := directiveTo(t, s, clock, "old cli")
	d5 := directiveTo(t, s, clock, "send complete")
	reply := func(id int64, body map[string]any) *httptest.ResponseRecorder {
		return callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", id), "team/worker", body)
	}

	// reply --complete: neither is refused with the one-line how-to, both too.
	if w := reply(d1.ID, map[string]any{"body": "done", "done": true}); w.Code != 400 || !strings.Contains(w.Body.String(), "--left-none") {
		t.Fatalf("reply --complete without left: %d %s", w.Code, w.Body)
	}
	if w := reply(d1.ID, map[string]any{"body": "done", "done": true, "left": []string{"x"}, "left_none": true}); w.Code != 400 {
		t.Fatalf("both: %d %s", w.Code, w.Body)
	}
	wantRows(t, s)
	if done, _ := s.mailCompleted(d1.ID); done {
		t.Fatal("refused reply completed the directive")
	}
	// An acknowledgement and a progress reply end nothing.
	if w := reply(d1.ID, map[string]any{"body": "on it"}); w.Code != 201 {
		t.Fatalf("progress reply: %d %s", w.Code, w.Body)
	}
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/ack", d1.ID), "team/worker", nil); w.Code != 200 {
		t.Fatalf("ack: %d %s", w.Code, w.Body)
	}
	if w := reply(d1.ID, map[string]any{"body": "done", "done": true, "left": []string{"delete files", " rename "}}); w.Code != 201 {
		t.Fatalf("reply --complete --left: %d %s", w.Code, w.Body)
	}

	// complete: refused without, taken with --left-none; once ended, a
	// complete again asks nothing and stores nothing.
	complete := func(id int64, body any) *httptest.ResponseRecorder {
		return callNew(s, "POST", fmt.Sprintf("/api/messages/%d/complete", id), "team/worker", body)
	}
	if w := complete(d2.ID, nil); w.Code != 400 {
		t.Fatalf("complete without left: %d %s", w.Code, w.Body)
	}
	if w := complete(d2.ID, wire.LeftNote{LeftNone: true}); w.Code != 200 {
		t.Fatalf("complete --left-none: %d %s", w.Code, w.Body)
	}
	if w := complete(d2.ID, nil); w.Code != 200 {
		t.Fatalf("complete again: %d %s", w.Code, w.Body)
	}

	// close: the sender's close of an open directive says what is left.
	closeNew := func(id int64, body any) *httptest.ResponseRecorder {
		return callNew(s, "POST", fmt.Sprintf("/api/messages/%d/close", id), "hq/boss", body)
	}
	if w := closeNew(d3.ID, nil); w.Code != 400 {
		t.Fatalf("close without left: %d %s", w.Code, w.Body)
	}
	if w := closeNew(d3.ID, wire.LeftNote{Left: []string{"permissions per login"}}); w.Code != 200 {
		t.Fatalf("close --left: %d %s", w.Code, w.Body)
	}
	if w := closeNew(d3.ID, nil); w.Code != 200 {
		t.Fatalf("close again: %d %s", w.Code, w.Body)
	}

	// An older CLI (no header) is never refused: its ending answer leaves
	// one open unrecorded row, once per directive.
	for i := 0; i < 2; i++ {
		if w := callAs(s, "POST", fmt.Sprintf("/api/messages/%d/reply", d4.ID), workerToken, "team/worker", map[string]any{"body": "done", "done": true}); w.Code != 201 {
			t.Fatalf("old reply --complete: %d %s", w.Code, w.Body)
		}
	}

	// send --complete of a directive ends it too.
	send := map[string]any{"recipient": "hq/boss", "subject": "report", "complete": []int64{d5.ID}}
	if w := callNew(s, "POST", "/api/messages", "team/worker", send); w.Code != 400 {
		t.Fatalf("send --complete without left: %d %s", w.Code, w.Body)
	}
	send["left_none"] = true
	if w := callNew(s, "POST", "/api/messages", "team/worker", send); w.Code != 201 {
		t.Fatalf("send --complete --left-none: %d %s", w.Code, w.Body)
	}

	// The complete of mail that is not a directive asks nothing.
	plain := apiMail(t, s, clock, "team/worker", fmt.Sprintf("/api/messages/%d/reply", d2.ID), map[string]any{"body": "fyi"})
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/complete", plain.ID), "hq/boss", nil); w.Code != 200 {
		t.Fatalf("complete of a plain reply: %d %s", w.Code, w.Body)
	}

	wantRows(t, s,
		fmt.Sprintf("%d item open team/worker delete files", d1.ID),
		fmt.Sprintf("%d item open team/worker rename", d1.ID),
		fmt.Sprintf("%d none none team/worker", d2.ID),
		fmt.Sprintf("%d item open hq/boss permissions per login", d3.ID),
		fmt.Sprintf("%d unrecorded open team/worker", d4.ID),
		fmt.Sprintf("%d none none team/worker", d5.ID),
	)

	// left add: only to a directive the caller ended.
	add := func(role string, d int64) *httptest.ResponseRecorder {
		return callNew(s, "POST", "/api/left", role, wire.LeftAdd{Directive: d, Left: []string{"found later"}})
	}
	open := directiveTo(t, s, clock, "still open")
	if w := add("team/worker", open.ID); w.Code != 409 {
		t.Fatalf("left add to an open directive: %d %s", w.Code, w.Body)
	}
	if err := seedOther(s); err != nil {
		t.Fatal(err)
	}
	if w := add("partner-a/coordinator", d2.ID); w.Code != 403 {
		t.Fatalf("left add by a stranger: %d %s", w.Code, w.Body)
	}
	w := add("team/worker", d2.ID)
	var list wire.LeftList
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 1 || list.Items[0].Text != "found later" || list.Items[0].DirectiveSubject != "complete" {
		t.Fatalf("left add: %d %s", w.Code, w.Body)
	}
}

func seedOther(s *Server) error {
	_, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('partner-a/coordinator','secret-partner-a/coordinator','now')`)
	return err
}

// mailCompleted reports whether the recipient marked id complete.
func (s *Server) mailCompleted(id int64) (bool, error) {
	m, err := s.mail().View(id)
	return m.Status == "complete", err
}

// Ending an item (#470 T2): the directive's sender raises it as an issue or
// passes it on in a directive it sent; only a signed-in admin drops it,
// with a reason. An ended item answers 409 and leaves 진행.
func TestLeftEnd(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	s.admin.ProxyToken = "proxy-secret"
	if err := seedOther(s); err != nil {
		t.Fatal(err)
	}
	d := directiveTo(t, s, clock, "work")
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", d.ID), "team/worker", map[string]any{"body": "done", "done": true, "left": []string{"a", "b", "c", "d"}}); w.Code != 201 {
		t.Fatalf("reply: %d %s", w.Code, w.Body)
	}
	ids := map[string]int64{}
	for _, it := range openLeft(t, s) {
		ids[it.Text] = it.ID
	}
	post := func(role, path string, body any) *httptest.ResponseRecorder {
		return callNew(s, "POST", path, role, body)
	}
	issue := fmt.Sprintf("/api/left/%d/issue", ids["a"])
	if w := post("team/worker", issue, wire.LeftEnd{URL: "https://example.com/i/1"}); w.Code != 403 {
		t.Fatalf("issue by the recipient: %d %s", w.Code, w.Body)
	}
	if w := post("hq/boss", issue, wire.LeftEnd{URL: "not a url"}); w.Code != 400 {
		t.Fatalf("issue without a URL: %d %s", w.Code, w.Body)
	}
	w := post("hq/boss", issue, wire.LeftEnd{URL: "https://example.com/i/1"})
	var it wire.LeftItem
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &it) != nil || it.State != wire.LeftIssue || it.Value != "https://example.com/i/1" || it.EndedBy != "hq/boss" || it.EndedAt == nil {
		t.Fatalf("issue: %d %s", w.Code, w.Body)
	}
	if w := post("hq/boss", issue, wire.LeftEnd{URL: "https://example.com/i/2"}); w.Code != 409 {
		t.Fatalf("issue again: %d %s", w.Code, w.Body)
	}

	pass := fmt.Sprintf("/api/left/%d/pass", ids["b"])
	other := apiMail(t, s, clock, "partner-a/coordinator", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "not boss's"})
	if w := post("hq/boss", pass, wire.LeftEnd{Directive: other.ID}); w.Code != 400 {
		t.Fatalf("pass to another sender's directive: %d %s", w.Code, w.Body)
	}
	if w := post("hq/boss", pass, wire.LeftEnd{Directive: d.ID}); w.Code != 400 {
		t.Fatalf("pass to its own directive: %d %s", w.Code, w.Body)
	}
	next := directiveTo(t, s, clock, "take over b")
	if w := post("hq/boss", pass, wire.LeftEnd{Directive: next.ID}); w.Code != 200 || !strings.Contains(w.Body.String(), fmt.Sprintf(`"value":"%d"`, next.ID)) {
		t.Fatalf("pass: %d %s", w.Code, w.Body)
	}

	// Drop: a role token is refused, through the boss routes too; the
	// admin needs the write guard and a reason.
	drop := fmt.Sprintf("/api/admin/left/%d/drop", ids["c"])
	if w := post("hq/boss", "/api/boss/"+strings.TrimPrefix(drop, "/api/admin/"), wire.LeftEnd{Reason: "x"}); w.Code < 400 {
		t.Fatalf("drop by a boss token: %d %s", w.Code, w.Body)
	}
	if w := post("hq/boss", drop, wire.LeftEnd{Reason: "x"}); w.Code != 401 && w.Code != 403 {
		t.Fatalf("drop by a role token at the admin path: %d %s", w.Code, w.Body)
	}
	if w := adminRequest(s, "POST", drop, `{"reason":"x"}`, nil); w.Code != 403 {
		t.Fatalf("drop without the write guard: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "POST", drop, `{"reason":" "}`); w.Code != 400 {
		t.Fatalf("drop without a reason: %d %s", w.Code, w.Body)
	}
	w = adminWrite(s, "POST", drop, `{"reason":"done elsewhere"}`)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &it) != nil || it.State != wire.LeftDropped || it.Value != "done elsewhere" || !strings.HasPrefix(it.EndedBy, "admin:") {
		t.Fatalf("drop: %d %s", w.Code, w.Body)
	}
	// The dashboard's [이슈] for the admin.
	if w := adminWrite(s, "POST", fmt.Sprintf("/api/admin/left/%d/issue", ids["d"]), `{"url":"https://example.com/i/3"}`); w.Code != 200 {
		t.Fatalf("admin issue: %d %s", w.Code, w.Body)
	}
	if got := openLeft(t, s); len(got) != 0 {
		t.Fatalf("open after ending all: %+v", got)
	}
}

func openLeft(t *testing.T, s *Server) []wire.LeftItem {
	t.Helper()
	items, err := mail.OpenLeft(s.db)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// The CLI says what is left (#470): an ending answer without --left or
// --left-none is refused with the one-line how-to, both together is a usage
// error, --left repeats, and left add, issue and pass reach the ledger.
func TestCLILeft(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "a"}, "a")
	h.configure(nil, "b")
	h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", "build", "--body", "x") // #1
	h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", "next", "--body", "y")  // #2

	if r := h.run(nil, "reply", "1", "--complete", "--body", "built"); r.code == 0 || !strings.Contains(r.stderr, `--left "ITEM" (repeatable) or --left-none`) {
		t.Fatalf("reply --complete without left: %+v", r)
	}
	if r := h.run(nil, "reply", "1", "--complete", "--left", "x", "--left-none", "--body", "built"); r.code == 0 || !strings.Contains(r.stderr, "not both") {
		t.Fatalf("both: %+v", r)
	}
	h.mustRun(nil, "reply", "1", "--complete", "--left", "delete files", "--left", "rename", "--body", "built") // #3
	if out := h.mustRun(nil, "left", "add", "1", "upload"); !strings.Contains(out, "on directive #1: upload") {
		t.Fatalf("left add: %q", out)
	}
	items := openLeft(t, h.s)
	if len(items) != 3 || items[0].Text != "delete files" || items[2].Text != "upload" {
		t.Fatalf("open %+v", items)
	}
	id := func(i int) string { return fmt.Sprint(items[i].ID) }
	if r := h.run(nil, "left", "issue", id(0), "https://example.com/i/1"); r.code == 0 {
		t.Fatalf("issue by the recipient: %+v", r)
	}
	if out := h.mustRun(nil, "--profile", "a", "left", "issue", id(0), "https://example.com/i/1"); !strings.Contains(out, "issue: https://example.com/i/1") {
		t.Fatalf("left issue: %q", out)
	}
	if out := h.mustRun(nil, "--profile", "a", "left", "pass", id(1), "--directive", "2"); !strings.Contains(out, "passed: 2") {
		t.Fatalf("left pass: %q", out)
	}
	if r := h.run(nil, "--profile", "a", "left", "pass", id(2)); r.code == 0 {
		t.Fatalf("left pass without --directive: %+v", r)
	}
	if got := openLeft(t, h.s); len(got) != 1 || got[0].Text != "upload" {
		t.Fatalf("open after issue and pass: %+v", got)
	}
}

// 진행 (#470 T3): GET /api/admin/progress lists the open items, oldest
// first, with their directive, and counts them on the service of the role
// that recorded them; an ended item leaves both.
func TestProgressLeft(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	s.admin.ProxyToken = "proxy-secret"
	d := directiveTo(t, s, clock, "work")
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", d.ID), "team/worker", map[string]any{"body": "done", "done": true, "left": []string{"a", "b"}}); w.Code != 201 {
		t.Fatalf("reply: %d %s", w.Code, w.Body)
	}
	old := directiveTo(t, s, clock, "old cli")
	if w := callAs(s, "POST", fmt.Sprintf("/api/messages/%d/complete", old.ID), workerToken, "team/worker", nil); w.Code != 200 {
		t.Fatalf("old complete: %d %s", w.Code, w.Body)
	}
	progress := func() wire.ProgressReport {
		w := adminRequest(s, "GET", "/api/admin/progress", "", nil)
		var rp wire.ProgressReport
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rp) != nil {
			t.Fatalf("progress: %d %s", w.Code, w.Body)
		}
		return rp
	}
	count := func(rp wire.ProgressReport, svc string) int {
		for _, p := range rp.Services {
			if p.Service == svc {
				return p.Left
			}
		}
		return -1
	}
	rp := progress()
	if len(rp.Left) != 3 || rp.Left[0].Text != "a" || rp.Left[0].Directive != d.ID || rp.Left[0].DirectiveSubject != "work" || rp.Left[0].DirectiveFrom != "hq/boss" || rp.Left[2].Kind != wire.LeftKindUnrecorded || count(rp, "team") != 3 || count(rp, "hq") != 0 {
		t.Fatalf("progress left %+v, team %d", rp.Left, count(rp, "team"))
	}
	if w := adminWrite(s, "POST", fmt.Sprintf("/api/admin/left/%d/drop", rp.Left[0].ID), `{"reason":"not needed"}`); w.Code != 200 {
		t.Fatalf("drop: %d %s", w.Code, w.Body)
	}
	if rp = progress(); len(rp.Left) != 2 || count(rp, "team") != 2 {
		t.Fatalf("after drop %+v", rp.Left)
	}
}

// complete <answer> --left by the directive's sender records into the
// directive's ledger (#483): it ends a directive still open, as close
// <answer> does, and adds to one that has ended. Elsewhere --left is
// refused with the command to give instead.
func TestLeftCompleteAnswer(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	reply := func(d int64, body map[string]any) wire.Message {
		t.Helper()
		return apiMail(t, s, clock, "team/worker", fmt.Sprintf("/api/messages/%d/reply", d), body)
	}
	complete := func(role string, id int64, body any) *httptest.ResponseRecorder {
		return callNew(s, "POST", fmt.Sprintf("/api/messages/%d/complete", id), role, body)
	}
	noReply := func(id int64) bool {
		t.Helper()
		var v bool
		if err := s.db.QueryRow(`SELECT no_reply FROM messages WHERE id=?`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	// The answer ended the directive (reply --complete): the items join
	// its ledger.
	d1 := directiveTo(t, s, clock, "ended by the answer")
	a1 := reply(d1.ID, map[string]any{"body": "done", "done": true, "left_none": true})
	if w := complete("hq/boss", a1.ID, wire.LeftNote{Left: []string{"a", "b"}}); w.Code != 200 {
		t.Fatalf("complete <answer> --left: %d %s", w.Code, w.Body)
	}
	if done, _ := s.mailCompleted(a1.ID); !done {
		t.Fatal("the answer is not complete")
	}

	// The answer left the directive open (a progress reply): the sender's
	// complete ends it, as close does.
	d2 := directiveTo(t, s, clock, "open after the answer")
	a2 := reply(d2.ID, map[string]any{"body": "half"})
	if w := complete("hq/boss", a2.ID, wire.LeftNote{Left: []string{"c"}}); w.Code != 200 {
		t.Fatalf("complete <answer> --left, open directive: %d %s", w.Code, w.Body)
	}
	if !noReply(d2.ID) {
		t.Fatal("the directive did not end")
	}
	d3 := directiveTo(t, s, clock, "none")
	a3 := reply(d3.ID, map[string]any{"body": "half"})
	if w := complete("hq/boss", a3.ID, wire.LeftNote{LeftNone: true}); w.Code != 200 {
		t.Fatalf("complete <answer> --left-none: %d %s", w.Code, w.Body)
	}
	if !noReply(d3.ID) {
		t.Fatal("--left-none did not end the directive")
	}
	// Already ended: --left-none adds nothing.
	if w := complete("hq/boss", a1.ID, wire.LeftNote{LeftNone: true}); w.Code != 200 {
		t.Fatalf("complete <answer> --left-none again: %d %s", w.Code, w.Body)
	}

	wantRows(t, s,
		fmt.Sprintf("%d none none team/worker", d1.ID),
		fmt.Sprintf("%d item open hq/boss a", d1.ID),
		fmt.Sprintf("%d item open hq/boss b", d1.ID),
		fmt.Sprintf("%d item open hq/boss c", d2.ID),
		fmt.Sprintf("%d none none hq/boss", d3.ID),
	)

	// Refused, with the command to give instead.
	refused := func(role string, id int64, want string) {
		t.Helper()
		w := complete(role, id, wire.LeftNote{Left: []string{"x"}})
		if w.Code != 400 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("complete #%d --left by %s: %d %s, want %q", id, role, w.Code, w.Body, want)
		}
	}
	// The worker's own directive, ended: left add.
	refused("team/worker", d1.ID, fmt.Sprintf(`left add %d "ITEM"`, d1.ID))
	// A reply the worker got in the thread of an ended directive.
	r1 := apiMail(t, s, clock, "hq/boss", fmt.Sprintf("/api/messages/%d/reply", a1.ID), map[string]any{"body": "thanks"})
	refused("team/worker", r1.ID, fmt.Sprintf(`left add %d "ITEM"`, d1.ID))
	// ... of a directive still open: end it with what is left.
	d4 := directiveTo(t, s, clock, "still open")
	a4 := reply(d4.ID, map[string]any{"body": "question"})
	r4 := apiMail(t, s, clock, "hq/boss", fmt.Sprintf("/api/messages/%d/reply", a4.ID), map[string]any{"body": "answer"})
	refused("team/worker", r4.ID, fmt.Sprintf(`complete %d --left "ITEM"`, d4.ID))
	// The sender's close of an ended directive: left add with its number.
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/close", d1.ID), "hq/boss", wire.LeftNote{Left: []string{"x"}}); w.Code != 400 || !strings.Contains(w.Body.String(), fmt.Sprintf(`left add %d "ITEM"`, d1.ID)) {
		t.Fatalf("close of an ended directive --left: %d %s", w.Code, w.Body)
	}
	if n := len(leftRows(t, s)); n != 5 {
		t.Fatalf("refusals stored rows: %d", n)
	}
}
