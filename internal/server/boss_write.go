package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// errGlobalOnly is a boss write target only a global boss may change: the
// server-wide limit, a node, the default question setting.
var errGlobalOnly = errors.New("global boss roles only")

// errAnyBoss is a target in every boss role's view.
var errAnyBoss = errors.New("any boss role")

// errNoTarget is a write target that does not exist (404 from the resolver,
// before the handler runs).
var errNoTarget = errors.New("not found")

// bossWriteRoute is one dashboard write a boss role may make with its token
// (#399): the admin path's shape and the role whose scope it falls in.
type bossWriteRoute struct {
	method string
	match  func(path string) bool
	// owner names the role the target belongs to ("" with errGlobalOnly for
	// server-wide targets; roles separated by a space when either puts it in
	// view); body is the request body, already read.
	owner func(s *Server, path string, body []byte) (string, error)
}

// bossWriteRoutes are the dashboard's writes (internal/dashboard/cli_pairs.go)
// a boss role reaches at /api/boss/<admin path>. Admin accounts, host grants
// and invites stay with the admin; the terminal ticket is the screen's.
var bossWriteRoutes = []bossWriteRoute{
	{http.MethodPost, pathShape("messages/*/close"), func(s *Server, p string, _ []byte) (string, error) {
		id, err := wire.ParseID(seg(p, 1))
		if err != nil {
			return "", errNoTarget
		}
		m, err := s.mail().View(mail.CurrentID(s.db, id))
		if err != nil {
			return "", errNoTarget
		}
		// Either end of the directive puts it in view, as for reading it.
		return m.Sender + " " + m.Recipient, nil
	}},
	{http.MethodPost, pathShape("jobs/*/stop"), jobOwner},
	{http.MethodDelete, pathShape("jobs/*"), jobOwner},
	{http.MethodDelete, pathShape("schedules/*"), scheduleOwner},
	{http.MethodPost, pathShape("schedules/*/pause"), scheduleOwner},
	{http.MethodPost, pathShape("schedules/*/resume"), scheduleOwner},
	{http.MethodPost, roleAction("/revoke"), roleOwner("/revoke")},
	{http.MethodPost, roleAction("/restore"), roleOwner("/restore")},
	{http.MethodPut, prefixed("question-settings/"), settingOwner},
	{http.MethodDelete, prefixed("question-settings/"), settingOwner},
	{http.MethodPost, pathShape("terminals"), func(s *Server, _ string, body []byte) (string, error) {
		var c wire.HostedSessionCreate
		if json.Unmarshal(body, &c) != nil {
			return "", errNoTarget
		}
		return c.Base, nil
	}},
	{http.MethodPut, pathShape("terminals/limit"), globalOnly},
	// Work groups (#435) are the whole host's: a global boss's writes.
	{http.MethodPost, pathShape("terminals/work-groups"), globalOnly},
	{http.MethodPut, pathShape("terminals/work-groups/*"), globalOnly},
	{http.MethodDelete, pathShape("terminals/work-groups/*"), globalOnly},
	{http.MethodPut, terminal(""), terminalOwner},
	{http.MethodDelete, terminal(""), terminalOwner},
	{http.MethodPost, terminal("/restart"), terminalOwner},
	{http.MethodPost, terminal("/new-conversation"), terminalOwner},
	{http.MethodPost, pathShape("terminals/*/autos/*/run"), terminalOwner},
	{http.MethodPost, pathShape("terminals/*/autos/*/pause"), terminalOwner},
	{http.MethodPost, pathShape("terminals/*/autos/*/resume"), terminalOwner},
	{http.MethodDelete, pathShape("terminals/*/autos/*"), terminalOwner},
	{http.MethodDelete, pathShape("nodes/*"), globalOnly},
	// Reads of the terminal tab that are not a list view (boss.go): the
	// limit, a session's events and screen log, one scheduled run.
	{http.MethodGet, pathShape("terminals/limit"), anyBoss},
	{http.MethodGet, pathShape("terminals/work-groups"), anyBoss},
	{http.MethodGet, terminal("/events"), terminalOwner},
	{http.MethodGet, terminal("/log"), terminalOwner},
	{http.MethodGet, pathShape("terminals/*/autos/*"), terminalOwner},
	// One job or schedule with its command or prompt (#404); the jobs
	// package records the read.
	{http.MethodGet, pathShape("jobs/*"), jobOwner},
	{http.MethodGet, pathShape("schedules/*"), scheduleOwner},
}

// bossReadThrough reports whether a GET /api/boss/<path> is one of the
// admin reads in bossWriteRoutes, which bossWrite serves (unrecorded).
func bossReadThrough(path string) bool {
	for _, rt := range bossWriteRoutes {
		if rt.method == http.MethodGet && rt.match(path) {
			return true
		}
	}
	return false
}

