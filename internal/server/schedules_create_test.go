package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The dashboard makes a role schedule through the admin path (#551): for a
// role it names, machine "dashboard", and the owner is told. The write guard,
// a missing or unknown role and a role's token naming another role are
// refused.
func TestAdminScheduleCreate(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	c.s.admin.ProxyToken = "proxy-secret"
	body := `{"role":"a","kind":"cron","cron":"0 12 * * *","tz":"UTC","prompt":"check the build","label":"build"}`
	if w := adminRequest(c.s, "POST", "/api/admin/schedules", body, nil); w.Code != 403 {
		t.Fatalf("admin create without the write guard %d", w.Code)
	}
	w := adminWrite(c.s, "POST", "/api/admin/schedules", body)
	var sc wire.Schedule
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &sc) != nil {
		t.Fatalf("admin create %d %s", w.Code, w.Body.String())
	}
	next := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if sc.Role != "a" || sc.Machine != "dashboard" || sc.Label != "build" || sc.State != wire.ScheduleActive || sc.NextFireAt == nil || !sc.NextFireAt.Equal(next) {
		t.Fatalf("created %+v", sc)
	}
	if n := countMail(t, c.s, "a", "[예약 생김] build"); n != 1 {
		t.Fatalf("owner told %d times", n)
	}
	// The new schedule is in the list the dashboard reads.
	lw := adminWrite(c.s, "GET", "/api/admin/schedules", "")
	var list []wire.Schedule
	if lw.Code != 200 || json.Unmarshal(lw.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].ID != sc.ID {
		t.Fatalf("list %d %s", lw.Code, lw.Body.String())
	}
	// A once schedule without a label is named by its number.
	at := c.now.Add(2 * time.Hour)
	w = adminWrite(c.s, "POST", "/api/admin/schedules", `{"role":"a","kind":"once","at":"`+at.Format(time.RFC3339)+`","tz":"UTC","prompt":"p"}`)
	var once wire.Schedule
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &once) != nil || once.Kind != wire.ScheduleOnce || once.Label != "예약 #"+itoa(once.ID) || !once.NextFireAt.Equal(at) {
		t.Fatalf("once create %d %s", w.Code, w.Body.String())
	}
	for _, r := range []struct {
		name, body string
		want       int
	}{
		{"no role", `{"kind":"cron","cron":"0 9 * * *","tz":"UTC","prompt":"p"}`, 400},
		{"unknown role", `{"role":"nobody","kind":"cron","cron":"0 9 * * *","tz":"UTC","prompt":"p"}`, 404},
		{"bad cron", `{"role":"a","kind":"cron","cron":"nope","tz":"UTC","prompt":"p"}`, 400},
		{"no prompt", `{"role":"a","kind":"cron","cron":"0 9 * * *","tz":"UTC","prompt":" "}`, 400},
	} {
		if w := adminWrite(c.s, "POST", "/api/admin/schedules", r.body); w.Code != r.want {
			t.Fatalf("%s: %d %s, want %d", r.name, w.Code, w.Body.String(), r.want)
		}
	}
	// A role's token adds its own schedules only; its own name is accepted,
	// and the owner is not told of its own (two notices: the admin's two).
	if _, code, body := createSchedule(t, c.s, "secret-a", map[string]any{"role": "b", "kind": "cron", "cron": "0 9 * * *", "tz": "UTC", "prompt": "p"}, ""); code != 403 {
		t.Fatalf("role token naming another role %d %s", code, body)
	}
	own, code, body := createSchedule(t, c.s, "secret-a", map[string]any{"role": "a", "kind": "cron", "cron": "0 9 * * *", "tz": "UTC", "prompt": "p"}, "")
	if code != 201 || own.Role != "a" {
		t.Fatalf("role token naming itself %d %s", code, body)
	}
	if n := countMail(t, c.s, "a", "[예약 생김]%"); n != 2 {
		t.Fatalf("own create told the owner: %d", n)
	}
}

// A boss role makes schedules through /api/boss/ (#551) as the admin path
// does, for roles in its view only; a role that is no boss gets 403. A
// session's scheduled run is made for a session in view.
func TestBossScheduleCreateScope(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	s.admin.ProxyToken = "proxy-secret"
	body := func(role string) map[string]any {
		return map[string]any{"role": role, "kind": "cron", "cron": "0 9 * * *", "tz": "UTC", "prompt": "p", "label": "daily"}
	}
	for _, c := range []struct {
		name, token, role, target string
		want                      int
	}{
		{"non-boss", partnerDevToken, partnerDevRole, partnerDevRole, 403},
		{"group boss outside", partnerBossToken, partnerBossRole, partnerBDevRole, 403},
		{"group boss inside", partnerBossToken, partnerBossRole, partnerDevRole, 201},
		{"global boss", bossToken, internalBossRole, "a", 201},
	} {
		w := callAs(s, "POST", "/api/boss/schedules", c.token, c.role, body(c.target))
		if w.Code != c.want {
			t.Fatalf("%s: %d %s, want %d", c.name, w.Code, w.Body.String(), c.want)
		}
		if c.want != 201 {
			continue
		}
		var sc wire.Schedule
		if json.Unmarshal(w.Body.Bytes(), &sc) != nil || sc.Role != c.target {
			t.Fatalf("%s: created %s", c.name, w.Body.String())
		}
		if n := countMail(t, s, c.target, "[예약 생김] daily"); n != 1 {
			t.Fatalf("%s: owner told %d times", c.name, n)
		}
	}

	s.hosting = true
	if _, err := s.db.Exec(`INSERT INTO nodes(name,token_hash,created_at) VALUES('n1','h',1)`); err != nil {
		t.Fatal(err)
	}
	svc := s.hostingService()
	mine, err := svc.Create(wire.HostedSessionCreate{Node: "n1", Base: partnerDevRole, Profile: "p", Directory: "/w/a"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := svc.Create(wire.HostedSessionCreate{Node: "n1", Base: "a", Profile: "p", Directory: "/w/b"})
	if err != nil {
		t.Fatal(err)
	}
	auto := map[string]any{"cron": "*/30 * * * *", "prompt": "check the queue", "label": "queue"}
	if w := callAs(s, "POST", "/api/boss/terminals/"+theirs.ID+"/autos", partnerBossToken, partnerBossRole, auto); w.Code != 403 {
		t.Fatalf("group boss auto on a foreign session %d %s", w.Code, w.Body.String())
	}
	w := callAs(s, "POST", "/api/boss/terminals/"+mine.ID+"/autos", partnerBossToken, partnerBossRole, auto)
	var a wire.HostAuto
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &a) != nil || a.Session != mine.ID || a.Cron != "*/30 * * * *" || a.Label != "queue" {
		t.Fatalf("group boss auto on its session %d %s", w.Code, w.Body.String())
	}
	if w := adminWrite(s, "POST", "/api/admin/terminals/"+theirs.ID+"/autos", `{"cron":"0 9 * * *","prompt":"p"}`); w.Code != 201 {
		t.Fatalf("admin auto %d %s", w.Code, w.Body.String())
	}
}
