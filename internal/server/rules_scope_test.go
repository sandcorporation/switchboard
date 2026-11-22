package server

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// scopeServer is groupServer (global boss sandcorp/boss, internal
// team/worker, partner-a/boss, partner-a/dev, partner-b/dev) with the
// groups partner-a and partner-b and the dashboard's admin proxy.
func scopeServer(t *testing.T) *Server {
	t.Helper()
	s := groupServer(t)
	putGroup(t, s, "partner-a", "")
	putGroup(t, s, "partner-b", "")
	return s
}

func setServiceTags(t *testing.T, s *Server, svc string, tags ...string) {
	t.Helper()
	if tags == nil {
		tags = []string{}
	}
	w := callAs(s, "PUT", "/api/rules/tags/services", bossToken, internalBossRole, map[string]any{"service": svc, "tags": tags})
	if w.Code != 200 {
		t.Fatalf("set tags %s: %d %s", svc, w.Code, w.Body)
	}
}

func changeRoleTags(t *testing.T, s *Server, change wire.RoleTagChange) wire.RoleTags {
	t.Helper()
	w := callAs(s, "POST", "/api/rules/tags/roles", bossToken, internalBossRole, change)
	if w.Code != 200 {
		t.Fatalf("role tags %+v: %d %s", change, w.Code, w.Body)
	}
	var rt wire.RoleTags
	_ = json.Unmarshal(w.Body.Bytes(), &rt)
	return rt
}

