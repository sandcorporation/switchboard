package server

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/jobs"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// bossGet is GET /api/boss/<view> as role.
func bossGet(t *testing.T, s *Server, token, role, view string, out any) int {
	t.Helper()
	w := callAs(s, "GET", "/api/boss/"+view, token, role, nil)
	if w.Code == 200 && out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v %s", view, err, w.Body)
		}
	}
	return w.Code
}

func bossMailServer(t *testing.T) (*Server, map[string]int64) {
	t.Helper()
	s := groupServer(t)
	putGroup(t, s, "partner-a", "")
	ids := map[string]int64{}
	for _, m := range []struct{ name, token, role, to string }{
		{"a-b", "secret-a", "a", "b"},
		{"pa", partnerDevToken, partnerDevRole, partnerBossRole},
		{"pb", partnerBDevToken, partnerBDevRole, internalBossRole},
	} {
		w := callAs(s, "POST", "/api/messages", m.token, m.role, map[string]string{"recipient": m.to, "subject": m.name, "body": "x"})
		if w.Code != 201 {
			t.Fatalf("send %s: %d %s", m.name, w.Code, w.Body)
		}
		var sent wire.Message
		_ = json.Unmarshal(w.Body.Bytes(), &sent)
		ids[m.name] = sent.ID
	}
	return s, ids
}

func subjects(ms []wire.Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Subject)
	}
	slices.Sort(out)
	return out
}