// bossWrite serves a non-GET /api/boss/<admin path> (#399): a global boss
// makes the dashboard admin's write, a group boss only on targets of its
// groups (identity.BossScope), any other role gets 403. The same admin
// handler runs (adminRoutes). Every attempt past the role check is recorded
// (boss_writes) with role, method, path and target, never the body.
func (s *Server) bossWrite(w http.ResponseWriter, r *http.Request, who identity.Principal, path string) {
	ids := s.ids()
	see, ok := ids.BossScope(who)
	if !ok {
		http.Error(w, "boss roles only", http.StatusForbidden)
		return
	}
	var route *bossWriteRoute
	for i := range bossWriteRoutes {
		if bossWriteRoutes[i].method == r.Method && bossWriteRoutes[i].match(path) {
			route = &bossWriteRoutes[i]
			break
		}
	}
	if route == nil {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(path, "terminals") || strings.HasPrefix(path, "nodes") {
		if !s.hosting {
			http.Error(w, "terminal hosting is off on this server", http.StatusNotFound)
			return
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	owner, err := route.owner(s, path, body)
	global := ids.IsGlobalBoss(who)
	status := 0
	switch {
	case errors.Is(err, errAnyBoss):
	case errors.Is(err, errGlobalOnly):
		if !global {
			status = http.StatusForbidden
		}
	case err != nil:
		status = http.StatusNotFound
	case !global && !seesAny(see, owner):
		status = http.StatusForbidden
	}
	sw := &statusWriter{ResponseWriter: w}
	switch status {
	case http.StatusForbidden:
		http.Error(sw, "outside this boss role's groups", status)
	case http.StatusNotFound:
		http.Error(sw, "not found", status)
	default:
		w.Header().Set("Content-Type", "application/json")
		s.adminRoutes(sw, r, path, adminauth.Identity{User: who.Role}, &who)
	}
	code := sw.code
	if code == 0 {
		code = http.StatusOK
	}
	if r.Method != http.MethodGet {
		s.recordBossWrite(who.Role, r.Method, path, owner, code)
	}
}

// recordBossWrite keeps one boss write in boss_writes and the log: role,
// method, path, target and status; no body or header.
func (s *Server) recordBossWrite(role, method, path, target string, status int) {
	log.Printf("boss write: role=%s %s %s target=%s status=%d", role, method, path, target, status)
	if _, err := s.db.Exec(`INSERT INTO boss_writes(role,method,path,target,status,written_at) VALUES(?,?,?,?,?,?)`,
		role, method, path, target, status, time.Now().UnixMilli()); err != nil {
		log.Printf("boss write: record: %v", err)
	}
}

// pathShape matches a path segment by segment; * is any one non-empty segment.
func pathShape(shape string) func(string) bool {
	want := strings.Split(shape, "/")
	return func(p string) bool {
		got := strings.Split(p, "/")
		if len(got) != len(want) {
			return false
		}
		for i, w := range want {
			if got[i] == "" || w != "*" && w != got[i] {
				return false
			}
		}
		return true
	}
}

func prefixed(prefix string) func(string) bool {
	return func(p string) bool { return strings.HasPrefix(p, prefix) && len(p) > len(prefix) }
}

// roleAction matches roles/<role>/<suffix>; a role name may hold a slash.
func roleAction(suffix string) func(string) bool {
	return func(p string) bool {
		rest, ok := strings.CutPrefix(p, "roles/")
		role, ok2 := strings.CutSuffix(rest, suffix)
		return ok && ok2 && role != ""
	}
}

// terminal matches terminals/<id><suffix> for a record ID (no slash).
func terminal(suffix string) func(string) bool {
	return func(p string) bool {
		rest, ok := strings.CutPrefix(p, "terminals/")
		id, ok2 := strings.CutSuffix(rest, suffix)
		return ok && ok2 && id != "" && id != "limit" && id != "work-groups" && !strings.Contains(id, "/")
	}
}

func seg(p string, i int) string {
	parts := strings.Split(p, "/")
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

func seesAny(see func(string) bool, roles string) bool {
	for _, r := range strings.Fields(roles) {
		if see(r) {
			return true
		}
	}
	return false
}

// anyBoss is a target every boss role may read.
func anyBoss(*Server, string, []byte) (string, error) { return "", errAnyBoss }

func globalOnly(*Server, string, []byte) (string, error) { return "", errGlobalOnly }

func jobOwner(s *Server, p string, _ []byte) (string, error) {
	id, err := strconv.ParseInt(seg(p, 1), 10, 64)
	if err != nil {
		return "", errNoTarget
	}
	j, err := s.jobs().Get(id)
	if err != nil {
		return "", errNoTarget
	}
	return j.Role, nil
}

func scheduleOwner(s *Server, p string, _ []byte) (string, error) {
	id, err := strconv.ParseInt(seg(p, 1), 10, 64)
	if err != nil {
		return "", errNoTarget
	}
	sc, err := s.jobs().GetSchedule(id)
	if err != nil {
		return "", errNoTarget
	}
	return sc.Role, nil
}

func roleOwner(suffix string) func(*Server, string, []byte) (string, error) {
	return func(_ *Server, p string, _ []byte) (string, error) {
		return strings.TrimSuffix(strings.TrimPrefix(p, "roles/"), suffix), nil
	}
}

// settingOwner is a question setting's target: a role, or PREFIX/* (asked as
// a role under the prefix); the default * is the global boss's.
func settingOwner(_ *Server, p string, _ []byte) (string, error) {
	target := strings.TrimPrefix(p, "question-settings/")
	if prefix, ok := strings.CutSuffix(target, "/*"); ok {
		return prefix + "/" + groupProbe, nil
	}
	if !strings.Contains(target, "/") {
		return "", errGlobalOnly
	}
	return target, nil
}

func terminalOwner(s *Server, p string, _ []byte) (string, error) {
	rec, err := s.hostingService().Get(seg(p, 1))
	if err != nil {
		return "", errNoTarget
	}
	return rec.Base, nil
}
