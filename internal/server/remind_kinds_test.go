package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// remindMailTo counts the system mail to role whose subject starts with
// prefix.
func remindMailTo(t *testing.T, s *Server, role, prefix string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE sender=? AND recipient=? AND subject LIKE ?`, identity.SystemSender, role, prefix+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func addRequestAs(t *testing.T, s *Server, clock *fakeClock, title string) wire.RequestDetail {
	t.Helper()
	clock.add(time.Minute)
	return decodeRequest(t, callAs(s, "POST", "/api/requests", tokenFor("hq/boss"), "hq/boss", map[string]any{"title": title, "text": "the owner's words"}), 201)
}

func requestAs(t *testing.T, s *Server, id int64, action string, body any) {
	t.Helper()
	if w := callAs(s, "POST", fmt.Sprintf("/api/requests/%d/%s", id, action), tokenFor("hq/boss"), "hq/boss", body); w.Code != 200 {
		t.Fatalf("%s %d: %d %s", action, id, w.Code, w.Body)
	}
}

// #586: mail, left items and owner's requests run the same reminder rule,
// so one table drives all three: nothing at the switch-on however old the
// item, one mail to the kind's recipient once per time at a fixed gap,
// news starts the count again, and a new time counts from its change.
func TestRemindKindsShareOneRule(t *testing.T) {
	t.Parallel()
	const T = time.Hour
	kinds := []struct {
		kind   remind.Kind
		to     string
		prefix string
		// item makes one open item; news (nil: the kind has none) moves it.
		item func(t *testing.T, s *Server, clock *fakeClock) (news func())
	}{
		{remind.Mail, "team/worker", "[다시 알림]", func(t *testing.T, s *Server, clock *fakeClock) func() {
			d := directiveTo(t, s, clock, "do X")
			readAs(t, s, "team/worker", d.ID)
			return func() {
				apiMail(t, s, clock, "team/worker", fmt.Sprintf("/api/messages/%d/reply", d.ID), map[string]any{"subject": "re", "body": "working on it"})
			}
		}},
		{remind.Left, "hq/boss", "[남긴 것]", func(t *testing.T, s *Server, clock *fakeClock) func() {
			leftWithItems(t, s, clock, "do Y", "check the logs")
			return nil
		}},
		{remind.Request, "hq/boss", "[지시 소식 없음]", func(t *testing.T, s *Server, clock *fakeClock) func() {
			r := addRequestAs(t, s, clock, "ship it")
			return func() {
				clock.add(time.Second)
				requestAs(t, s, r.ID, "notes", map[string]any{"text": "and the docs"})
			}
		}},
	}
	for _, k := range kinds {
		t.Run(string(k.kind), func(t *testing.T) {
			t.Parallel()
			s, clock := slotsWithRoles(t)
			for _, other := range remind.Kinds {
				setRemind(t, s, other, 0)
			}
			news := k.item(t, s, clock)
			clock.add(5 * T) // quiet long before the time is set
			setRemind(t, s, k.kind, T)
			t0 := clock.now()
			total := 0
			step := func(at time.Time, want int, what string) {
				t.Helper()
				clock.add(at.Sub(clock.now()))
				sweepOnce(t, s)
				total += want
				if got := remindMailTo(t, s, k.to, k.prefix); got != total {
					t.Fatalf("%s: %d reminders to %s, want %d", what, got, k.to, total)
				}
			}
			step(t0, 0, "switch-on")
			step(t0.Add(T-time.Second), 0, "before T")
			step(t0.Add(T), 1, "at T")
			step(t0.Add(T), 0, "at T again")
			step(t0.Add(2*T-time.Second), 0, "before 2T")
			step(t0.Add(2*T), 1, "at 2T")
			next := t0.Add(3 * T)
			if news != nil {
				clock.add(10 * time.Minute)
				news()
				at := clock.now()
				step(next, 0, "at 3T after news")
				step(at.Add(T-time.Second), 0, "before news + T")
				next = at.Add(T)
			}
			step(next, 1, "one time after the last news or reminder")
			// A new time (2T) counts from its change.
			setRemind(t, s, k.kind, 2*T)
			c := clock.now()
			step(c.Add(2*T-time.Second), 0, "before the change + 2T")
			step(c.Add(2*T), 1, "at the change + 2T")
		})
	}
}

// #586: a request waiting on the owner (the boss role's mark, or an open
// linked question) gets no mail; past the time it is overdue on the 지시
// tab and in the settings row's 대표 답 대기 count. Clearing the mark is
// news: the count starts again from it.
func TestRequestRemindWaitingOnOwner(t *testing.T) {
	t.Parallel()
	const T = time.Hour
	s, clock := slotsWithRoles(t)
	s.admin.ProxyToken = "proxy-secret"
	setRemind(t, s, remind.Request, T)
	r := addRequestAs(t, s, clock, "pick a name")
	requestAs(t, s, r.ID, "wait", map[string]any{})
	clock.add(10 * T)
	sweepOnce(t, s)
	if n := remindMailTo(t, s, "hq/boss", "[지시 소식 없음]"); n != 0 {
		t.Fatalf("waiting request reminded %d times", n)
	}
	d := decodeRequest(t, callAs(s, "GET", fmt.Sprintf("/api/requests/%d", r.ID), tokenFor("hq/boss"), "hq/boss", nil), 200)
	if d.Status != wire.RequestWaiting || d.OwnerWait == nil || !d.Overdue {
		t.Fatalf("waiting request: status %s wait %v overdue %v", d.Status, d.OwnerWait, d.Overdue)
	}
	w := adminRequest(s, "GET", "/api/admin/settings", "", nil)
	var st adminSettings
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &st) != nil || len(st.Remind) != 3 {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	if row := st.Remind[2]; row.Kind != "request" || row.Waiting != 1 || row.Overdue != 0 || row.AfterSeconds != 3600 || row.To == "" {
		t.Fatalf("request row %+v", row)
	}

	requestAs(t, s, r.ID, "wait", map[string]any{"off": true})
	cleared := clock.now()
	step := func(at time.Time, want int) {
		t.Helper()
		clock.add(at.Sub(clock.now()))
		sweepOnce(t, s)
		if got := remindMailTo(t, s, "hq/boss", "[지시 소식 없음]"); got != want {
			t.Fatalf("at %s: %d reminders, want %d", at.Sub(cleared), got, want)
		}
	}
	step(cleared.Add(T-time.Second), 0)
	step(cleared.Add(T), 1)
	var body string
	if err := s.db.QueryRow(`SELECT body FROM messages WHERE sender=? AND subject LIKE '[지시 소식 없음]%'`, identity.SystemSender).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, fmt.Sprintf("지시 #%d pick a name", r.ID)) || !strings.Contains(body, "마지막 소식: "+cleared.UTC().Format("2006-01-02 15:04")) {
		t.Fatalf("body %q", body)
	}
	// The settings row counts it as overdue now, not waiting.
	w = adminRequest(s, "GET", "/api/admin/settings", "", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if row := st.Remind[2]; row.Waiting != 0 || row.Overdue != 1 {
		t.Fatalf("request row after clearing %+v", row)
	}
	// Setting the time from the settings screen counts from the change.
	if w := adminWrite(s, "PUT", "/api/admin/settings/remind/request", `{"after":"2h"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"after":"2h"`) {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "PUT", "/api/admin/settings/remind/nope", `{"after":"2h"}`); w.Code != 404 {
		t.Fatalf("unknown kind: %d", w.Code)
	}
	if w := adminWrite(s, "PUT", "/api/admin/settings/remind/mail", `{"after":"5s"}`); w.Code != 400 {
		t.Fatalf("too short: %d", w.Code)
	}
}
