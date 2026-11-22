package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func (c *scheduleClock) pause(token string, id int64, action string, body any) (wire.Schedule, int, string) {
	c.t.Helper()
	w := call(c.s, "POST", "/api/schedules/"+itoa(id)+"/"+action, token, body, "")
	var sc wire.Schedule
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
			c.t.Fatal(err)
		}
	}
	return sc, w.Code, w.Body.String()
}

func pauseHistory(t *testing.T, s *Server, id int64) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT action, actor, admin, reason, passed FROM schedule_pauses WHERE schedule_id=? ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action, actor, reason, passed string
		var admin int
		if err := rows.Scan(&action, &actor, &admin, &reason, &passed); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join([]string{action, actor, itoa(int64(admin)), reason, passed}, "|"))
	}
	return out
}

// A paused cron schedule sends nothing and counts nothing while paused; on
// resume it fires next at its first time after now, and its expiry moved on
// by the paused time. The owner pausing its own is not told.
func TestSchedulePauseDropsFiresAndHoldsExpiry(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	sc, code, body := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "hourly check", "label": "hourly"}, "")
	if code != 201 {
		t.Fatalf("create %d %s", code, body)
	}
	c.attach("a", 48*time.Hour)
	c.at(30 * time.Minute)
	got, code, body := c.pause("secret-a", sc.ID, "pause", map[string]string{"reason": "deploy freeze"})
	if code != 200 || got.State != wire.SchedulePaused || got.PausedBy == nil || got.PausedBy.Name != "a" || got.PausedBy.Admin || got.PauseReason != "deploy freeze" || got.PausedAt == nil {
		t.Fatalf("pause %d %s", code, body)
	}
	held := got.ExpiresAt // the attached sweep moved it on before the pause
	if again, code, _ := c.pause("secret-a", sc.ID, "pause", nil); code != 200 || again.State != wire.SchedulePaused {
		t.Fatalf("pause again %d", code)
	}
	c.at(3*time.Hour + 30*time.Minute)
	if m := scheduleMail(t, c.s, "a"); len(m) != 0 {
		t.Fatalf("fired while paused: %+v", m)
	}
	if got := c.get(sc.ID); got.Missed != 0 || got.State != wire.SchedulePaused || !got.ExpiresAt.Equal(held) {
		t.Fatalf("while paused %+v", got)
	}
	got, code, body = c.pause("secret-a", sc.ID, "resume", nil)
	if code != 200 || got.State != wire.ScheduleActive || got.PausedBy != nil || got.PauseReason != "" {
		t.Fatalf("resume %d %s", code, body)
	}
	if want := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC); got.NextFireAt == nil || !got.NextFireAt.Equal(want) || got.Missed != 0 {
		t.Fatalf("next after resume %+v", got)
	}
	if want := held.Add(3 * time.Hour); !got.ExpiresAt.Equal(want) {
		t.Fatalf("expiry %v want %v", got.ExpiresAt, want)
	}
	c.at(4*time.Hour + time.Minute)
	if m := scheduleMail(t, c.s, "a"); len(m) != 1 || m[0].Body != "hourly check" {
		t.Fatalf("after resume %+v", m)
	}
	if h := pauseHistory(t, c.s, sc.ID); strings.Join(h, ";") != "pause|a|0|deploy freeze|;resume|a|0||" {
		t.Fatalf("history %v", h)
	}
	if n := countMail(t, c.s, "a", "[예약%"); n != 0 {
		t.Fatalf("the owner was told of its own pause: %d", n)
	}
	// Another role's schedule is as unknown as a missing one; an ended one
	// does not pause.
	if _, code, _ := c.pause("secret-b", sc.ID, "pause", nil); code != 404 {
		t.Fatalf("other role pause %d", code)
	}
	if _, err := c.s.db.Exec(`UPDATE schedules SET state='expired' WHERE id=?`, sc.ID); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := c.pause("secret-a", sc.ID, "pause", nil); code != 409 {
		t.Fatalf("expired pause %d", code)
	}
}

