package rules

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Service tags (#406). Tags sit on a service; its roles (coordinator and
// numbered slots included, through their base role) inherit them, and a
// role adds or removes its own. A role's tags are its service's, plus the
// ones it added, minus the ones it removed. Only an admin or a global boss
// reads or sets them.

const tagsForbidden = "tags are read and set by an admin or a global boss (SWITCHBOARD_BOSS_ROLES)"

func readTags(q queryer, query string, args ...any) ([]string, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// serviceTags is the tags set on a service.
func (s *Service) serviceTags(q queryer, svc string) ([]string, error) {
	return readTags(q, `SELECT tag FROM service_tags WHERE service=? ORDER BY tag`, svc)
}

// tagsOf is a base role's tags: its service's, plus its additions, minus
// its removals.
func (s *Service) tagsOf(q queryer, role string) ([]string, error) {
	tags, err := s.serviceTags(q, ServiceOf(role))
	if err != nil {
		return nil, err
	}
	add, err := readTags(q, `SELECT tag FROM role_tags WHERE role=? AND op='+'`, role)
	if err != nil {
		return nil, err
	}
	remove, err := readTags(q, `SELECT tag FROM role_tags WHERE role=? AND op='-'`, role)
	if err != nil {
		return nil, err
	}
	tags = append(tags, add...)
	tags = slices.DeleteFunc(tags, func(t string) bool { return slices.Contains(remove, t) })
	slices.Sort(tags)
	return slices.Compact(tags), nil
}

// roleTags is a role's additions and removals, and the tags it ends with.
func (s *Service) roleTags(q queryer, role string) (wire.RoleTags, error) {
	rt := wire.RoleTags{Role: role}
	var err error
	if rt.Add, err = readTags(q, `SELECT tag FROM role_tags WHERE role=? AND op='+' ORDER BY tag`, role); err != nil {
		return rt, err
	}
	if rt.Remove, err = readTags(q, `SELECT tag FROM role_tags WHERE role=? AND op='-' ORDER BY tag`, role); err != nil {
		return rt, err
	}
	rt.Tags, err = s.tagsOf(q, role)
	return rt, err
}

// allTags is GET /api/rules/tags.
func (s *Service) allTags() (wire.RuleTags, error) {
	out := wire.RuleTags{Services: []wire.ServiceTags{}, Roles: []wire.RoleTags{}}
	svcs, err := readTags(s.db, `SELECT DISTINCT service FROM service_tags ORDER BY service`)
	if err != nil {
		return out, err
	}
	for _, svc := range svcs {
		tags, err := s.serviceTags(s.db, svc)
		if err != nil {
			return out, err
		}
		out.Services = append(out.Services, wire.ServiceTags{Service: svc, Tags: tags})
	}
	roles, err := readTags(s.db, `SELECT DISTINCT role FROM role_tags ORDER BY role`)
	if err != nil {
		return out, err
	}
	for _, role := range roles {
		rt, err := s.roleTags(s.db, role)
		if err != nil {
			return out, err
		}
		out.Roles = append(out.Roles, rt)
	}
	return out, nil
}

func checkTags(tags []string) error {
	for _, t := range tags {
		if err := wire.CheckRuleName("tag", t); err != nil {
			return err
		}
	}
	return nil
}

// tagsAPI serves /api/rules/tags (rest after "tags/"): GET "" every
// service's and role's tags, PUT services a service's whole tag set
// ({"service","tags"}; no tags clears it), POST roles a role's changes
// ({"role","add","remove","inherit"}).
func (s *Service) tagsAPI(w http.ResponseWriter, r *http.Request, c Caller, rest string) {
	if !c.Full {
		http.Error(w, tagsForbidden, http.StatusForbidden)
		return
	}
	switch {
	case rest == "" && r.Method == http.MethodGet:
		out, err := s.allTags()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, out)
	case rest == "services" && r.Method == http.MethodPut:
		s.setServiceTags(w, r)
	case rest == "roles" && r.Method == http.MethodPost:
		s.changeRoleTags(w, r)
	case rest == "" || rest == "services" || rest == "roles":
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) setServiceTags(w http.ResponseWriter, r *http.Request) {
	var q wire.ServiceTags
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&q) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if q.Service == "" || strings.ContainsAny(q.Service, "/ ,") {
		http.Error(w, "service: the part of a role before its first /", http.StatusBadRequest)
		return
	}
	if err := checkTags(q.Tags); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err := store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM service_tags WHERE service=?`, q.Service); err != nil {
			return err
		}
		for _, t := range q.Tags {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO service_tags(service,tag) VALUES(?,?)`, q.Service, t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, "could not set the tags", http.StatusInternalServerError)
		return
	}
	tags, err := s.serviceTags(s.db, q.Service)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, wire.ServiceTags{Service: q.Service, Tags: tags})
}

func (s *Service) changeRoleTags(w http.ResponseWriter, r *http.Request) {
	var q wire.RoleTagChange
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&q) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := checkRoleName(q.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	seen := map[string]bool{}
	for _, list := range [][]string{q.Add, q.Remove, q.Inherit} {
		if err := checkTags(list); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, t := range list {
			if seen[t] {
				http.Error(w, "tag "+t+": named twice", http.StatusBadRequest)
				return
			}
			seen[t] = true
		}
	}
	if len(seen) == 0 {
		http.Error(w, "name a tag to add, remove or inherit", http.StatusBadRequest)
		return
	}
	err := store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		for op, list := range map[string][]string{"+": q.Add, "-": q.Remove} {
			for _, t := range list {
				if _, err := tx.Exec(`INSERT INTO role_tags(role,tag,op) VALUES(?,?,?) ON CONFLICT(role,tag) DO UPDATE SET op=excluded.op`, q.Role, t, op); err != nil {
					return err
				}
			}
		}
		for _, t := range q.Inherit {
			if _, err := tx.Exec(`DELETE FROM role_tags WHERE role=? AND tag=?`, q.Role, t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, "could not change the tags", http.StatusInternalServerError)
		return
	}
	rt, err := s.roleTags(s.db, q.Role)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, rt)
}

// checkRoleName checks a base role named in a request.
func checkRoleName(role string) error {
	if role == "" || strings.ContainsAny(role, " ,") {
		return errors.New("role: a base role")
	}
	return nil
}
