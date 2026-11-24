package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// An edit keeps the schedule's ID and fire record and moves its next fire to
// the new time (#516).
func TestScheduleEditKeepsIDAndRecord(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	sc, code, body := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "cron", "cron": "*/10 * * * *", "tz": "UTC", "prompt": "check the build", "label": "build"}, "")
	if code != 201 {
		t.Fatalf("create %d %s", code, body)
	}
	c.attach("a", 24*time.Hour)
	c.at(10 * time.Minute)
	fired := c.get(sc.ID)
	if fired.LastFireAt == nil {
		t.Fatalf("no fire before the edit %+v", fired)
	}

	w := call(c.s, "PUT", "/api/schedules/"+itoa(sc.ID), "secret-a", map[string]any{"kind": "cron", "cron": "0 12 * * *", "tz": "UTC", "prompt": "check the deploy"}, "")
	var got wire.Schedule
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("edit %d %s", w.Code, w.Body.String())
	}
	next := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if got.ID != sc.ID || got.Cron != "0 12 * * *" || got.Prompt != "check the deploy" || got.Label != "build" ||
		got.NextFireAt == nil || !got.NextFireAt.Equal(next) || got.LastFireAt == nil || !got.LastFireAt.Equal(*fired.LastFireAt) ||
		!got.CreatedAt.Equal(sc.CreatedAt) || !got.ExpiresAt.Equal(fired.ExpiresAt) {
		t.Fatalf("edited %+v (before %+v)", got, fired)
	}
	// The old time no longer fires; the new one does, as the same schedule.
	for _, m := range scheduleMail(t, c.s, "a") {
		markRead(t, c.s, "a", m.ID)
	}
	c.at(20 * time.Minute)
	if m := scheduleMail(t, c.s, "a"); len(m) != 1 {
		t.Fatalf("the old time fired after the edit: %+v", m)
	}
	c.at(3 * time.Hour)
	m := scheduleMail(t, c.s, "a")
	if len(m) != 2 || m[1].Body != "check the deploy" || m[1].Subject != "[주기] build" {
		t.Fatalf("new time's fire %+v", m)
	}
	if after := c.get(sc.ID); after.ID != sc.ID || after.LastFireAt == nil || !after.LastFireAt.Equal(next) {
		t.Fatalf("after the new fire %+v", after)
	}
}

// A cron schedule edited to once fires once at the new time; the label goes
// back to 예약 #ID when cleared.
func TestScheduleEditToOnceAndLabel(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	sc, _, _ := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "p", "label": "hourly"}, "")
	at := c.now.Add(90 * time.Minute)
	w := call(c.s, "PUT", "/api/schedules/"+itoa(sc.ID), "secret-a", map[string]any{"kind": "once", "at": at, "label": ""}, "")
	var got wire.Schedule
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("edit %d %s", w.Code, w.Body.String())
	}
	if got.Kind != wire.ScheduleOnce || got.Cron != "" || got.TZ != "UTC" || got.NextFireAt == nil || !got.NextFireAt.Equal(at) || got.Label != "예약 #"+itoa(sc.ID) {
		t.Fatalf("edited %+v", got)
	}
}

func TestScheduleEditRefusals(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	sc, _, _ := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "p"}, "")
	path := "/api/schedules/" + itoa(sc.ID)
	for _, tc := range []struct {
		name, token string
		body        map[string]any
		code        int
	}{
		{"nothing", "secret-a", map[string]any{}, 400},
		{"kind without time", "secret-a", map[string]any{"kind": "cron"}, 400},
		{"bad cron", "secret-a", map[string]any{"kind": "cron", "cron": "nope"}, 400},
		{"past", "secret-a", map[string]any{"kind": "once", "at": c.now.Add(-time.Hour)}, 400},
		{"after expiry", "secret-a", map[string]any{"kind": "once", "at": c.now.Add(30 * 24 * time.Hour)}, 400},
		{"empty prompt", "secret-a", map[string]any{"prompt": " "}, 400},
		{"unknown field", "secret-a", map[string]any{"machine": "x"}, 400},
		{"another role", "secret-b", map[string]any{"prompt": "mine now"}, 404},
	} {
		if w := call(c.s, "PUT", path, tc.token, tc.body, ""); w.Code != tc.code {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
	if got := c.get(sc.ID); got.Prompt != "p" || got.Cron != "0 * * * *" {
		t.Fatalf("a refused edit changed %+v", got)
	}
	if _, err := c.s.db.Exec(`UPDATE schedules SET state=? WHERE id=?`, wire.ScheduleEnded, sc.ID); err != nil {
		t.Fatal(err)
	}
	if w := call(c.s, "PUT", path, "secret-a", map[string]any{"prompt": "late"}, ""); w.Code != 409 {
		t.Fatalf("ended: %d %s", w.Code, w.Body.String())
	}
}

// The dashboard edits a schedule through the admin path; a paused one stays
// paused, and the owner is told.
func TestAdminScheduleEdit(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	c.s.admin.ProxyToken = "proxy-secret"
	sc, _, _ := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "p", "label": "hourly"}, "")
	if w := adminWrite(c.s, "POST", "/api/admin/schedules/"+itoa(sc.ID)+"/pause", `{}`); w.Code != 200 {
		t.Fatalf("pause %d %s", w.Code, w.Body.String())
	}
	body := `{"kind":"cron","cron":"15 * * * *","tz":"UTC","prompt":"q","label":"quarter"}`
	if w := adminRequest(c.s, "PUT", "/api/admin/schedules/"+itoa(sc.ID), body, nil); w.Code != 403 {
		t.Fatalf("admin edit without the write guard %d", w.Code)
	}
	w := adminWrite(c.s, "PUT", "/api/admin/schedules/"+itoa(sc.ID), body)
	var got wire.Schedule
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("admin edit %d %s", w.Code, w.Body.String())
	}
	if got.ID != sc.ID || got.State != wire.SchedulePaused || got.Cron != "15 * * * *" || got.Label != "quarter" || got.NextFireAt == nil || !got.NextFireAt.Equal(c.now.Add(15*time.Minute)) {
		t.Fatalf("admin edited %+v", got)
	}
	var told int
	if err := c.s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE recipient='a' AND subject=?`, "[예약 고침] hourly").Scan(&told); err != nil || told != 1 {
		t.Fatalf("owner told %d times %v", told, err)
	}
}
