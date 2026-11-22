package hosting

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// AdminGrantsAPI serves /api/admin/host-grants for an identified admin past
// the write guard: GET lists the grants, POST sets one (wire.HostGrant;
// replaces the role's), DELETE ?role=ROLE removes one.
func (s *Service) AdminGrantsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, err := s.Grants()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		g, ok := decode[wire.HostGrant](w, r)
		if !ok {
			return
		}
		g, err := s.SetGrant(g)
		if err == nil {
			s.recordAction(r, wire.ActionGrantSet, g.Role)
		}
		writeGrant(w, http.StatusOK)(g, err)
	case http.MethodDelete:
		g, err := s.RevokeGrant(r.URL.Query().Get("role"))
		if err == nil {
			s.recordAction(r, wire.ActionGrantRevoke, g.Role)
		}
		writeGrant(w, http.StatusOK)(g, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeGrant(w http.ResponseWriter, ok int) func(wire.HostGrant, error) {
	return func(g wire.HostGrant, err error) {
		var invalid InvalidError
		switch {
		case err == nil:
			writeJSON(w, ok, g)
		case errors.As(err, &invalid):
			http.Error(w, invalid.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrNoGrant):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, "update failed", http.StatusInternalServerError)
		}
	}
}

// RoleHostAPI serves /api/host/… (rest is the path after it) for role, the
// base role of an authenticated role token (#369):
//
//	GET    terminals            the records whose parent is role
//	POST   terminals            add a record within role's grant
//	POST   terminals/{id}/start start role's own record
//	POST   terminals/{id}/stop  stop role's own record
//	DELETE terminals/{id}       remove role's own record
//
// Scheduled runs (auto…) and every other hosting action stay the admin's.
// After a write the nodes get their new lists at once.
func (h *Hub) RoleHostAPI(w http.ResponseWriter, r *http.Request, role, rest string) {
	if rest == "auto" || rest == "autos" || strings.HasPrefix(rest, "auto/") || strings.HasPrefix(rest, "autos/") {
		http.Error(w, "scheduled runs (host auto) are for the admin; a role token cannot set them", http.StatusForbidden)
		return
	}
	rest, ok := strings.CutPrefix(rest, "terminals")
	if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		all, err := h.List()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		out := []wire.HostedSession{}
		for _, rec := range all {
			if rec.Parent == role {
				out = append(out, rec)
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	case rest == "" && r.Method == http.MethodPost:
		c, ok := decode[wire.HostedSessionCreate](w, r)
		if !ok {
			return
		}
		rec, err := h.svc.RoleCreate(role, c)
		writeResult(w, http.StatusCreated, rec, err)
	case rest == "":
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	case !ValidID(parts[0]):
		http.Error(w, "no such hosted session", http.StatusNotFound)
		return
	case len(parts) == 1 && r.Method == http.MethodDelete:
		rec, err := h.svc.RoleDelete(role, parts[0])
		writeResult(w, http.StatusOK, rec, err)
	case len(parts) == 2 && (parts[1] == "start" || parts[1] == "stop") && r.Method == http.MethodPost:
		desired := DesiredRunning
		if parts[1] == "stop" {
			desired = DesiredStopped
		}
		rec, err := h.svc.RoleSetDesired(role, parts[0], desired)
		writeResult(w, http.StatusOK, rec, err)
	case len(parts) == 2 && r.Method == http.MethodPost:
		http.Error(w, "host "+parts[1]+" is for the admin; a role token can add, start, stop, remove and list", http.StatusForbidden)
		return
	case len(parts) <= 2:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err := h.Sync(); err != nil {
		log.Printf("hosting: sync: %v", err)
	}
}
