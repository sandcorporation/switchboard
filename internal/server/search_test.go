package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// A role's search shows its own service only; a global boss and a signed-in
// admin see everything (#481).
func TestSearchScopesByGroup(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t) // mail a-b (a → b), pa (partner-a/dev → partner-a/boss), pb (partner-b/dev → global boss)
	putGroup(t, s, "partner-b", "")
	search := func(token, role, query string) (int, wire.SearchResult) {
		w := callAs(s, "GET", "/api/search?"+query, token, role, nil)
		var res wire.SearchResult
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("%v %s", err, w.Body)
			}
		}
		return w.Code, res
	}
	titles := func(res wire.SearchResult) []string {
		var out []string
		for _, h := range res.Hits {
			out = append(out, h.Title)
		}
		return out
	}
	for _, c := range []struct {
		name, token, role, want, scope string
	}{
		{"global boss", bossToken, internalBossRole, "[pb pa a-b]", "all"},
		{"group role", partnerDevToken, partnerDevRole, "[pa]", "partner-a"},
		{"other group", partnerBDevToken, partnerBDevRole, "[pb]", "partner-b"},
	} {
		code, res := search(c.token, c.role, "q=X")
		if code != 200 || fmt.Sprint(titles(res)) != c.want || res.Scope != c.scope {
			t.Errorf("%s: %d %v scope %q, want %s %q", c.name, code, titles(res), res.Scope, c.want, c.scope)
		}
	}
	w := adminRequest(s, "GET", "/api/admin/search?q=x&kind=mail", "", nil)
	var res wire.SearchResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &res) != nil || len(res.Hits) != 3 || res.Scope != "all" {
		t.Fatalf("admin: %d %s", w.Code, w.Body)
	}
	if code, _ := search(bossToken, internalBossRole, "q=(&regex=1"); code != 400 {
		t.Fatalf("bad regexp: %d", code)
	}
	if code, _ := search(bossToken, internalBossRole, "q="); code != 400 {
		t.Fatalf("empty pattern: %d", code)
	}
	if w := callAs(s, "POST", "/api/search?q=x", bossToken, internalBossRole, nil); w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
	if w := callAs(s, "GET", "/api/search?q=x", "wrong", partnerDevRole, nil); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
}
