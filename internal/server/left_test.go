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
// error, --left repeats, and left add reaches the ledger.
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
}
