package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// callLeftAs is callNew with an explicit token, for a numbered slot that
// signs in with its base's token.
func callLeftAs(s *Server, path, token, role string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	_ = json.NewEncoder(&b).Encode(body)
	r := httptest.NewRequest("POST", path, &b)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Switchboard-Role", role)
	r.Header.Set(config.HeaderPrefix+config.LeftHeaderName, "1")
	w := httptest.NewRecorder()
	s.api(w, r)
	return w
}

// A numbered slot and its base are one role for the ledger (#570): a
// directive sent by either is ended by the other, and by another slot. A
// role row whose name only looks like a slot (x-2 registered on its own) is
// a different role, so it is refused either way, even when that row was
// made after the directive was sent by slot x-2.
func TestLeftEndSlotAndBase(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('ops/coord','secret-ops/coord','now')`); err != nil {
		t.Fatal(err)
	}
	const tok = "secret-ops/coord"
	leave := func(from string) int64 {
		t.Helper()
		clock.add(60e9)
		w := callLeftAs(s, "/api/messages", tok, from, map[string]any{"recipient": "team/worker", "subject": "work"})
		var d wire.Message
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatalf("send as %s: %d %s", from, w.Code, w.Body)
		}
		if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/reply", d.ID), "team/worker", map[string]any{"body": "done", "done": true, "left": []string{"x"}}); w.Code != 201 {
			t.Fatalf("reply: %d %s", w.Code, w.Body)
		}
		for _, it := range openLeft(t, s) {
			if it.Directive == d.ID {
				return it.ID
			}
		}
		t.Fatalf("no item for directive %d", d.ID)
		return 0
	}
	end := func(id int64, token, as string, want int) {
		t.Helper()
		if w := callLeftAs(s, fmt.Sprintf("/api/left/%d/done", id), token, as, wire.LeftEnd{}); w.Code != want {
			t.Fatalf("done %d by %s: %d %s, want %d", id, as, w.Code, w.Body, want)
		}
	}
	end(leave("ops/coord"), tok, "ops/coord-2", 200)   // base sent, slot ends
	end(leave("ops/coord-2"), tok, "ops/coord", 200)   // slot sent, base ends
	end(leave("ops/coord-2"), tok, "ops/coord-3", 200) // slot sent, another slot ends
	end(leave("ops/coord-1"), tok, "ops/coord-3", 200) // slot 1 is the base

	sentBySlot := leave("ops/coord-2")
	sentByBase := leave("ops/coord")
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('ops/coord-2','secret-own','now')`); err != nil {
		t.Fatal(err)
	}
	end(sentBySlot, tok, "ops/coord", 403)
	end(sentByBase, "secret-own", "ops/coord-2", 403)
}

// A worker reports in new mail (not a reply), so the report is a directive
// of its own and what the coordinator wrote when it completed it hangs on
// the report (#570). The coordinator, who sent the first directive between
// them under that task, ends those items in all four ways; the worker, the
// report's sender, still may. Neither gains more: the coordinator's items on
// a boss's directive and the worker's on the coordinator's follow-up stay
// refused.
func TestLeftEndByRecorderOfReport(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('ops/coord','secret-ops/coord','now')`); err != nil {
		t.Fatal(err)
	}
	const coord, tok = "ops/coord", "secret-ops/coord"
	as := func(role string) string {
		if role == coord {
			return tok
		}
		return tokenFor(role)
	}
	post := func(role, path string, body any, want int) wire.Message {
		t.Helper()
		clock.add(60e9)
		w := callLeftAs(s, path, as(role), role, body)
		if w.Code != want {
			t.Fatalf("%s by %s: %d %s, want %d", path, role, w.Code, w.Body, want)
		}
		var m wire.Message
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return m
	}
	items := func(directive int64) map[string]int64 {
		out := map[string]int64{}
		for _, it := range openLeft(t, s) {
			if it.Directive == directive {
				out[it.Text] = it.ID
			}
		}
		return out
	}

	p := post(coord, "/api/messages", map[string]any{"recipient": "team/worker", "subject": "work", "task_id": "t1"}, 201)
	post("team/worker", fmt.Sprintf("/api/messages/%d/reply", p.ID), map[string]any{"body": "done", "done": true, "left_none": true}, 201)
	report := post("team/worker", "/api/messages", map[string]any{"recipient": coord, "subject": "report", "task_id": "t1"}, 201)
	post(coord, fmt.Sprintf("/api/messages/%d/complete", report.ID), map[string]any{"left": []string{"a", "b", "c", "d", "e"}}, 200)
	ids := items(report.ID)
	if len(ids) != 5 {
		t.Fatalf("items on the report: %v", ids)
	}

	end := func(role string, id int64, action string, body wire.LeftEnd, want int) {
		t.Helper()
		w := callLeftAs(s, fmt.Sprintf("/api/left/%d/%s", id, action), as(role), role, body)
		var it wire.LeftItem
		if w.Code != want || (want == 200 && (json.Unmarshal(w.Body.Bytes(), &it) != nil || it.EndedBy != role)) {
			t.Fatalf("%s %d by %s: %d %s, want %d", action, id, role, w.Code, w.Body, want)
		}
	}
	end(coord, ids["a"], "done", wire.LeftEnd{}, 200)
	end(coord, ids["b"], "drop", wire.LeftEnd{Reason: "moot"}, 200)
	end(coord, ids["c"], "issue", wire.LeftEnd{URL: "https://example.com/i/1"}, 200)
	// pass: into a directive the coordinator sent, not one the worker sent.
	theirs := post("team/worker", "/api/messages", map[string]any{"recipient": coord, "subject": "more", "task_id": "t9"}, 201)
	end(coord, ids["d"], "pass", wire.LeftEnd{Directive: theirs.ID}, 400)
	next := post(coord, "/api/messages", map[string]any{"recipient": "team/worker", "subject": "take over d", "task_id": "t1"}, 201)
	end(coord, ids["d"], "pass", wire.LeftEnd{Directive: next.ID}, 200)
	end("team/worker", ids["e"], "done", wire.LeftEnd{}, 200)

	// The worker's items on the coordinator's follow-up stay the
	// coordinator's.
	post("team/worker", fmt.Sprintf("/api/messages/%d/reply", next.ID), map[string]any{"body": "done", "done": true, "left": []string{"y"}}, 201)
	end("team/worker", items(next.ID)["y"], "done", wire.LeftEnd{}, 403)
	end(coord, items(next.ID)["y"], "done", wire.LeftEnd{}, 200)

	// A boss's directive: what the coordinator wrote is the boss's to end.
	b := post("hq/boss", "/api/messages", map[string]any{"recipient": coord, "subject": "boss work", "task_id": "t1"}, 201)
	post(coord, fmt.Sprintf("/api/messages/%d/complete", b.ID), map[string]any{"left": []string{"z"}}, 200)
	end(coord, items(b.ID)["z"], "done", wire.LeftEnd{}, 403)
}
