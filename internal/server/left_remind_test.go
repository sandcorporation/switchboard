package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
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