// A once schedule whose time passed while paused is Passed: resume needs
// fire (the prompt goes now) or drop (the schedule ends, nothing sent).
func TestSchedulePauseOncePassed(t *testing.T) {
	t.Parallel()
	c := newScheduleClock(t)
	at := c.now.Add(time.Hour)
	fire, _, _ := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "once", "at": at, "prompt": "wake up", "label": "wake"}, "")
	drop, _, _ := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "once", "at": at, "prompt": "other", "label": "other"}, "")
	later, _, _ := createSchedule(t, c.s, "secret-a", map[string]any{"kind": "once", "at": c.now.Add(5 * time.Hour), "prompt": "later", "label": "later"}, "")
	c.attach("a", 48*time.Hour)
	for _, id := range []int64{fire.ID, drop.ID, later.ID} {
		if _, code, body := c.pause("secret-a", id, "pause", nil); code != 200 {
			t.Fatalf("pause %d %s", code, body)
		}
	}
	c.at(2 * time.Hour)
	if got := c.get(fire.ID); !got.Passed || got.State != wire.SchedulePaused {
		t.Fatalf("passed %+v", got)
	}
	if got := c.get(later.ID); got.Passed {
		t.Fatalf("not passed yet %+v", got)
	}
	if _, code, body := c.pause("secret-a", fire.ID, "resume", nil); code != 409 || !strings.Contains(body, "passed=fire") {
		t.Fatalf("resume without a choice %d %s", code, body)
	}
	if _, code, _ := c.pause("secret-a", fire.ID, "resume", map[string]string{"passed": "later"}); code != 400 {
		t.Fatalf("bad choice %d", code)
	}
	if got, code, body := c.pause("secret-a", fire.ID, "resume", map[string]string{"passed": "fire"}); code != 200 || got.State != wire.ScheduleActive {
		t.Fatalf("fire %d %s", code, body)
	}
	if got, code, body := c.pause("secret-a", drop.ID, "resume", map[string]string{"passed": "drop"}); code != 200 || got.State != wire.ScheduleEnded || got.NextFireAt != nil {
		t.Fatalf("drop %d %s", code, body)
	}
	if got, code, _ := c.pause("secret-a", later.ID, "resume", nil); code != 200 || got.NextFireAt == nil || !got.NextFireAt.Equal(c.now.Add(3*time.Hour)) {
		t.Fatalf("later keeps its time %+v", got)
	}
	c.at(2*time.Hour + time.Minute)
	m := scheduleMail(t, c.s, "a")
	if len(m) != 1 || m[0].Body != "wake up" {
		t.Fatalf("fired once %+v", m)
	}
	if got := c.get(fire.ID); got.State != wire.ScheduleEnded {
		t.Fatalf("fire after delivery %+v", got)
	}
	if h := pauseHistory(t, c.s, drop.ID); len(h) != 2 || h[1] != "resume|a|0||drop" {
		t.Fatalf("drop history %v", h)
	}
}

func countMail(t *testing.T, s *Server, to, subject string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE recipient=? AND subject LIKE ?`, to, subject).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A boss role over the owner (on its token's path and through /api/boss/)
// and the admin pause and resume; each tells the owner once, a normal
// system mail that is no directive. A role outside gets 404 or 403.
func TestSchedulePauseByOthersTellsOwner(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	s.admin.ProxyToken = "proxy-secret"
	w := callAs(s, "POST", "/api/schedules", partnerDevToken, partnerDevRole, map[string]any{"machine": "m1", "kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "p", "label": "hourly"})
	var sc wire.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil || w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	path := "schedules/" + itoa(sc.ID)
	if w := callAs(s, "POST", "/api/"+path+"/pause", "secret-a", "a", map[string]string{}); w.Code != 404 {
		t.Fatalf("outside role %d", w.Code)
	}
	if w := callAs(s, "POST", "/api/boss/"+path+"/pause", partnerBDevToken, partnerBDevRole, map[string]string{}); w.Code != 403 {
		t.Fatalf("non-boss through /api/boss %d", w.Code)
	}
	if w := callAs(s, "POST", "/api/"+path+"/pause", partnerBossToken, partnerBossRole, map[string]string{"reason": "too noisy"}); w.Code != 200 || !strings.Contains(w.Body.String(), `"paused_by":{"name":"`+partnerBossRole+`"}`) {
		t.Fatalf("group boss pause %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "POST", "/api/admin/"+path+"/resume", `{}`); w.Code != 200 {
		t.Fatalf("admin resume %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/boss/"+path+"/pause", bossToken, internalBossRole, map[string]string{}); w.Code != 200 {
		t.Fatalf("global boss pause through /api/boss %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/"+path+"/resume", partnerDevToken, partnerDevRole, map[string]string{}); w.Code != 200 {
		t.Fatalf("owner resume %d %s", w.Code, w.Body)
	}
	rows, err := s.db.Query(`SELECT sender, subject, body, priority FROM messages WHERE recipient=? AND subject LIKE '[예약%' ORDER BY id`, partnerDevRole)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var sender, subject, body, priority string
		if err := rows.Scan(&sender, &subject, &body, &priority); err != nil {
			t.Fatal(err)
		}
		if priority != "normal" || !strings.Contains(sender, "system") {
			t.Fatalf("notice %s %s %s", sender, priority, subject)
		}
		got = append(got, subject+" / "+body)
	}
	if len(got) != 3 || !strings.Contains(got[0], "[예약 멈춤] hourly") || !strings.Contains(got[0], "역할 "+partnerBossRole) || !strings.Contains(got[0], "까닭: too noisy") ||
		!strings.Contains(got[1], "[예약 켬] hourly") || !strings.Contains(got[1], "관리자 ") ||
		!strings.Contains(got[2], "역할 "+internalBossRole) {
		t.Fatalf("notices (the owner's own resume sends none):\n%s", strings.Join(got, "\n"))
	}
	if h := pauseHistory(t, s, sc.ID); len(h) != 4 || !strings.HasPrefix(h[1], "resume|") || !strings.HasSuffix(h[1], "|1||") {
		t.Fatalf("history %v", h)
	}
}
