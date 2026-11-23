package server

import (
	"net/http"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/search"
)

// search is the text search over this server's database (#481).
func (s *Server) search() *search.Service { return search.New(s.db) }

// searchView is what a role's search may show and the scope it reports. A
// global boss sees everything. Any other role sees the roles identity lets
// it see (MaySee: its own group, or everything for a role in no group, as
// for its mail and questions) and, as a group boss, the groups it answers
// for (BossScope). Slot and label addresses count for their base.
func (s *Server) searchView(p identity.Principal) (search.See, string) {
	ids := s.ids()
	if ids.IsGlobalBoss(p) || ids.Unrestricted(p) {
		return nil, ""
	}
	boss, isBoss := ids.BossScope(p)
	see := func(role string) bool {
		base := search.BaseOf(role)
		return ids.MaySee(p, base) || isBoss && boss(base)
	}
	return see, ids.ScopeGroup(p)
}

// searchAPI serves GET /api/search for a role.
func (s *Server) searchAPI(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	see, scope := s.searchView(p)
	s.search().API(w, r, see, scope)
}
