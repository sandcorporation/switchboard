package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Held mail gets a new ID each time a session takes it; the ID its sender got
// at send time still answers read, reply, complete, close and withdraw, on
// the mail it is now, and only for whoever may see that mail (#392).
func TestHeldMailOldIDFollowsToNewID(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('ops/other','secret-ops/other','now')`); err != nil {
		t.Fatal(err)
	}
	slotDo(t, s, "register", ident("m", "/x")) // A: team/worker
	d := bossSend(t, s, "team/worker", "do X")
	back := bossSend(t, s, "team/worker", "take back")
	done := bossSend(t, s, "team/worker", "done by send")

	// A's lease ends unread twice; each time A comes back it takes the mail
	// under a new ID.
	var a wire.SlotGrant
	for i := 0; i < 2; i++ {
		clock.add(2 * time.Minute)
		slotDo(t, s, "register", ident("m", "/y"))
		a = slotDo(t, s, "register", ident("m", "/x"))
	}
	if got := inboxSubjects(t, s, a.Role); strings.Join(got, ",") != "done by send,take back,do X" {
		t.Fatalf("A (%s) inbox: %v", a.Role, got)
	}

	get := func(as, token string, id int64) (wire.Message, int) {
		w := callAs(s, "GET", "/api/messages/"+itoa(id), token, as, nil)
		var m wire.Message
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return m, w.Code
	}
	now, code := get("hq/boss", tokenFor("hq/boss"), d.ID)
	if code != 200 || now.ID == d.ID || now.Subject != "do X" || now.HeldID != d.ID {
		t.Fatalf("sender reads old #%d: %d %+v (want the new ID, held_id the sent one)", d.ID, code, now)
	}
	if _, code := get("ops/other", tokenFor("ops/other"), d.ID); code != 404 {
		t.Fatalf("another role reads old #%d: %d, want 404", d.ID, code)
	}

	w := callAs(s, "POST", "/api/messages/"+itoa(back.ID)+"/withdraw", tokenFor("hq/boss"), "hq/boss", nil)
	var wd wire.Withdrawn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &wd) != nil || wd.ID == back.ID || !wd.Withdrawn {
		t.Fatalf("withdraw old #%d: %d %s", back.ID, w.Code, w.Body)
	}
	if got := inboxSubjects(t, s, a.Role); strings.Join(got, ",") != "done by send,do X" {
		t.Fatalf("A inbox after withdraw: %v", got)
	}

	if m, code := get(a.Role, workerToken, d.ID); code != 200 || m.ID != now.ID {
		t.Fatalf("recipient reads old #%d: %d %+v", d.ID, code, m)
	}
	w = callAs(s, "POST", "/api/messages/"+itoa(d.ID)+"/reply", workerToken, a.Role, map[string]any{"body": "on it"})
	var reply wire.Message
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.ReplyTo != now.ID || reply.Recipient != "hq/boss" {
		t.Fatalf("reply to old #%d: %d %s", d.ID, w.Code, w.Body)
	}
	w = callAs(s, "POST", "/api/messages/"+itoa(d.ID)+"/complete", workerToken, a.Role, wire.LeftNote{LeftNone: true})
	var st wire.Message
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &st) != nil || st.ID != now.ID || st.Status != "complete" {
		t.Fatalf("complete old #%d: %d %s", d.ID, w.Code, w.Body)
	}
	w = callAs(s, "POST", "/api/messages/"+itoa(d.ID)+"/close", tokenFor("hq/boss"), "hq/boss", nil)
	var cl wire.Closed
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &cl) != nil || cl.ID != now.ID || !cl.Closed {
		t.Fatalf("close old #%d: %d %s", d.ID, w.Code, w.Body)
	}
	if w = callAs(s, "POST", "/api/messages/"+itoa(d.ID)+"/close", tokenFor("ops/other"), "ops/other", nil); w.Code != 403 && w.Code != 404 {
		t.Fatalf("another role closes old #%d: %d", d.ID, w.Code)
	}

	// send --complete with the old ID completes the mail it is now.
	w = callAs(s, "POST", "/api/messages", workerToken, a.Role, map[string]any{"recipient": "hq/boss", "subject": "report", "complete": []int64{done.ID}, "left_none": true})
	if w.Code != 201 {
		t.Fatalf("send --complete old #%d: %d %s", done.ID, w.Code, w.Body)
	}
	if m, _ := get(a.Role, workerToken, done.ID); m.Status != "complete" || m.ID == done.ID {
		t.Fatalf("after send --complete: %+v", m)
	}

	var body string
	if err := s.db.QueryRow(`SELECT body FROM messages WHERE sender=? AND idem=?`, identity.SystemSender, "handoff-held:"+itoa(d.ID)).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "세션에 넘겨질 때 새 번호를 받는다. 옛 번호로도 찾을 수 있다") {
		t.Fatalf("[미수신] body: %q", body)
	}
}
