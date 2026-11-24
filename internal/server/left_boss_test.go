package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// A boss role ends what another role's directive left (#570): a global boss
// any item, in all four ways, recorded as the one who ended it; a group boss
// only items of directives inside its group; any other role still 403. A
// boss passes the item on in a directive the item's own sender sent.
func TestLeftEndByBoss(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	s.admin.ProxyToken = "proxy-secret"
	if err := seedOther(s); err != nil {
		t.Fatal(err)
	}
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('partner-a/boss','secret-partner-a/boss','now'),('partner-a/dev','secret-partner-a/dev','now')`); err != nil {
		t.Fatal(err)
	}
	putGroup(t, s, "partner-a", "")

	// endDirective sends a directive from → to, which to ends leaving items.
	endDirective := func(from, to string, items ...string) (wire.Message, map[string]int64) {
		t.Helper()
		d := apiMail(t, s, clock, from, "/api/messages", map[string]any{"recipient": to, "subject": "work"})
		if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", d.ID), to, map[string]any{"body": "done", "done": true, "left": items}); w.Code != 201 {
			t.Fatalf("reply as %s: %d %s", to, w.Code, w.Body)
		}
		ids := map[string]int64{}
		for _, it := range openLeft(t, s) {
			if it.Directive == d.ID {
				ids[it.Text] = it.ID
			}
		}
		return d, ids
	}
	end := func(role string, id int64, action string, body wire.LeftEnd, code int) wire.LeftItem {
		t.Helper()
		w := callNew(s, "POST", fmt.Sprintf("/api/left/%d/%s", id, action), role, body)
		if w.Code != code {
			t.Fatalf("%s %d by %s: %d %s, want %d", action, id, role, w.Code, w.Body, code)
		}
		var it wire.LeftItem
		if code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &it); err != nil || it.EndedBy != role {
				t.Fatalf("%s %d by %s: ended_by %q (%v)", action, id, role, it.EndedBy, err)
			}
		}
		return it
	}

	// Inside group partner-a, sent by a role that is no boss.
	_, in := endDirective("partner-a/coordinator", "partner-a/dev", "a", "b", "c", "d", "e", "f")
	// Outside it: the global boss's own directive to an internal role.
	_, out := endDirective("hq/boss", "team/worker", "x")

	// Not a boss: the recipient and an unrelated role are refused.
	end("partner-a/dev", in["e"], "done", wire.LeftEnd{}, 403)
	end("team/worker", in["e"], "done", wire.LeftEnd{}, 403)

	// The global boss ends another role's items in all four ways.
	if it := end("hq/boss", in["a"], "done", wire.LeftEnd{}, 200); it.State != wire.LeftDone {
		t.Fatalf("done: %+v", it)
	}
	if it := end("hq/boss", in["b"], "drop", wire.LeftEnd{Reason: "not needed"}, 200); it.State != wire.LeftDropped || it.Value != "not needed" {
		t.Fatalf("drop: %+v", it)
	}
	if it := end("hq/boss", in["c"], "issue", wire.LeftEnd{URL: "https://example.com/i/1"}, 200); it.State != wire.LeftIssue {
		t.Fatalf("issue: %+v", it)
	}
	// pass: the boss's own directive is not the item's sender's; one the
	// item's sender sent takes it.
	mine := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "partner-a/dev", "subject": "boss's"})
	end("hq/boss", in["d"], "pass", wire.LeftEnd{Directive: mine.ID}, 400)
	next := apiMail(t, s, clock, "partner-a/coordinator", "/api/messages", map[string]any{"recipient": "partner-a/dev", "subject": "take over d"})
	if it := end("hq/boss", in["d"], "pass", wire.LeftEnd{Directive: next.ID}, 200); it.State != wire.LeftPassed || it.Value != fmt.Sprint(next.ID) {
		t.Fatalf("pass: %+v", it)
	}

	// The group boss: inside its group yes, outside no.
	end("partner-a/boss", in["e"], "done", wire.LeftEnd{}, 200)
	end("partner-a/boss", out["x"], "done", wire.LeftEnd{}, 403)
	// The sender itself still may.
	end("partner-a/coordinator", in["f"], "drop", wire.LeftEnd{Reason: "moot"}, 200)

	// The ledger keeps who ended each, the boss though it sent none.
	got := map[string]string{}
	rows, err := s.db.Query(`SELECT text,ended_by FROM left_items WHERE state<>'open'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var text, by string
		if err := rows.Scan(&text, &by); err != nil {
			t.Fatal(err)
		}
		got[text] = by
	}
	want := map[string]string{"a": "hq/boss", "b": "hq/boss", "c": "hq/boss", "d": "hq/boss", "e": "partner-a/boss", "f": "partner-a/coordinator"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ended_by: %v, want %v", got, want)
	}
}
