package hosting

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Roles is what the hosted-session store asks identity about a record's
// base role (#562); the server passes in identity's.
type Roles struct {
	// IsBoss reports whether role is a boss role: a global boss or the boss
	// role of a live group.
	IsBoss func(role string) bool
	// Create registers role with a token nobody is given (the session gets
	// its credential from the host). It refuses a name no role may have.
	Create func(role string) error
}

// WithRoles sets the base-role checks of a new record. Without them
// --create-role is refused and a boss base is not told apart.
func (s *Service) WithRoles(r *Roles) *Service {
	s.roles = r
	return s
}

// missingRole is the refusal of a base that is not a role, without
// CreateRole: a typo never registers a role.
func missingRole(base string) error {
	return invalidf("base: role %s does not exist (host add --create-role registers it with the session)", base)
}

// bossBase is the refusal of a new record whose base is a boss role,
// without AnotherBoss: it names both ways on.
func bossBase(base string) error {
	return invalidf("base: %s is a boss role; for a session of another role use --base <new role> --create-role, or to run one more %s session add --another-boss", base, base)
}

// ErrCreateRoleForbidden is a role's own add (its host grant) that asks to
// register its base: registering a role is a boss role's or the admin's.
var ErrCreateRoleForbidden = ForbiddenError{"create_role: registering a role is a boss role's (its token) or the admin's (DB_PATH); a host grant does not allow it"}

// noCredentials is the refusal of a new role's record on a node that does
// not take role credentials: the node launches it with the profile the
// record names, and it has none of a role registered a moment ago.
func noCredentials(node, base string) error {
	return invalidf("node: %s does not take role credentials, so it has no profile of the new role %s and the session could not use its mailbox; add it on a node that takes them (one that joined with switchboard node join; up's own node does not), or register %s on that machine with a profile first and add it without --create-role", node, base, base)
}

// credentialsUnknown is the reason recorded on a new role's record when its
// node has not said whether it takes role credentials.
func credentialsUnknown(node, base string) string {
	return fmt.Sprintf("node %s has not said whether it takes role credentials (it has not attached since the host began recording it); if it does not, it has no profile of the new role %s and the session cannot use its mailbox: move the session to a node that takes them (one that joined with switchboard node join), or register %s on that machine with a profile", node, base, base)
}

// nodeCredentials is whether node asked for role credentials when it last
// attached; known is false before it attached under a host that records it.
func (s *Service) nodeCredentials(node string) (takes, known bool, err error) {
	var v sql.NullInt64
	switch err := s.db.QueryRow(`SELECT credentials FROM nodes WHERE name=?`, node).Scan(&v); {
	case errors.Is(err, sql.ErrNoRows):
		return false, false, nil
	case err != nil:
		return false, false, err
	}
	return v.Int64 == 1, v.Valid, nil
}

// setNodeCredentials records whether node asked for role credentials as it
// attached.
func (s *Service) setNodeCredentials(node string, takes bool) error {
	_, err := s.db.Exec(`UPDATE nodes SET credentials=? WHERE name=?`, boolInt(takes), node)
	return err
}

// baseRole checks c's base role for a new record. guard is a CLI's add (the
// admin's DB_PATH, a boss role's or a role's token), which may not add a
// boss base without AnotherBoss; the dashboard's admin form is not guarded.
// newRole is whether Create must register the base before the record.
func (s *Service) baseRole(c wire.HostedSessionCreate, guard bool) (newRole bool, err error) {
	exists := s.roleExists != nil && s.roleExists(c.Base)
	if !exists && !c.CreateRole {
		return false, missingRole(c.Base)
	}
	if guard && !c.AnotherBoss && s.roles != nil && s.roles.IsBoss != nil && s.roles.IsBoss(c.Base) {
		return false, bossBase(c.Base)
	}
	if exists {
		return false, nil
	}
	if s.roles == nil || s.roles.Create == nil {
		return false, invalidf("base: role %s does not exist and this host does not register roles", c.Base)
	}
	takes, known, err := s.nodeCredentials(c.Node)
	if err != nil {
		return false, err
	}
	if known && !takes {
		return false, noCredentials(c.Node, c.Base)
	}
	return true, nil
}
