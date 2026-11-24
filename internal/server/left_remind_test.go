package server

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// leftReminders is the [남긴 것] mail stored so far, in send order, as
// "<recipient>\n<body>".
func leftReminders(t *testing.T, s *Server) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT recipient,subject,body FROM messages WHERE sender=? AND idem LIKE 'left-remind:%' ORDER BY id`, identity.SystemSender)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var to, subject, body string
		if err = rows.Scan(&to, &subject, &body); err != nil {
			t.Fatal(err)
		}
		out = append(out, to+" "+subject+"\n"+body)
	}
	return out
}

// leftWithItems sends a directive hq/boss -> team/worker and ends it with
// items, returning the directive and the items' IDs by text.
func leftWithItems(t *testing.T, s *Server, clock *fakeClock, subject string, items ...string) (wire.Message, map[string]int64) {
	t.Helper()
	d := directiveTo(t, s, clock, subject)
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", d.ID), "team/worker", map[string]any{"body": "done", "done": true, "left": items}); w.Code != 201 {
		t.Fatalf("reply: %d %s", w.Code, w.Body)
	}
	ids := map[string]int64{}
	for _, it := range openLeft(t, s) {
		ids[it.Text] = it.ID
	}
	return d, ids
}

func leftSweep(t *testing.T, s *Server, clock *fakeClock, d time.Duration) int {
	t.Helper()
	clock.add(d)
	return sweepOnce(t, s).LeftRemind
}

// #553: the reminder of open left items. The default 24h counts from the
// server's first use, so the items left before it wait a full day; the
// directive's sender gets one mail for all its due items; an item is not
// reminded of twice within the time; done and dropped items are not
// reminded of again; a new time counts from when it is set; empty is off.
func TestLeftRemind(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	s.admin.ProxyToken = "proxy-secret"
	d1, ids := leftWithItems(t, s, clock, "build", "delete files", "rename")
	d2, more := leftWithItems(t, s, clock, "deploy", "write docs")
	clock.add(48 * time.Hour) // the items are old before the server first looks

	if n := leftSweep(t, s, clock, 0); n != 0 {
		t.Fatalf("first use sent %d", n)
	}
	if n := leftSweep(t, s, clock, 23*time.Hour); n != 0 {
		t.Fatalf("before the time sent %d", n)
	}
	if n := leftSweep(t, s, clock, time.Hour); n != 1 {
		t.Fatalf("after the time sent %d, want one mail", n)
	}
	got := leftReminders(t, s)
	want := []string{
		fmt.Sprintf("#%d delete files — 지시 #%d build", ids["delete files"], d1.ID),
		fmt.Sprintf("#%d rename — 지시 #%d build", ids["rename"], d1.ID),
		fmt.Sprintf("#%d write docs — 지시 #%d deploy", more["write docs"], d2.ID),
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "hq/boss [남긴 것] 3건") {
		t.Fatalf("reminders %q", got)
	}
	for _, line := range want {
		if !strings.Contains(got[0], line+"\n") {
			t.Fatalf("reminder lacks %q:\n%s", line, got[0])
		}
	}
	if n := leftSweep(t, s, clock, 0); n != 0 {
		t.Fatalf("sweep again sent %d", n)
	}
	if n := leftSweep(t, s, clock, 23*time.Hour); n != 0 {
		t.Fatalf("within the time sent %d", n)
	}

	// Ends: the directive's sender only; drop needs a reason.
	done := fmt.Sprintf("/api/left/%d/done", ids["delete files"])
	if w := callNew(s, "POST", done, "team/worker", wire.LeftEnd{}); w.Code != 403 {
		t.Fatalf("done by the recipient: %d %s", w.Code, w.Body)
	}
	var it wire.LeftItem
	if w := callNew(s, "POST", done, "hq/boss", wire.LeftEnd{}); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &it) != nil || it.State != wire.LeftDone || it.EndedBy != "hq/boss" {
		t.Fatalf("done: %d %s", w.Code, w.Body)
	}
	if w := callNew(s, "POST", done, "hq/boss", wire.LeftEnd{}); w.Code != 409 {
		t.Fatalf("done again: %d %s", w.Code, w.Body)
	}
	drop := fmt.Sprintf("/api/left/%d/drop", ids["rename"])
	if w := callNew(s, "POST", drop, "hq/boss", wire.LeftEnd{Reason: " "}); w.Code != 400 {
		t.Fatalf("drop without a reason: %d %s", w.Code, w.Body)
	}
	if w := callNew(s, "POST", drop, "hq/boss", wire.LeftEnd{Reason: "not needed"}); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &it) != nil || it.State != wire.LeftDropped || it.Value != "not needed" {
		t.Fatalf("drop: %d %s", w.Code, w.Body)
	}

	if n := leftSweep(t, s, clock, time.Hour); n != 1 {
		t.Fatalf("a day after the first sent %d", n)
	}
	got = leftReminders(t, s)
	if len(got) != 2 || !strings.HasPrefix(got[1], "hq/boss [남긴 것] 1건") || strings.Contains(got[1], "delete files") || strings.Contains(got[1], "rename") || !strings.Contains(got[1], "write docs") {
		t.Fatalf("second reminder %q", got)
	}

	// A new time counts from when it is set.
	if w := adminWrite(s, "PUT", "/api/admin/settings/left-remind", `{"after":"30s"}`); w.Code != 400 {
		t.Fatalf("under a minute: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "PUT", "/api/admin/settings/left-remind", `{"after":"1h"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"after":"1h"`) {
		t.Fatalf("set 1h: %d %s", w.Code, w.Body)
	}
	if n := leftSweep(t, s, clock, 59*time.Minute); n != 0 {
		t.Fatalf("before the new time sent %d", n)
	}
	if n := leftSweep(t, s, clock, time.Minute); n != 1 {
		t.Fatalf("after the new time sent %d", n)
	}
	w := adminRequest(s, "GET", "/api/admin/settings", "", nil)
	var st adminSettings
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &st) != nil || !st.LeftRemind.On || st.LeftRemind.AfterSeconds != 3600 || st.LeftRemind.Since == nil {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "PUT", "/api/admin/settings/left-remind", `{"after":""}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"on":false`) {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	if n := leftSweep(t, s, clock, 100*time.Hour); n != 0 {
		t.Fatalf("off sent %d", n)
	}
	if w := callNew(s, "PUT", "/api/admin/settings/left-remind", "hq/boss", map[string]string{"after": "1h"}); w.Code < 400 {
		t.Fatalf("a role token sets the time: %d %s", w.Code, w.Body)
	}
}

// One reminder lists at most 20 items, then how many more; each is marked,
// so none comes again before the time.
func TestLeftRemindCapped(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	if n := leftSweep(t, s, clock, 0); n != 0 {
		t.Fatalf("first use sent %d", n)
	}
	items := make([]string, 25)
	for i := range items {
		items[i] = fmt.Sprintf("item %02d", i+1)
	}
	leftWithItems(t, s, clock, "many", items...)
	if n := leftSweep(t, s, clock, 25*time.Hour); n != 1 {
		t.Fatalf("sent %d", n)
	}
	got := leftReminders(t, s)
	if len(got) != 1 || !strings.Contains(got[0], "item 20 —") || strings.Contains(got[0], "item 21") || !strings.Contains(got[0], "외 5건\n") || !strings.HasPrefix(got[0], "hq/boss [남긴 것] 25건") {
		t.Fatalf("reminder %q", got)
	}
	if n := leftSweep(t, s, clock, time.Hour); n != 0 {
		t.Fatalf("the rest came again: %d", n)
	}
}

// left done ID and left drop ID --reason (#553) end an item for the
// directive's sender.
func TestCLILeftDoneDrop(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "a"}, "a")
	h.configure(nil, "b")
	h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", "build", "--body", "x")
	h.mustRun(nil, "reply", "1", "--complete", "--left", "docs", "--left", "tests", "--body", "built")
	items := openLeft(t, h.s)
	if len(items) != 2 {
		t.Fatalf("open %+v", items)
	}
	id := func(i int) string { return fmt.Sprint(items[i].ID) }
	if r := h.run(nil, "left", "done", id(0)); r.code == 0 {
		t.Fatalf("done by the recipient: %+v", r)
	}
	if out := h.mustRun(nil, "--profile", "a", "left", "done", id(0)); strings.TrimSpace(out) != "left #"+id(0)+" done" {
		t.Fatalf("left done: %q", out)
	}
	if r := h.run(nil, "--profile", "a", "left", "drop", id(1)); r.code == 0 {
		t.Fatalf("left drop without --reason: %+v", r)
	}
	if out := h.mustRun(nil, "--profile", "a", "left", "drop", id(1), "--reason", "not needed"); !strings.Contains(out, "dropped: not needed") {
		t.Fatalf("left drop: %q", out)
	}
	if got := openLeft(t, h.s); len(got) != 0 {
		t.Fatalf("open after done and drop: %+v", got)
	}
}

// remindOf sweeps once d later and answers the newest [남긴 것] reminder's ID
// and body.
func remindOf(t *testing.T, s *Server, clock *fakeClock, d time.Duration) (int64, string) {
	t.Helper()
	if n := leftSweep(t, s, clock, d); n != 1 {
		t.Fatalf("sent %d reminders, want 1", n)
	}
	var id int64
	var body string
	if err := s.db.QueryRow(`SELECT id,body FROM messages WHERE sender=? AND idem LIKE 'left-remind:%' ORDER BY id DESC LIMIT 1`, identity.SystemSender).Scan(&id, &body); err != nil {
		t.Fatal(err)
	}
	return id, body
}

// leftState is item id's state and value.
func leftState(t *testing.T, s *Server, id int64) (state, value string) {
	t.Helper()
	if err := s.db.QueryRow(`SELECT state,value FROM left_items WHERE id=?`, id).Scan(&state, &value); err != nil {
		t.Fatal(err)
	}
	return state, value
}

// #565: a [남긴 것] reminder completes only with a verdict for each open
// item it lists, by complete, reply --complete or send --complete. The
// verdicts end the items as said; keep leaves one open, and it comes again
// in the next reminder. An item ended since needs none.
func TestLeftRemindVerdicts(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	if n := leftSweep(t, s, clock, 0); n != 0 {
		t.Fatalf("first use sent %d", n)
	}
	_, ids := leftWithItems(t, s, clock, "build", "a", "b", "c", "d", "e")
	rid, body := remindOf(t, s, clock, 25*time.Hour)
	a, b, c, d, e := ids["a"], ids["b"], ids["c"], ids["d"], ids["e"]
	if want := fmt.Sprintf("끝내기: complete %d --verdict %d=판정 --verdict %d=판정", rid, a, b); !strings.Contains(body, want) {
		t.Fatalf("body lacks %q:\n%s", want, body)
	}
	// Ended since the reminder: needs no verdict.
	if w := callNew(s, "POST", fmt.Sprintf("/api/left/%d/done", e), "hq/boss", wire.LeftEnd{}); w.Code != 200 {
		t.Fatalf("left done: %d %s", w.Code, w.Body)
	}
	// Its event names the open items to judge, for the plugin's tail (#571).
	w := callNew(s, "GET", "/api/events/poll?timeout=0", "hq/boss", nil)
	var poll mail.PollResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &poll) != nil {
		t.Fatalf("poll: %d %s", w.Code, w.Body)
	}
	found := false
	for _, ev := range poll.Events {
		if ev.ID == rid {
			found = true
			if want := []int64{a, b, c, d}; !slices.Equal(ev.LeftRemind, want) {
				t.Fatalf("left_remind = %v, want %v", ev.LeftRemind, want)
			}
		} else if ev.LeftRemind != nil {
			t.Fatalf("mail #%d is not a reminder: left_remind %v", ev.ID, ev.LeftRemind)
		}
	}
	if !found {
		t.Fatalf("no event for reminder #%d: %s", rid, w.Body)
	}
	complete := fmt.Sprintf("/api/messages/%d/complete", rid)
	v := func(item int64, verdict, value string) wire.LeftVerdict {
		return wire.LeftVerdict{Item: item, Verdict: verdict, Value: value}
	}
	refused := []struct {
		name, path string
		body       any
		want       string
	}{
		{"complete without verdicts", complete, nil, fmt.Sprintf("without a verdict: #%d #%d #%d #%d;", a, b, c, d)},
		{"complete with some", complete, wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "done", ""), v(b, "keep", "")}}, fmt.Sprintf("without a verdict: #%d #%d;", c, d)},
		{"reply --complete without verdicts", fmt.Sprintf("/api/messages/%d/reply", rid), map[string]any{"body": "ok", "done": true}, "without a verdict"},
		{"send --complete without verdicts", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "x", "body": "x", "complete": []int64{rid}}, "without a verdict"},
		{"drop without why", complete, wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "drop", " ")}}, "why it is dropped"},
		{"issue without a URL", complete, wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "issue", "later")}}, "issue URL"},
		{"unknown verdict", complete, wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "maybe", "")}}, "the verdict is done, keep"},
		{"twice", complete, wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "done", ""), v(a, "keep", "")}}, "one verdict per item"},
		{"not listed", complete, wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "done", ""), v(b, "done", ""), v(c, "done", ""), v(d, "done", ""), v(d+100, "done", "")}}, "not in the reminder"},
	}
	refused = append(refused, struct {
		name, path string
		body       any
		want       string
	}{"report --mail", "/api/reports", map[string]any{"title": "t", "body": "b", "mail": []int64{rid}}, "items to judge"})
	for _, r := range refused {
		if w := callNew(s, "POST", r.path, "hq/boss", r.body); w.Code != 400 || !strings.Contains(w.Body.String(), r.want) {
			t.Fatalf("%s: %d %s", r.name, w.Code, w.Body)
		}
	}
	if done, _ := mail.Completed(s.db, rid); done {
		t.Fatal("a refused complete completed the reminder")
	}
	if got := openLeft(t, s); len(got) != 4 {
		t.Fatalf("a refused complete ended items: %+v", got)
	}

	all := wire.LeftNote{Verdicts: []wire.LeftVerdict{v(a, "done", ""), v(b, "keep", ""), v(c, "drop", "not needed"), v(d, "issue", "https://git.example/team/repo/issues/9")}}
	if w := callNew(s, "POST", complete, "hq/boss", all); w.Code != 200 {
		t.Fatalf("complete with every verdict: %d %s", w.Code, w.Body)
	}
	if done, _ := mail.Completed(s.db, rid); !done {
		t.Fatal("the reminder is not complete")
	}
	for _, x := range []struct {
		id           int64
		state, value string
	}{{a, wire.LeftDone, ""}, {b, wire.LeftOpen, ""}, {c, wire.LeftDropped, "not needed"}, {d, wire.LeftIssue, "https://git.example/team/repo/issues/9"}, {e, wire.LeftDone, ""}} {
		if st, val := leftState(t, s, x.id); st != x.state || val != x.value {
			t.Fatalf("item %d: %s %q, want %s %q", x.id, st, val, x.state, x.value)
		}
	}
	// Completing it again asks nothing; a verdict on other mail is refused.
	if w := callNew(s, "POST", complete, "hq/boss", nil); w.Code != 200 {
		t.Fatalf("complete again: %d %s", w.Code, w.Body)
	}
	other := directiveTo(t, s, clock, "plain")
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/complete", other.ID), "team/worker", wire.LeftNote{LeftNone: true, Verdicts: []wire.LeftVerdict{v(b, "done", "")}}); w.Code != 400 || !strings.Contains(w.Body.String(), "goes with completing a [남긴 것] reminder") {
		t.Fatalf("verdict on a directive: %d %s", w.Code, w.Body)
	}

	// keep: the next reminder lists it again; reply --complete takes verdicts.
	rid2, body2 := remindOf(t, s, clock, 24*time.Hour)
	if !strings.Contains(body2, fmt.Sprintf("#%d b —", b)) || strings.Contains(body2, fmt.Sprintf("#%d a —", a)) {
		t.Fatalf("second reminder:\n%s", body2)
	}
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", rid2), "hq/boss", map[string]any{"body": "ok", "verdicts": []wire.LeftVerdict{v(b, "done", "")}}); w.Code != 400 || !strings.Contains(w.Body.String(), "goes with reply --complete") {
		t.Fatalf("verdict on a reply that does not complete: %d %s", w.Code, w.Body)
	}
	if w := callNew(s, "POST", "/api/messages", "hq/boss", map[string]any{"recipient": "team/worker", "subject": "x", "body": "x", "complete": []int64{rid2}, "verdicts": []wire.LeftVerdict{v(b, "done", "")}}); w.Code != 201 {
		t.Fatalf("send --complete with the verdict: %d %s", w.Code, w.Body)
	}
	if st, _ := leftState(t, s, b); st != wire.LeftDone {
		t.Fatalf("kept item after send --complete: %s", st)
	}
}

// complete ID --verdict ITEM=VERDICT (#565) from the CLI; a refusal names
// the items and the command.
// A reminder whose items all ended since carries left_remind [] (#571), so a
// plugin does not fall back to the verdicts on its last line.
func TestLeftRemindEventNoneOpen(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	leftSweep(t, s, clock, 0)
	_, ids := leftWithItems(t, s, clock, "build", "a")
	rid, _ := remindOf(t, s, clock, 25*time.Hour)
	if w := callNew(s, "POST", fmt.Sprintf("/api/left/%d/done", ids["a"]), "hq/boss", wire.LeftEnd{}); w.Code != 200 {
		t.Fatalf("left done: %d %s", w.Code, w.Body)
	}
	w := callNew(s, "GET", "/api/events/poll?timeout=0", "hq/boss", nil)
	if want := fmt.Sprintf(`"id":%d,`, rid); !strings.Contains(w.Body.String(), want) || !strings.Contains(w.Body.String(), `"left_remind":[]`) {
		t.Fatalf("want reminder #%d with left_remind []: %s", rid, w.Body)
	}
	if n := strings.Count(w.Body.String(), `"left_remind"`); n != 1 {
		t.Fatalf("left_remind on %d events, want only the reminder: %s", n, w.Body)
	}
}

func TestCLILeftVerdict(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "a"}, "a")
	h.configure(nil, "b")
	h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", "build", "--body", "x")
	h.mustRun(nil, "reply", "1", "--complete", "--left", "docs", "--left", "tests", "--body", "built")
	items := openLeft(t, h.s)
	if len(items) != 2 {
		t.Fatalf("open %+v", items)
	}
	// Due a day ago, against the real clock.
	past := time.Now().UTC().Add(-48 * time.Hour)
	if _, err := h.s.db.Exec(`UPDATE left_items SET recorded_at=?`, past.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.db.Exec(`INSERT OR REPLACE INTO remind_settings(kind,after_s,since) VALUES('left',86400,?)`, past.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if got := sweepOnce(t, h.s).LeftRemind; got != 1 {
		t.Fatalf("sent %d reminders", got)
	}
	var rid int64
	if err := h.s.db.QueryRow(`SELECT id FROM messages WHERE idem LIKE 'left-remind:%'`).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	id := func(i int) string { return fmt.Sprint(items[i].ID) }
	r := h.run(nil, "--profile", "a", "complete", fmt.Sprint(rid), "--verdict", id(0)+"=done")
	if r.code == 0 || !strings.Contains(r.stderr+r.stdout, "#"+id(1)) || !strings.Contains(r.stderr+r.stdout, "--verdict "+id(1)+"=keep") {
		t.Fatalf("complete with a verdict missing: %+v", r)
	}
	if r := h.run(nil, "--profile", "a", "complete", fmt.Sprint(rid), "--verdict", "x=done"); r.code == 0 {
		t.Fatalf("a verdict without an item ID: %+v", r)
	}
	h.mustRun(nil, "--profile", "a", "complete", fmt.Sprint(rid), "--verdict", id(0)+"=issue:https://git.example/team/repo/issues/3", "--verdict", id(1)+"=keep")
	if st, val := leftState(t, h.s, items[0].ID); st != wire.LeftIssue || val != "https://git.example/team/repo/issues/3" {
		t.Fatalf("issue verdict: %s %q", st, val)
	}
	if got := openLeft(t, h.s); len(got) != 1 || got[0].ID != items[1].ID {
		t.Fatalf("open after the verdicts: %+v", got)
	}
}
