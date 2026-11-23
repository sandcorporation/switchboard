package identity

// Role rotation and retirement (#7). Rotation replaces the role's own token
// (roles.token) with one the operator brings; the old token stops working at
// once, and the role's slots, mail and role credentials (#343, issued per
// hosted record) stay as they are. Retirement is the end of a role: it is
// revoked as by SetRevoked and marked retired, so it can no longer be
// restored, rotated or registered again under its name, while its row and
// every stored message stay, and the dashboard still lists the role and
// reads its mail.

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

var (
	errRetired   = errors.New("role retired")
	errSameToken = errors.New("new credential is the current one")
	errWeakToken = errors.New("credential too short")
)

// minCredentialLen is the shortest role credential registration takes.
const minCredentialLen = 24

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// roleView reads role's row as a RoleView (without group and last use).
func roleView(q Queryer, role string) (RoleView, error) {
	var at, rotated, retired sql.NullString
	v := RoleView{Role: role}
	err := q.QueryRow(`SELECT created_at,revoked_at,rotated_at,retired_at FROM roles WHERE role=?`, role).Scan(&v.CreatedAt, &at, &rotated, &retired)
	if errors.Is(err, sql.ErrNoRows) {
		return v, errNoRole
	}
	v.RevokedAt, v.RotatedAt, v.RetiredAt = nullString(at), nullString(rotated), nullString(retired)
	return v, err
}

// RotateToken replaces role's token with credential. It fails with errNoRole,
// errRetired, errWeakToken (shorter than registration takes) or errSameToken.
// A revoked role may be rotated; the new token works once it is restored.
func (s *Service) RotateToken(ctx context.Context, role, credential string) (RoleView, error) {
	if len(credential) < minCredentialLen {
		return RoleView{}, errWeakToken
	}
	var v RoleView
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		hash, _, ok := roleRecord(tx, role)
		if !ok {
			return errNoRole
		}
		var err error
		if v, err = roleView(tx, role); err != nil {
			return err
		}
		if v.RetiredAt != nil {
			return errRetired
		}
		if tokenMatches(hash, credential) {
			return errSameToken
		}
		stamp := s.now().UTC().Format(time.RFC3339Nano)
		if _, err = tx.Exec(`UPDATE roles SET token=?,rotated_at=? WHERE role=?`, store.TokenHash(credential), stamp, role); err != nil {
			return err
		}
		v.RotatedAt = &stamp
		return nil
	})
	return v, err
}

// Retire revokes role (SetRevoked, with its notice and slot release) and
// marks it retired. Retiring a retired role keeps the first retired_at.
func (s *Service) Retire(ctx context.Context, role string) (RoleView, error) {
	if _, err := s.SetRevoked(ctx, role, true); err != nil {
		return RoleView{}, err
	}
	stamp := s.now().UTC().Format(time.RFC3339Nano)
	var v RoleView
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE roles SET retired_at=? WHERE role=? AND retired_at IS NULL`, stamp, role); err != nil {
			return err
		}
		var err error
		v, err = roleView(tx, role)
		return err
	})
	return v, err
}

// lifecycleAPI serves POST roles/{role}/rotate and roles/{role}/retire and
// reports whether rest was one of them.
func (s *Service) lifecycleAPI(w http.ResponseWriter, r *http.Request, rest string) bool {
	role, rotate := strings.CutSuffix(rest, "/rotate")
	if !rotate {
		var retire bool
		if role, retire = strings.CutSuffix(rest, "/retire"); !retire {
			return false
		}
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if !wire.ValidRole(role) {
		http.Error(w, "invalid role", 400)
		return true
	}
	var v RoleView
	var err error
	if rotate {
		var req struct {
			Credential string `json:"credential"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.Credential == "" {
			http.Error(w, "credential required", 400)
			return true
		}
		if e := config.EnvAlias("ENROLLMENT_TOKEN"); e != "" && len(e) == len(req.Credential) && subtle.ConstantTimeCompare([]byte(e), []byte(req.Credential)) == 1 {
			http.Error(w, "role credential must differ from enrollment credential", 400)
			return true
		}
		v, err = s.RotateToken(r.Context(), role, req.Credential)
	} else {
		if _, revoked, ok := roleRecord(s.db, role); ok && !revoked && s.refuseInUse(w, r, role) {
			return true
		}
		v, err = s.Retire(r.Context(), role)
	}
	switch {
	case errors.Is(err, errNoRole):
		http.Error(w, "no such role", 404)
	case errors.Is(err, errRetired):
		http.Error(w, "role retired", 409)
	case errors.Is(err, errWeakToken):
		http.Error(w, "credential must be at least 24 characters", 400)
	case errors.Is(err, errSameToken):
		http.Error(w, "credential must differ from the current one", 400)
	case err != nil:
		http.Error(w, "update failed", 500)
	default:
		if g, ok := s.GroupOf(v.Role); ok {
			v.Group = g.Prefix
		}
		writeJSON(w, v)
	}
	return true
}