// Every boss view is 403 for a role that is no boss; a group boss sees the
// mail of its group only, a global boss all of it, and nothing is marked
// read.
func TestBossViewsScopeAndRefuseNonBoss(t *testing.T) {
	t.Parallel()
	s, ids := bossMailServer(t)
	for _, view := range []string{"messages", "messages/" + strconv.FormatInt(ids["a-b"], 10), "jobs", "roles", "settings", "terminals", "nodes", "autos"} {
		if code := bossGet(t, s, "secret-a", "a", view, nil); code != 403 {
			t.Fatalf("non-boss %s: %d", view, code)
		}
		if code := bossGet(t, s, partnerDevToken, partnerDevRole, view, nil); code != 403 {
			t.Fatalf("group role %s: %d", view, code)
		}
	}
	if w := callAs(s, "POST", "/api/boss/messages", bossToken, internalBossRole, nil); w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}

	var all, group []wire.Message
	if code := bossGet(t, s, bossToken, internalBossRole, "messages", &all); code != 200 || !slices.Equal(subjects(all), []string{"a-b", "pa", "pb"}) {
		t.Fatalf("global boss: %d %v", code, subjects(all))
	}
	if code := bossGet(t, s, partnerBossToken, partnerBossRole, "messages", &group); code != 200 || !slices.Equal(subjects(group), []string{"pa"}) {
		t.Fatalf("group boss: %d %v", code, subjects(group))
	}
	var filtered []wire.Message
	bossGet(t, s, bossToken, internalBossRole, "messages?from=a&open=1&limit=5&since=2000-01-01T00:00:00Z", &filtered)
	if !slices.Equal(subjects(filtered), []string{"a-b"}) {
		t.Fatalf("filtered: %v", subjects(filtered))
	}
	for _, q := range []string{"limit=0", "limit=x", "since=yesterday"} {
		if code := bossGet(t, s, bossToken, internalBossRole, "messages?"+q, nil); code != 400 {
			t.Fatalf("%s: %d", q, code)
		}
	}

	// Showing another role's mail leaves its state and records the view;
	// a group boss gets 404 for mail outside its groups.
	ab := "messages/" + strconv.FormatInt(ids["a-b"], 10)
	if code := bossGet(t, s, partnerBossToken, partnerBossRole, ab, nil); code != 404 {
		t.Fatalf("group boss foreign show: %d", code)
	}
	var m wire.Message
	if code := bossGet(t, s, bossToken, internalBossRole, ab, &m); code != 200 || m.Subject != "a-b" || m.Status != "pending" || !m.ViewOnly {
		t.Fatalf("global boss show: %d %+v", code, m)
	}
	var views int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM mailbox_views WHERE viewer=? AND message_id=?`, internalBossRole, ids["a-b"]).Scan(&views)
	if views != 1 {
		t.Fatalf("views recorded: %d", views)
	}
	var status string
	_ = s.db.QueryRow(`SELECT COALESCE((SELECT status FROM role_states WHERE message_id=?),'pending')`, ids["a-b"]).Scan(&status)
	if status != "pending" {
		t.Fatalf("state after show: %s", status)
	}
}

// Roles, settings: a group boss sees its group only and no server summary;
// the settings name the secret settings without their values.
func TestBossRolesAndSettings(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	var global, group bossRoles
	bossGet(t, s, bossToken, internalBossRole, "roles", &global)
	if code := bossGet(t, s, partnerBossToken, partnerBossRole, "roles", &group); code != 200 {
		t.Fatalf("group roles: %d", code)
	}
	names := func(v bossRoles) (out []string) {
		for _, r := range v.Roles {
			out = append(out, r.Role)
		}
		return out
	}
	if !slices.Equal(names(group), []string{partnerBossRole, partnerDevRole}) || len(group.Groups) != 1 || group.Groups[0].Prefix != "partner-a" {
		t.Fatalf("group boss roles: %v groups %+v", names(group), group.Groups)
	}
	if !slices.Contains(names(global), "a") || !slices.Contains(names(global), partnerBDevRole) {
		t.Fatalf("global boss roles: %v", names(global))
	}

	w := callAs(s, "GET", "/api/boss/settings", bossToken, internalBossRole, nil)
	if w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	if body := w.Body.String(); strings.Contains(body, s.admin.ProxyToken) {
		t.Fatalf("settings carry the proxy secret: %s", body)
	}
	var st bossSettings
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st.Server == nil || st.AdminAuth == "" || len(st.QuestionSettings) == 0 {
		t.Fatalf("global settings: %+v", st)
	}
	set := map[string]bool{}
	for _, v := range st.Secrets {
		set[strings.TrimPrefix(v.Name, "SWITCHBOARD_")] = v.Set
	}
	if !set["ADMIN_PROXY_TOKEN"] || set["DISCORD_WEBHOOK"] || len(set) != 3 {
		t.Fatalf("secrets: %+v", st.Secrets)
	}
	var gs bossSettings
	w = callAs(s, "GET", "/api/boss/settings", partnerBossToken, partnerBossRole, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &gs)
	if w.Code != 200 || gs.Server != nil || gs.Secrets != nil || strings.Contains(w.Body.String(), `"server"`) {
		t.Fatalf("group settings: %d %s", w.Code, w.Body)
	}
}

// Jobs: a group boss sees the jobs of its group; the command is left out.
// Terminals: 404 while hosting is off; with it on, the records in view (as
// the dashboard shows them), their nodes and scheduled runs.
func TestBossJobsAndTerminals(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	for _, j := range []struct{ token, role string }{{partnerDevToken, partnerDevRole}, {"secret-a", "a"}} {
		w := callAs(s, "POST", "/api/jobs", j.token, j.role, map[string]any{"machine": "m1", "kind": "command", "label": "build", "command": "make secret-cmd", "cwd": "/w"})
		if w.Code != 201 && w.Code != 200 {
			t.Fatalf("create job %s: %d %s", j.role, w.Code, w.Body)
		}
	}
	var group, global jobs.Ledger
	bossGet(t, s, partnerBossToken, partnerBossRole, "jobs", &group)
	bossGet(t, s, bossToken, internalBossRole, "jobs", &global)
	if len(group.Jobs) != 1 || group.Jobs[0].Role != partnerDevRole || group.Jobs[0].Command != "" || len(global.Jobs) != 2 {
		t.Fatalf("jobs group %+v global %d", group.Jobs, len(global.Jobs))
	}

	if code := bossGet(t, s, bossToken, internalBossRole, "terminals", nil); code != 404 {
		t.Fatalf("hosting off: %d", code)
	}
	s.hosting = true
	if _, err := s.db.Exec(`INSERT INTO nodes(name,token_hash,created_at) VALUES('n1','h',1),('n2','h',1)`); err != nil {
		t.Fatal(err)
	}
	svc := s.hostingService()
	for _, c := range []wire.HostedSessionCreate{
		{Node: "n1", Base: partnerDevRole, Profile: "p", Directory: "/w/a", Env: map[string]string{"MODE": "fast"}},
		{Node: "n2", Base: "a", Profile: "p", Directory: "/w/b"},
	} {
		h, err := svc.Create(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateAuto(h.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
	}
	w := callAs(s, "GET", "/api/boss/terminals", partnerBossToken, partnerBossRole, nil)
	if w.Code != 200 {
		t.Fatalf("terminals: %d %s", w.Code, w.Body)
	}
	var recs []wire.HostedSession
	_ = json.Unmarshal(w.Body.Bytes(), &recs)
	if len(recs) != 1 || recs[0].Base != partnerDevRole {
		t.Fatalf("group terminals: %+v", recs)
	}
	var autos []wire.HostAuto
	if bossGet(t, s, partnerBossToken, partnerBossRole, "autos", &autos); len(autos) != 1 || autos[0].Session != recs[0].ID {
		t.Fatalf("group autos: %+v", autos)
	}
	var all []wire.HostAuto
	if bossGet(t, s, bossToken, internalBossRole, "autos", &all); len(all) != 2 {
		t.Fatalf("global autos: %d", len(all))
	}
	var nodes []wire.NodeView
	if bossGet(t, s, partnerBossToken, partnerBossRole, "nodes", &nodes); len(nodes) != 1 || nodes[0].Name != "n1" {
		t.Fatalf("group nodes: %+v", nodes)
	}
	if bossGet(t, s, bossToken, internalBossRole, "nodes", &nodes); len(nodes) != 2 {
		t.Fatalf("global nodes: %+v", nodes)
	}
}
