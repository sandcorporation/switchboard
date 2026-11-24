package hosting

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Admin actions (#446): the admin's hosting writes, appended to
// admin_actions. A row names who (the admin, or a boss role), how they signed
// in, the action, its target (a name or a path, never a value) and the
// client address.

// Actor is who makes an admin request: the admin or boss role name, the
// login mode and the client address. The server puts it on the request
// (WithActor) before a hosting admin handler runs.
type Actor struct {
	Admin  string
	Method string
	Addr   string
}

type actorKey struct{}

// WithActor returns r carrying a.
func WithActor(r *http.Request, a Actor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorKey{}, a))
}

func actorOf(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// AdminActionKeep is how many rows one GET returns at most.
const AdminActionKeep = 200

// recordAction appends one admin action for the actor on r. A request
// without an actor (a test, the CLI) records nothing. A failed write is
// logged, never fatal: the action itself has happened.
func (s *Service) recordAction(r *http.Request, action, target string) {
	s.recordActionDetail(r, action, target, "")
}

// recordActionDetail is recordAction with a detail (a path and a size,
// never a value).
func (s *Service) recordActionDetail(r *http.Request, action, target, detail string) {
	a, ok := actorOf(r.Context())
	if !ok {
		return
	}
	RecordAction(s.db, s.clock(), a, action, target, detail)
}

// RecordAction appends one admin action of a at the time given: the record
// other packages' admin writes share (the webhooks', #549). A failed write
// is logged, never fatal.
func RecordAction(db *sql.DB, at time.Time, a Actor, action, target, detail string) {
	if _, err := db.Exec(`INSERT INTO admin_actions(at,admin,method,action,target,addr,detail) VALUES(?,?,?,?,?,?,?)`,
		at.UnixMilli(), a.Admin, a.Method, action, target, a.Addr, detail); err != nil {
		log.Printf("admin action %s: %v", action, err)
	}
}

// AdminActions lists the admin actions newest first, at most limit
// (AdminActionKeep when limit is out of range); target "" is every target.
func (s *Service) AdminActions(target string, limit int) ([]wire.AdminAction, error) {
	if limit <= 0 || limit > AdminActionKeep {
		limit = AdminActionKeep
	}
	q := `SELECT at,admin,method,action,target,addr,detail FROM admin_actions`
	args := []any{}
	if target != "" {
		q += ` WHERE target=?`
		args = append(args, target)
	}
	q += ` ORDER BY at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.AdminAction{}
	for rows.Next() {
		var a wire.AdminAction
		if err := rows.Scan(&a.At, &a.Admin, &a.Method, &a.Action, &a.Target, &a.Addr, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AdminActionsAPI serves GET /api/admin/admin-actions[?target=NAME&limit=N].
func (s *Service) AdminActionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.AdminActions(r.URL.Query().Get("target"), limit)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminInviteAPI serves POST /api/admin/node-invites (#446): the same
// one-time code as `switchboard node invite`, with the same lifetime. The
// code is in this response only; the action is recorded with the node name.
func (s *Service) AdminInviteAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	in, ok := decode[wire.NodeInvite](w, r)
	if !ok {
		return
	}
	code, expires, err := s.InviteNode(in.Name)
	var invalid InvalidError
	switch {
	case errors.As(err, &invalid):
		http.Error(w, invalid.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, ErrNodeExists):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "invite failed", http.StatusInternalServerError)
		return
	}
	s.recordAction(r, wire.ActionNodeInvite, in.Name)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, wire.NodeInvited{Name: in.Name, Code: code, ExpiresAt: expires.UnixMilli()})
}
