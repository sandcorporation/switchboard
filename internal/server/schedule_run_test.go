package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func (c *scheduleClock) run(token string, id int64) (wire.ScheduleRun, int, string) {
	c.t.Helper()
	w := call(c.s, "POST", "/api/schedules/"+itoa(id)+"/run", token, nil, "")
	var out wire.ScheduleRun
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			c.t.Fatal(err)
		}
	}
	return out, w.Code, w.Body.String()
}

// Running now mails the prompt once and moves neither the next fire, the
// expiry, the missed count nor last_fire_at; a run again inside the window
// is the same run, one later is a new mail. A paused schedule runs; an
// ended or expired one is 409, another role's 404.
func TestScheduleRunNow(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	sc, code, body := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "hourly check", "label": "hourly"}, "")
	if code != 201 {
		t.Fatalf("create %d %s", code, body)
	}
	c.at(10 * time.Minute)
	before := c.get(sc.ID)
	out, code, body := c.run("secret-a", sc.ID)
	if code != 200 || out.Repeated || out.MailID == 0 {
		t.Fatalf("run %d %s", code, body)
	}
	got := out.Schedule
	if got.NextFireAt == nil || !got.NextFireAt.Equal(*before.NextFireAt) || !got.ExpiresAt.Equal(before.ExpiresAt) || got.LastFireAt != nil || got.Missed != 0 {
		t.Fatalf("run moved the schedule: before %+v after %+v", before, got)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(c.now) {
		t.Fatalf("last run %v", got.LastRunAt)
	}
	m := scheduleMail(t, c.s, "a")
	if len(m) != 1 || m[0].Subject != "[주기] hourly · 지금 실행" || m[0].Body != "hourly check" || m[0].ID != out.MailID {
		t.Fatalf("mail %+v", m)
	}
	// A double click: the same run, no second mail.
	c.now = c.now.Add(5 * time.Second)
	again, code, _ := c.run("secret-a", sc.ID)
	if code != 200 || !again.Repeated || again.MailID != out.MailID {
		t.Fatalf("repeat %d %+v", code, again)
	}
	if n := len(scheduleMail(t, c.s, "a")); n != 1 {
		t.Fatalf("repeat sent %d mails", n)
	}
	// Paused: it runs, and stays paused.
	if _, code, body := c.pause("secret-a", sc.ID, "pause", nil); code != 200 {
		t.Fatalf("pause %d %s", code, body)
	}
	c.now = c.now.Add(wire.ScheduleRunWindow)
	paused, code, body := c.run("secret-a", sc.ID)
	if code != 200 || paused.Repeated || paused.MailID == out.MailID || paused.Schedule.State != wire.SchedulePaused {
		t.Fatalf("paused run %d %s", code, body)
	}
	if n := len(scheduleMail(t, c.s, "a")); n != 2 {
		t.Fatalf("paused run mails %d", n)
	}
	if h := pauseHistory(t, c.s, sc.ID); strings.Join(h, ";") != "run|a|0||;pause|a|0||;run|a|0||" {
		t.Fatalf("history %v", h)
	}
	if n := countMail(t, c.s, "a", "[예약%"); n != 0 {
		t.Fatalf("the owner was told of its own run: %d", n)
	}
	if _, code, _ := c.run("secret-b", sc.ID); code != 404 {
		t.Fatalf("other role %d", code)
	}
	for _, state := range []string{wire.ScheduleEnded, wire.ScheduleExpired} {
		if _, err := c.s.db.Exec(`UPDATE schedules SET state=? WHERE id=?`, state, sc.ID); err != nil {
			t.Fatal(err)
		}
		if _, code, _ := c.run("secret-a", sc.ID); code != 409 {
			t.Fatalf("%s run %d", state, code)
		}
	}
	if w := call(c.s, "GET", "/api/schedules/"+itoa(sc.ID)+"/run", "secret-a", nil, ""); w.Code != 405 {
		t.Fatalf("GET run %d", w.Code)
	}
}

// A boss role over the owner and the admin run it; the owner is told each
// time. A role outside gets 404 on its path and 403 through /api/boss/.
func TestScheduleRunByOthersTellsOwner(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	s.admin.ProxyToken = "proxy-secret"
	w := callAs(s, "POST", "/api/schedules", partnerDevToken, partnerDevRole, map[string]any{"machine": "m1", "kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "p", "label": "hourly"})
	var sc wire.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil || w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	path := "schedules/" + itoa(sc.ID) + "/run"
	if w := callAs(s, "POST", "/api/"+path, "secret-a", "a", nil); w.Code != 404 {
		t.Fatalf("outside role %d", w.Code)
	}
	if w := callAs(s, "POST", "/api/boss/"+path, partnerBDevToken, partnerBDevRole, nil); w.Code != 403 {
		t.Fatalf("non-boss through /api/boss %d", w.Code)
	}
	if w := callAs(s, "POST", "/api/"+path, partnerBossToken, partnerBossRole, nil); w.Code != 200 {
		t.Fatalf("group boss run %d %s", w.Code, w.Body)
	}
	// Past the window, so the admin's run is a new one.
	later := sc.CreatedAt.Add(wire.ScheduleRunWindow)
	s.clock = func() time.Time { return later }
	w = adminWrite(s, "POST", "/api/admin/"+path, `{}`)
	var out wire.ScheduleRun
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Repeated || out.MailID == 0 {
		t.Fatalf("admin run %d %s", w.Code, w.Body)
	}
	if n := countMail(t, s, partnerDevRole, "[주기] hourly · 지금 실행"); n != 2 {
		t.Fatalf("run mails %d", n)
	}
	if n := countMail(t, s, partnerDevRole, "[예약 지금 실행] hourly"); n != 2 {
		t.Fatalf("notices %d", n)
	}
	if h := pauseHistory(t, s, sc.ID); len(h) != 2 || !strings.HasPrefix(h[0], "run|"+partnerBossRole+"|0") || !strings.HasSuffix(h[1], "|1||") {
		t.Fatalf("history %v", h)
	}
}
