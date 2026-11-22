package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/jobs"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// The three paths to the jobs tab — the dashboard admin, a boss role's
// token and the owner's own token — go through one read model (jobs.View,
// #404) and agree: the admin and a global boss see the same rows, a group
// boss the admin's rows of its group, the owner the admin's rows of its own
// (with the body, which no list for anyone else carries).
func TestLedgerPathsAgree(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	s.admin.ProxyToken = "proxy-secret"
	for _, o := range []struct{ token, role string }{{partnerDevToken, partnerDevRole}, {"secret-a", "a"}} {
		if w := callAs(s, "POST", "/api/jobs", o.token, o.role, map[string]any{"machine": "m1", "label": "build", "command": "make secret-cmd", "cwd": "/w"}); w.Code != 201 {
			t.Fatalf("job %s: %d %s", o.role, w.Code, w.Body)
		}
		if w := callAs(s, "POST", "/api/schedules", o.token, o.role, map[string]any{"machine": "m1", "kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "secret prompt", "label": "hourly"}); w.Code != 201 {
			t.Fatalf("schedule %s: %d %s", o.role, w.Code, w.Body)
		}
	}
	decode := func(code int, body []byte, out any) {
		t.Helper()
		if code != 200 {
			t.Fatalf("%d %s", code, body)
		}
		if err := json.Unmarshal(body, out); err != nil {
			t.Fatal(err)
		}
	}
	var admin jobs.Ledger
	w := adminRequest(s, "GET", "/api/admin/jobs", "", nil)
	decode(w.Code, w.Body.Bytes(), &admin.Jobs)
	w = adminRequest(s, "GET", "/api/admin/schedules", "", nil)
	decode(w.Code, w.Body.Bytes(), &admin.Schedules)
	if len(admin.Jobs) != 2 || len(admin.Schedules) != 2 {
		t.Fatalf("admin %+v", admin)
	}
	for _, raw := range []string{"secret-cmd", "secret prompt"} {
		if b, _ := json.Marshal(admin); strings.Contains(string(b), raw) {
			t.Fatalf("admin list carries %q", raw)
		}
	}
	var global, group jobs.Ledger
	bossGet(t, s, bossToken, internalBossRole, "jobs", &global)
	bossGet(t, s, partnerBossToken, partnerBossRole, "jobs", &group)
	if !reflect.DeepEqual(global, admin) {
		t.Fatalf("global boss %+v\nadmin %+v", global, admin)
	}
	if want := ofRole(admin, partnerDevRole); !reflect.DeepEqual(group, want) {
		t.Fatalf("group boss %+v\nwant %+v", group, want)
	}

	var own jobs.Ledger
	w = callAs(s, "GET", "/api/jobs", partnerDevToken, partnerDevRole, nil)
	decode(w.Code, w.Body.Bytes(), &own.Jobs)
	w = callAs(s, "GET", "/api/schedules", partnerDevToken, partnerDevRole, nil)
	decode(w.Code, w.Body.Bytes(), &own.Schedules)
	if len(own.Jobs) != 1 || own.Jobs[0].Command != "make secret-cmd" || len(own.Schedules) != 1 || own.Schedules[0].Prompt != "secret prompt" {
		t.Fatalf("owner %+v", own)
	}
	own.Jobs[0].Command, own.Schedules[0].Prompt = "", ""
	if want := ofRole(admin, partnerDevRole); !reflect.DeepEqual(own, want) {
		t.Fatalf("owner %+v\nwant %+v", own, want)
	}
}

func ofRole(l jobs.Ledger, role string) jobs.Ledger {
	out := jobs.Ledger{Jobs: []wire.Job{}, Schedules: []wire.Schedule{}}
	for _, j := range l.Jobs {
		if j.Role == role {
			out.Jobs = append(out.Jobs, j)
		}
	}
	for _, sc := range l.Schedules {
		if sc.Role == role {
			out.Schedules = append(out.Schedules, sc)
		}
	}
	return out
}

// One job or schedule opens with its command or prompt for the owner, a
// boss role over it (on its own token's paths and through /api/boss/) and
// the admin; each read by anyone but the owner is recorded without the
// text. A boss role's list of another role leaves the text out.
func TestLedgerShowRecordsForeignReads(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	s.admin.ProxyToken = "proxy-secret"
	w := callAs(s, "POST", "/api/jobs", partnerDevToken, partnerDevRole, map[string]any{"machine": "m1", "label": "build", "command": "make secret-cmd", "cwd": "/w"})
	var j wire.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil || w.Code != 201 {
		t.Fatalf("job: %d %s", w.Code, w.Body)
	}
	w = callAs(s, "POST", "/api/schedules", partnerDevToken, partnerDevRole, map[string]any{"machine": "m1", "kind": "cron", "cron": "0 * * * *", "tz": "UTC", "prompt": "secret prompt", "label": "hourly"})
	var sc wire.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil || w.Code != 201 {
		t.Fatalf("schedule: %d %s", w.Code, w.Body)
	}
	views := func() []string {
		t.Helper()
		rows, err := s.db.Query(`SELECT viewer, admin, kind, item_id, owner FROM ledger_views ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var viewer, kind, owner string
			var admin, id int64
			if err := rows.Scan(&viewer, &admin, &kind, &id, &owner); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("%s/%d %s#%d %s", viewer, admin, kind, id, owner))
		}
		return out
	}
	jp, sp := "jobs/"+itoa(j.ID), "schedules/"+itoa(sc.ID)
	for _, c := range []struct {
		name string
		do   func(path string) *httptest.ResponseRecorder
		code int
	}{
		{"owner", func(p string) *httptest.ResponseRecorder {
			return callAs(s, "GET", "/api/"+p, partnerDevToken, partnerDevRole, nil)
		}, 200},
		{"other role", func(p string) *httptest.ResponseRecorder { return callAs(s, "GET", "/api/"+p, "secret-a", "a", nil) }, 404},
		{"another group's boss", func(p string) *httptest.ResponseRecorder {
			return callAs(s, "GET", "/api/"+p, partnerBDevToken, partnerBDevRole, nil)
		}, 404},
		{"group boss", func(p string) *httptest.ResponseRecorder {
			return callAs(s, "GET", "/api/"+p, partnerBossToken, partnerBossRole, nil)
		}, 200},
		{"global boss", func(p string) *httptest.ResponseRecorder {
			return callAs(s, "GET", "/api/boss/"+p, bossToken, internalBossRole, nil)
		}, 200},
		{"admin", func(p string) *httptest.ResponseRecorder { return adminRequest(s, "GET", "/api/admin/"+p, "", nil) }, 200},
	} {
		for _, p := range []string{jp, sp} {
			w := c.do(p)
			if w.Code != c.code {
				t.Fatalf("%s %s: %d %s", c.name, p, w.Code, w.Body)
			}
			if c.code == 200 && !strings.Contains(w.Body.String(), "secret-cmd") && !strings.Contains(w.Body.String(), "secret prompt") {
				t.Fatalf("%s %s: no text %s", c.name, p, w.Body)
			}
		}
	}
	got := strings.Join(views(), "\n")
	for _, want := range []string{
		partnerBossRole + "/0 job#" + itoa(j.ID) + " " + partnerDevRole,
		partnerBossRole + "/0 schedule#" + itoa(sc.ID) + " " + partnerDevRole,
		internalBossRole + "/0 job#",
		"/1 job#", "/1 schedule#",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("views missing %q:\n%s", want, got)
		}
	}
	if n := len(views()); n != 6 || strings.Contains(got, partnerDevRole+"/0") {
		t.Fatalf("views %d (the owner's reads are not views):\n%s", n, got)
	}
	if w := callAs(s, "GET", "/api/boss/"+jp, partnerBDevToken, partnerBDevRole, nil); w.Code == 200 {
		t.Fatalf("non-boss through /api/boss: %d", w.Code)
	}

	for _, p := range []string{"jobs", "schedules"} {
		w := callAs(s, "GET", "/api/"+p+"?role="+partnerDevRole, partnerBossToken, partnerBossRole, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), partnerDevRole) {
			t.Fatalf("boss list of %s: %d %s", p, w.Code, w.Body)
		}
	}
}