// TestRuleReadScope: a role that is not an admin or a global boss reads only
// the rows in force of its own group that reach it (all, its service, its
// tags), as number, kind, text and topic; groups are apart, a group's boss
// included.
func TestRuleReadScope(t *testing.T) {
	s := scopeServer(t)
	nt := map[string]any{"new_topic": true}
	all := addRule(t, s, ruleBody("decision", "all", "release", "ship on green", nt))
	team := addRule(t, s, ruleBody("decision", "service:team", "review", "two reviewers", nt))
	other := addRule(t, s, ruleBody("tip", "service:other", "review", "one reviewer", nil))
	tagged := addRule(t, s, ruleBody("tip", "tag:ci-tips", "ci", "rerun once", nt))
	paAll := addRule(t, s, ruleBody("decision", "all", "release", "ask first", map[string]any{"group": "partner-a"}))
	pa := addRule(t, s, ruleBody("tip", "service:partner-a", "review", "write tests", nil))
	pb := addRule(t, s, ruleBody("tip", "service:partner-b", "review", "keep it short", nil))
	if paAll.Group != "partner-a" || pa.Group != "partner-a" || pb.Group != "partner-b" || all.Group != "" {
		t.Fatalf("groups: %q %q %q %q", paAll.Group, pa.Group, pb.Group, all.Group)
	}
	addRuleAs(t, s, bossToken, internalBossRole, ruleBody("tip", "all", "ci", "x", map[string]any{"group": "nope"}), 400)
	addRuleAs(t, s, bossToken, internalBossRole, ruleBody("tip", "service:partner-a,team", "ci", "x", map[string]any{"alongside": true}), 400)

	sees := func(token, role string, want ...int64) {
		t.Helper()
		l := listRulesAs(t, s, token, role, "")
		slices.Sort(want)
		if got := ruleIDs(l.Rules); !slices.Equal(got, want) || l.Full {
			t.Fatalf("%s sees %v (full %v), want %v", role, got, l.Full, want)
		}
		for _, r := range l.Rules {
			if r.Scope != nil || r.Status != "" || r.Author != "" || r.Group != "" || r.Sources != nil || r.SourceText != "" || r.CreatedAt != nil {
				t.Fatalf("%s gets more than number, kind, text and topic: %+v", role, r)
			}
		}
	}
	sees(workerToken, internalWorkerRol, all.ID, team.ID)
	sees(partnerDevToken, partnerDevRole, paAll.ID, pa.ID)
	sees(partnerBossToken, partnerBossRole, paAll.ID, pa.ID)
	sees(partnerBDevToken, partnerBDevRole, pb.ID)

	// Tags: the service's, plus the role's additions, minus its removals.
	setServiceTags(t, s, "team", "ci-tips")
	sees(workerToken, internalWorkerRol, all.ID, team.ID, tagged.ID)
	rt := changeRoleTags(t, s, wire.RoleTagChange{Role: internalWorkerRol, Remove: []string{"ci-tips"}})
	if !slices.Equal(rt.Remove, []string{"ci-tips"}) || len(rt.Tags) != 0 {
		t.Fatalf("after remove: %+v", rt)
	}
	sees(workerToken, internalWorkerRol, all.ID, team.ID)
	changeRoleTags(t, s, wire.RoleTagChange{Role: internalWorkerRol, Inherit: []string{"ci-tips"}})
	sees(workerToken, internalWorkerRol, all.ID, team.ID, tagged.ID)
	setServiceTags(t, s, "team")
	changeRoleTags(t, s, wire.RoleTagChange{Role: partnerBDevRole, Add: []string{"ci-tips"}})
	sees(workerToken, internalWorkerRol, all.ID, team.ID)
	// A tag does not cross groups: the tagged row is in no group.
	sees(partnerBDevToken, partnerBDevRole, pb.ID)

	// Rows out of scope are not found; history and all rows are for full readers.
	showRule(t, s, workerToken, internalWorkerRol, other.ID, 404)
	showRule(t, s, partnerBossToken, partnerBossRole, pb.ID, 404)
	if w := callAs(s, "GET", "/api/rules?all=1", workerToken, internalWorkerRol, nil); w.Code != 403 {
		t.Fatalf("all=1 as a worker: %d", w.Code)
	}
	w := callAs(s, "GET", "/api/rules/topics", workerToken, internalWorkerRol, nil)
	var tp wire.RuleTopics
	_ = json.Unmarshal(w.Body.Bytes(), &tp)
	if len(tp.Topics) != 2 || tp.Topics[0].Name != "release" || tp.Topics[1].Name != "review" {
		t.Fatalf("worker topics = %+v", tp)
	}

	// Writes and tags are an admin's or a global boss's, not a group boss's.
	addRuleAs(t, s, partnerBossToken, partnerBossRole, ruleBody("tip", "service:partner-a", "review", "x", map[string]any{"alongside": true}), 403)
	for _, c := range []struct{ method, path string }{{"GET", "/api/rules/tags"}, {"PUT", "/api/rules/tags/services"}, {"POST", "/api/rules/tags/roles"}} {
		if w := callAs(s, c.method, c.path, partnerBossToken, partnerBossRole, map[string]any{"service": "partner-a", "tags": []string{"x"}}); w.Code != 403 {
			t.Fatalf("%s %s as a group boss: %d", c.method, c.path, w.Code)
		}
	}

	// A full reader filters by service: what that service's roles read.
	setServiceTags(t, s, "team", "ci-tips")
	if got := ruleIDs(listRulesAs(t, s, bossToken, internalBossRole, "?service=team").Rules); !slices.Equal(got, []int64{all.ID, team.ID, tagged.ID}) {
		t.Fatalf("service=team: %v", got)
	}
	if got := ruleIDs(listRulesAs(t, s, bossToken, internalBossRole, "?service=partner-a").Rules); !slices.Equal(got, []int64{paAll.ID, pa.ID}) {
		t.Fatalf("service=partner-a: %v", got)
	}
	var tags wire.RuleTags
	w = adminWrite(s, "GET", "/api/admin/rules/tags", "")
	_ = json.Unmarshal(w.Body.Bytes(), &tags)
	if w.Code != 200 || len(tags.Services) != 1 || len(tags.Roles) != 1 || tags.Roles[0].Role != partnerBDevRole {
		t.Fatalf("tags = %d %+v", w.Code, tags)
	}
}
