package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// remindGap is the mail reminder time in these tests (T).
const remindGap = time.Hour

// setRemind sets kind k's reminder time as of the server's clock, as the
// settings screen does.
func setRemind(t *testing.T, s *Server, k remind.Kind, after time.Duration) {
	t.Helper()
	if _, err := remind.Save(context.Background(), s.db, k, after, s.now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func remindOn(t *testing.T, s *Server) { t.Helper(); setRemind(t, s, remind.Mail, remindGap) }

// remindKeys lists the idempotency keys of the reminder mail stored so far,
// in send order, as "<recipient> <key>".
func remindKeys(t *testing.T, s *Server) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT recipient,idem FROM messages WHERE sender=? AND idem LIKE 'remind%' ORDER BY id`, identity.SystemSender)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var to, key string
		if err = rows.Scan(&to, &key); err != nil {
			t.Fatal(err)
		}
		out = append(out, to+" "+key)
	}
	return out
}

func readAs(t *testing.T, s *Server, role string, id int64) {
	t.Helper()
	if w := callAs(s, "GET", fmt.Sprintf("/api/messages/%d", id), tokenFor(role), role, nil); w.Code != 200 {
		t.Fatalf("read %d as %s: %d %s", id, role, w.Code, w.Body)
	}
}

// sweepAt moves the clock to at and sweeps, returning how many reminders
// and notices went out.
func sweepAt(t *testing.T, s *Server, clock *fakeClock, at time.Time) int {
	t.Helper()
	clock.add(at.Sub(clock.now()))
	return sweepOnce(t, s).Remind
}

func countRows(t *testing.T, s *Server, query string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Off (the start value, #586): no reminder mail, however old the read
// directive.
func TestRemindOffSendsNothing(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	d := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "do X"})
	readAs(t, s, "team/worker", d.ID)
	before := countRows(t, s, `SELECT COUNT(*) FROM messages`)
	for _, h := range []time.Duration{time.Hour, 3 * time.Hour, 100 * time.Hour} {
		if n := sweepAt(t, s, clock, d.CreatedAt.Add(h)); n != 0 {
			t.Fatalf("off: sweep sent %d", n)
		}
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM messages`); got != before {
		t.Fatalf("off: messages %d -> %d", before, got)
	}
	if got := openIDs(t, s); !sameIDs(got, []int64{d.ID}) {
		t.Fatalf("off: open %v", got)
	}
}

// On: a reminder every T at a fixed gap (#586: no doubling, no limit, no
// sender notice), each exactly once across repeated sweeps and a server
// restart. Unread and --no-reply directives are never reminded.
func TestRemindScheduleOnceEach(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	remindOn(t, s)
	d := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "do X", "task_id": "T-1"})
	unread := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "later"})
	fyi := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "fyi", "no_reply": true})
	if !fyi.NoReply {
		t.Fatalf("no_reply not stored: %+v", fyi)
	}
	readAs(t, s, "team/worker", d.ID)
	readAs(t, s, "team/worker", fyi.ID)
	if got := openIDs(t, s); !sameIDs(got, []int64{d.ID, unread.ID}) {
		t.Fatalf("open %v, want %d %d (not the --no-reply %d)", got, d.ID, unread.ID, fyi.ID)
	}

	at := d.CreatedAt
	worker := "team/worker " + "remind:"
	steps := []struct {
		at   time.Duration
		sent int
		want []string
	}{
		{remindGap - time.Second, 0, nil},
		{remindGap, 1, []string{fmt.Sprintf("%s%d:1", worker, d.ID)}},
		{remindGap, 0, nil},
		{2*remindGap - time.Second, 0, nil},
		{2 * remindGap, 1, []string{fmt.Sprintf("%s%d:2", worker, d.ID)}},
		{3 * remindGap, 1, []string{fmt.Sprintf("%s%d:3", worker, d.ID)}},
		{4*remindGap - time.Second, 0, nil},
		{4 * remindGap, 1, []string{fmt.Sprintf("%s%d:4", worker, d.ID)}},
		{20 * remindGap, 1, []string{fmt.Sprintf("%s%d:5", worker, d.ID)}},
	}
	var want []string
	for i, st := range steps {
		if i == 5 {
			// A restart: a new server over the same database.
			s = &Server{db: s.db, tokens: s.tokens, clock: s.clock, bossRoles: s.bossRoles, presenceCfg: s.presenceCfg}
			if n := sweepOnce(t, s); n.Remind != 0 {
				t.Fatalf("restart re-sent %d", n.Remind)
			}
		}
		if n := sweepAt(t, s, clock, at.Add(st.at)); n != st.sent {
			t.Fatalf("step %d at %s: sent %d, want %d (%v)", i, st.at, n, st.sent, remindKeys(t, s))
		}
		want = append(want, st.want...)
		if got := remindKeys(t, s); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("step %d at %s: reminders %v, want %v", i, st.at, got, want)
		}
	}

	var first wire.Message
	for _, m := range systemMail(t, s, workerToken, "team/worker") {
		if strings.HasSuffix(m.Subject, "do X") {
			first = m
			break
		}
	}
	if first.Subject != fmt.Sprintf("[다시 알림] #%d do X", d.ID) || first.Body != fmt.Sprintf("#%d 가 끝났으면 reply --done 으로 보고하고, 아직이면 reply 로 진행을 알려라", d.ID) || first.Status != "pending" || first.TaskID != "T-1" {
		t.Fatalf("reminder %+v", first)
	}
	// Reminders are system mail: never directives themselves.
	if got := openIDs(t, s); !sameIDs(got, []int64{d.ID, unread.ID}) {
		t.Fatalf("open after reminders %v", got)
	}
}

