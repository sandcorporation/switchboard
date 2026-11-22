package server

import (
	"encoding/json"
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
