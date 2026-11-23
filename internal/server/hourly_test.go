package server

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/cli"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

func reportAt(t *testing.T, s *Server, author string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO reports(author,title,body,created_at) VALUES(?,?,?,?)`, author, "t", "b", at.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}

func questionAt(t *testing.T, s *Server, asker string, at time.Time, seq int) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO questions(id,asker,asker_base,questions,created_at,seq) VALUES(?,?,?,?,?,?)`,
		"q"+strconv.Itoa(seq), asker, asker, "[]", at.UnixMilli(), seq); err != nil {
		t.Fatal(err)
	}
}

// TestMetricsHourly counts mail, reports and questions in clock-hour slots
// over 24 hours and per role over the 60 minutes ending now; system mail and
// anything before the oldest slot are left out.
func TestMetricsHourly(t *testing.T) {
	t.Parallel()
	s, clock := slotServer(t)
	clock.t = time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC)
	now := clock.now()
	day := func(h, m int) time.Time { return time.Date(2026, 11, 16, h, m, 0, 0, time.UTC) }
	today := func(h, m int) time.Time { return time.Date(2026, 11, 17, h, m, 0, 0, time.UTC) }
	mailAt(t, s, flowRow{sender: "team/boss", recipient: "team/worker", at: today(9, 10)})
	mailAt(t, s, flowRow{sender: "team/worker-1", recipient: "team/boss", at: today(8, 45)})
	mailAt(t, s, flowRow{sender: identity.SystemSender, recipient: "team/boss", at: today(9, 20)})
	mailAt(t, s, flowRow{sender: "team/boss", recipient: "team/worker", at: today(8, 20)})
	mailAt(t, s, flowRow{sender: "team/boss", recipient: "team/worker", at: day(10, 0)})
	mailAt(t, s, flowRow{sender: "team/boss", recipient: "team/worker", at: day(9, 59)})
	reportAt(t, s, "team/worker-1", today(9, 0))
	reportAt(t, s, "team/worker-1", today(5, 15))
	questionAt(t, s, "team/worker-1", today(8, 31), 1)
	questionAt(t, s, "team/worker-1", day(9, 0), 2)

	out, err := s.insight().Hourly()
	if err != nil {
		t.Fatal(err)
	}
	if !out.From.Equal(day(10, 0)) || !out.To.Equal(now) || len(out.Hours) != 24 {
		t.Fatalf("window: %v %v %d", out.From, out.To, len(out.Hours))
	}
	want := map[int]wire.HourTotals{0: {Mail: 1}, 19: {Reports: 1}, 22: {Mail: 2, Questions: 1}, 23: {Mail: 1, Reports: 1}}
	for i, h := range out.Hours {
		if !h.Start.Equal(day(10, 0).Add(time.Duration(i) * time.Hour)) {
			t.Fatalf("slot %d starts %v", i, h.Start)
		}
		if h.HourTotals != want[i] {
			t.Fatalf("slot %d (%v): %+v, want %+v", i, h.Start, h.HourTotals, want[i])
		}
	}
	if out.LastHour != (wire.HourTotals{Mail: 2, Reports: 1, Questions: 1}) {
		t.Fatalf("last hour: %+v", out.LastHour)
	}
	wantRoles := []wire.RoleHourly{
		{Role: "team/worker-1", Sent: 1, Reports: 1, Questions: 1},
		{Role: "team/boss", Sent: 1, Received: 1},
		{Role: "team/worker", Received: 1},
	}
	if !reflect.DeepEqual(out.Roles, wantRoles) {
		t.Fatalf("roles: %+v", out.Roles)
	}
	var idx int
	if err = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_messages_created','idx_reports_created')`).Scan(&idx); err != nil || idx != 2 {
		t.Fatalf("indexes: %d %v", idx, err)
	}
}

func TestMetricsHourlyAPI(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.admin.ProxyToken = "proxy-secret"
	if w := call(s, "GET", "/api/metrics/hourly", "secret-a", nil, ""); w.Code != 403 {
		t.Fatalf("non-boss: %d %s", w.Code, w.Body)
	}
	s.bossRoles = map[string]bool{"a": true}
	w := call(s, "GET", "/api/metrics/hourly", "secret-a", nil, "")
	if w.Code != 200 {
		t.Fatalf("boss: %d %s", w.Code, w.Body)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"from", "to", "hours", "last_hour", "roles"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("missing %q: %s", k, w.Body)
		}
	}
	if !strings.Contains(w.Body.String(), `"roles":[]`) || !strings.Contains(w.Body.String(), `"last_hour":{"mail":0,"reports":0,"questions":0}`) {
		t.Fatalf("empty: %s", w.Body)
	}
	if w := call(s, "POST", "/api/metrics/hourly", "secret-a", nil, ""); w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/admin/metrics/hourly", nil)
	r.Header.Set("X-Switchboard-Admin", "proxy-secret")
	aw := httptest.NewRecorder()
	s.api(aw, r)
	if aw.Code != 200 || !strings.Contains(aw.Body.String(), `"hours":[`) {
		t.Fatalf("admin: %d %s", aw.Code, aw.Body)
	}
}

func TestMetricsHourlyCommand(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"b": true} })
	now := time.Now()
	mailAt(t, h.s, flowRow{sender: "a", recipient: "b", at: now.Add(-10 * time.Minute)})
	h.configure(nil, "a")
	if res := h.run(nil, "metrics", "hourly"); res.code == 0 || !strings.Contains(res.stderr, "403") && !strings.Contains(res.stderr, "boss") {
		t.Fatalf("non-boss: %d %s", res.code, res.stderr)
	}
	h.configure(nil, "b")
	out := h.mustRun(nil, "metrics", "hourly")
	for _, want := range []string{"last hour", "1 mail, 0 reports, 0 questions", "role (last hour)", "a ", "b "} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if out = h.mustRun(nil, "metrics", "hourly", "--json"); !strings.Contains(out, `"last_hour":{"mail":1`) {
		t.Fatalf("--json: %s", out)
	}
	for _, bad := range [][]string{{"metrics", "hourly", "--days", "7"}, {"metrics", "daily"}} {
		if res := h.run(nil, bad...); res.code != cli.ExitUsage {
			t.Fatalf("%v: %d %s", bad, res.code, res.stderr)
		}
	}
	if !cli.Known("metrics hourly") {
		t.Fatal("metrics hourly is not a known command")
	}
}