// Complete stops the reminders; a reply (reply_to) leaves the directive open
// and only moves the next reminder to a gap after it (#269); new mail that
// only mentions #id (or shares the task) changes nothing.
func TestRemindStopsOnReplyOrComplete(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	remindOn(t, s)
	replied := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "a"})
	done := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "b"})
	mentioned := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "c", "task_id": "T-9"})
	for _, d := range []wire.Message{replied, done, mentioned} {
		readAs(t, s, "team/worker", d.ID)
	}
	start := mentioned.CreatedAt
	if n := sweepAt(t, s, clock, start.Add(remindGap)); n != 3 {
		t.Fatalf("first round sent %d, want 3", n)
	}
	clock.add(30 * time.Minute)
	apiMail(t, s, clock, "team/worker", fmt.Sprintf("/api/messages/%d/reply", replied.ID), map[string]any{"subject": "re", "body": "working on it"})
	apiMail(t, s, clock, "team/worker", fmt.Sprintf("/api/messages/%d/complete", done.ID), map[string]any{"left_none": true})
	apiMail(t, s, clock, "team/worker", "/api/messages", map[string]any{"recipient": "hq/boss", "subject": fmt.Sprintf("status, see #%d", mentioned.ID), "task_id": "T-9"})
	if got := openIDs(t, s); !sameIDs(got, []int64{replied.ID, mentioned.ID}) {
		t.Fatalf("open %v, want %d and %d (a reply does not close)", got, replied.ID, mentioned.ID)
	}
	// At 2T only the mentioned one: the reply moved #replied to reply + T.
	if n := sweepAt(t, s, clock, start.Add(2*remindGap)); n != 1 {
		t.Fatalf("at 2T sent %d, want 1 (%v)", n, remindKeys(t, s))
	}
	sweepAt(t, s, clock, start.Add(3*remindGap))
	want := []string{
		fmt.Sprintf("team/worker remind:%d:1", replied.ID),
		fmt.Sprintf("team/worker remind:%d:1", done.ID),
		fmt.Sprintf("team/worker remind:%d:1", mentioned.ID),
		fmt.Sprintf("team/worker remind:%d:2", mentioned.ID),
		fmt.Sprintf("team/worker remind:%d:2", replied.ID),
		fmt.Sprintf("team/worker remind:%d:3", mentioned.ID),
	}
	if got := remindKeys(t, s); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("reminders %v, want %v", got, want)
	}
}

// Directives quiet long before the time was set count from when it was set
// (#268, #586): switching on sends nothing at once and the first reminders
// come one time later; a new value counts from its change again.
func TestRemindOlderDirectivesCountFromSet(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	old := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "old"})
	older := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "older"})
	readAs(t, s, "team/worker", old.ID)
	readAs(t, s, "team/worker", older.ID)
	clock.add(5 * remindGap) // both are long past T before reminders exist
	remindOn(t, s)
	set := clock.now()
	if n := sweepOnce(t, s).Remind; n != 0 {
		t.Fatalf("switch-on sent %d, want no burst", n)
	}
	if n := sweepAt(t, s, clock, set.Add(remindGap-time.Second)); n != 0 {
		t.Fatalf("sent %d before set + T", n)
	}
	if n := sweepAt(t, s, clock, set.Add(remindGap)); n != 2 {
		t.Fatalf("at set + T sent %d, want 2", n)
	}
	// Changing to 3T at set + 1.5T: nothing until 3T after the change.
	clock.add(remindGap / 2)
	setRemind(t, s, remind.Mail, 3*remindGap)
	changed := clock.now()
	if n := sweepAt(t, s, clock, changed.Add(3*remindGap-time.Second)); n != 0 {
		t.Fatalf("sent %d before the change + 3T", n)
	}
	if n := sweepAt(t, s, clock, changed.Add(3*remindGap)); n != 2 {
		t.Fatalf("at the change + 3T sent %d, want 2", n)
	}
}

// Directives to a boss role are not reminded.
func TestRemindSkipsBossRecipients(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	s.bossRoles["team/worker"] = false
	s.bossRoles["ops/lead"] = true
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES('ops/lead','secret-ops/lead','now')`); err != nil {
		t.Fatal(err)
	}
	remindOn(t, s)
	d := apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "ops/lead", "subject": "peer"})
	readAs(t, s, "ops/lead", d.ID)
	if n := sweepAt(t, s, clock, d.CreatedAt.Add(20*remindGap)); n != 0 {
		t.Fatalf("boss recipient reminded: %v", remindKeys(t, s))
	}
}

func TestCLINoReply(t *testing.T) {
	t.Parallel()
	h := newCLIHarness(t)
	h.configure([]string{"--profile", "a"}, "a")
	h.configure(nil, "b")
	h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", "fyi", "--body", "x", "--no-reply")
	h.mustRun(nil, "--profile", "a", "send", "--to", "b", "--subject", "work", "--body", "y")
	h.mustRun(nil, "reply", "1", "--subject", "ok", "--body", "thanks", "--no-reply")
	var sent []wire.Message
	if out := h.mustRun(nil, "--profile", "a", "outbox", "--json"); json.Unmarshal([]byte(out), &sent) != nil || len(sent) != 2 || sent[0].NoReply || !sent[1].NoReply {
		t.Fatalf("send --no-reply: %s", out)
	}
	if out := h.mustRun(nil, "outbox", "--json"); json.Unmarshal([]byte(out), &sent) != nil || len(sent) != 1 || !sent[0].NoReply {
		t.Fatalf("reply --no-reply: %s", out)
	}
	for _, cmd := range []string{"send", "reply"} {
		if !strings.Contains(h.mustRun(nil, cmd, "--help"), "--no-reply") {
			t.Fatalf("%s --help lacks --no-reply", cmd)
		}
	}
}
