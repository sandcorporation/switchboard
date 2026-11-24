package identity

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// RegisterHostedRole registers role for a hosted session (#562, `host add
// --create-role`). Its token is random and kept only as its hash: nobody is
// given it, and the session gets the role's credential from the host. A role
// that is already registered is left as it is. global are the global boss
// roles (SWITCHBOARD_BOSS_ROLES), which, as with an invite, this does not
// register.
func RegisterHostedRole(db *sql.DB, global map[string]bool, role string, now time.Time) error {
	if !wire.ValidRole(role) || Reserved(role) {
		return fmt.Errorf("role %q cannot be registered", role)
	}
	if global[role] {
		return fmt.Errorf("role %s is a global boss role; host add --create-role does not register it", role)
	}
	token, err := newCredential()
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO roles(role,token,created_at) VALUES(?,?,?)`, role, store.TokenHash(token), now.UTC().Format(time.RFC3339Nano))
	return err
}

// IsBossRole reports whether role is a boss role as IsBoss judges a role of
// that name: one of global (SWITCHBOARD_BOSS_ROLES) or the boss role of a
// live group. It reads the groups from q, for a caller without the
// server's group list (the admin's DB_PATH).
func IsBossRole(q Queryer, global map[string]bool, role string) bool {
	if global[role] {
		return true
	}
	var n int
	return q.QueryRow(`SELECT COUNT(*) FROM groups WHERE boss_role=? AND revoked_at IS NULL`, role).Scan(&n) == nil && n > 0
}
