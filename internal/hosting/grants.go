package hosting

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sandcorporation/switchboard/internal/agent"
	"io"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Host grants (#369): the admin lets a role add, start, stop and remove
// hosted sessions under itself with its own token, within a grant of base
// roles, nodes and directory roots.

var (
	// ErrNoGrant means the role has no host grant.
	ErrNoGrant = errors.New("no host grant")
)

// ForbiddenError is a role's request outside its grant; its message names
// the condition it broke.
type ForbiddenError struct{ msg string }

func (e ForbiddenError) Error() string { return e.msg }

func forbiddenf(format string, a ...any) error { return ForbiddenError{fmt.Sprintf(format, a...)} }

// cleanList trims, splits on commas and drops empty and repeated entries.
func cleanList(in []string) []string {
	out := []string{}
	for _, v := range in {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// SetGrant stores role's grant, replacing the one it had.
func (s *Service) SetGrant(g wire.HostGrant) (wire.HostGrant, error) {
	if !wire.ValidRole(g.Role) {
		return g, invalidf("role: invalid role %q", g.Role)
	}
	if s.roleExists == nil || !s.roleExists(g.Role) {
		return g, invalidf("role: role %s does not exist", g.Role)
	}
	g.Bases, g.Nodes = cleanList(g.Bases), cleanList(g.Nodes)
	if len(g.Bases) == 0 || len(g.Nodes) == 0 {
		return g, invalidf("a grant needs at least one base, node and directory root")
	}
	for _, b := range g.Bases {
		if !wire.ValidRole(b) {
			return g, invalidf("base: invalid role %q", b)
		}
		if !s.roleExists(b) {
			return g, invalidf("base: role %s does not exist", b)
		}
	}
	for _, n := range g.Nodes {
		if err := checkNodeName(n); err != nil {
			return g, err
		}
	}
	roots := []string{}
	for _, r := range cleanList(g.DirRoots) {
		c, _, ok := wire.CleanDir(r)
		if !ok {
			return g, invalidf("dir-root: %q is not an absolute path", r)
		}
		if !slices.Contains(roots, c) {
			roots = append(roots, c)
		}
	}
	if len(roots) == 0 {
		return g, invalidf("a grant needs at least one base, node and directory root")
	}
	g.DirRoots = roots
	g.Adapters = cleanList(g.Adapters)
	for _, a := range g.Adapters {
		if !adapterPattern.MatchString(a) {
			return g, invalidf("adapter: invalid name %q", a)
		}
	}
	g.Groups = cleanList(g.Groups)
	for _, n := range g.Groups {
		if err := s.checkGroup(n); err != nil {
			return g, err
		}
	}
	now := s.clock().UnixMilli()
	bases, _ := json.Marshal(g.Bases)
	nodes, _ := json.Marshal(g.Nodes)
	dirs, _ := json.Marshal(g.DirRoots)
	if _, err := s.db.Exec(`INSERT INTO host_grants(role,bases,nodes,dir_roots,adapters,groups,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(role) DO UPDATE SET bases=excluded.bases, nodes=excluded.nodes, dir_roots=excluded.dir_roots, adapters=excluded.adapters, groups=excluded.groups, updated_at=excluded.updated_at`,
		g.Role, string(bases), string(nodes), string(dirs), encodeList(g.Adapters), encodeList(g.Groups), now, now); err != nil {
		return g, err
	}
	return s.Grant(g.Role)
}

func scanGrant(r rowScanner) (wire.HostGrant, error) {
	var g wire.HostGrant
	var bases, nodes, dirs, adapters, groups string
	if err := r.Scan(&g.Role, &bases, &nodes, &dirs, &g.CreatedAt, &g.UpdatedAt, &adapters, &groups); err != nil {
		return g, err
	}
	g.Adapters, g.Groups = decodeList(adapters), decodeList(groups)
	for _, f := range []struct {
		raw string
		to  *[]string
	}{{bases, &g.Bases}, {nodes, &g.Nodes}, {dirs, &g.DirRoots}} {
		if json.Unmarshal([]byte(f.raw), f.to) != nil || *f.to == nil {
			*f.to = []string{}
		}
	}
	return g, nil
}

// Grant is role's grant, or ErrNoGrant.
func (s *Service) Grant(role string) (wire.HostGrant, error) {
	g, err := scanGrant(s.db.QueryRow(`SELECT role,bases,nodes,dir_roots,created_at,updated_at,adapters,groups FROM host_grants WHERE role=?`, role))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNoGrant
	}
	return g, err
}

// Grants lists every grant, by role.
func (s *Service) Grants() ([]wire.HostGrant, error) {
	rows, err := s.db.Query(`SELECT role,bases,nodes,dir_roots,created_at,updated_at,adapters,groups FROM host_grants ORDER BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.HostGrant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeGrant removes role's grant and returns it as it was. The records the
// role added stay; the role can no longer change them.
func (s *Service) RevokeGrant(role string) (wire.HostGrant, error) {
	g, err := s.Grant(role)
	if err != nil {
		return g, err
	}
	_, err = s.db.Exec(`DELETE FROM host_grants WHERE role=?`, role)
	return g, err
}

// admit checks c, a record role asks to add, against role's grant, and
// returns the grant root its directory is under. An empty parent becomes
// role.
func (s *Service) admit(role string, c *wire.HostedSessionCreate) (string, error) {
	g, err := s.Grant(role)
	if errors.Is(err, ErrNoGrant) {
		return "", forbiddenf("no host grant for %s (the admin gives one with host grant)", role)
	}
	if err != nil {
		return "", err
	}
	if c.Parent == "" {
		c.Parent = role
	}
	if c.Parent != role {
		return "", forbiddenf("parent: %s is not %s (a role adds sessions under itself)", c.Parent, role)
	}
	if !slices.Contains(g.Bases, c.Base) {
		return "", forbiddenf("base: %s is not in the grant of %s (%s)", c.Base, role, strings.Join(g.Bases, ", "))
	}
	if !slices.Contains(g.Nodes, c.Node) {
		return "", forbiddenf("node: %s is not in the grant of %s (%s)", c.Node, role, strings.Join(g.Nodes, ", "))
	}
	if c.Group != "" && !slices.Contains(g.Groups, c.Group) {
		if len(g.Groups) == 0 {
			return "", forbiddenf("group: the grant of %s allows no group", role)
		}
		return "", forbiddenf("group: %s is not in the grant of %s (%s)", c.Group, role, strings.Join(g.Groups, ", "))
	}
	if len(g.Adapters) > 0 {
		eff := c.Adapter
		if eff == "" {
			eff = s.nodeDefaultAdapter(c.Node)
		}
		if !slices.Contains(g.Adapters, eff) {
			return "", forbiddenf("adapter: %s is not in the grant of %s (%s)", eff, role, strings.Join(g.Adapters, ", "))
		}
	}
	if err := agent.CheckRoleArgs(roleAdapter(c.Adapter), c.Args); err != nil {
		return "", forbiddenf("%v", err)
	}
	if len(c.Env) > 0 {
		return "", forbiddenf("env: a role's session takes no env (it could point the session at another profile); the admin sets env")
	}
	clean, _, ok := wire.CleanDir(c.Directory)
	if !ok {
		return "", invalidf("directory: %q is not an absolute path", c.Directory)
	}
	for _, root := range g.DirRoots {
		if wire.DirWithin(clean, root) {
			c.Directory = clean
			return root, nil
		}
	}
	return "", forbiddenf("directory: %s is not under the grant roots of %s (%s)", clean, role, strings.Join(g.DirRoots, ", "))
}

// RoleCreate adds c as role's record within its grant (parent role, a
// group of the grant or none) and records who added it in the session's history.
func (s *Service) RoleCreate(role string, c wire.HostedSessionCreate) (wire.HostedSession, error) {
	if c.CreateRole {
		return wire.HostedSession{}, ErrCreateRoleForbidden
	}
	root, err := s.admit(role, &c)
	if err != nil {
		return wire.HostedSession{}, err
	}
	h, err := s.create(c, root, true)
	if err != nil {
		return h, err
	}
	return h, s.auditEvent(h.ID, wire.HostedEventAdd, role)
}

// own is the record id if role is its parent; ErrNotFound when there is
// none, and a ForbiddenError when it is another's.
func (s *Service) own(role, id string) (wire.HostedSession, error) {
	h, err := s.Get(id)
	if err != nil {
		return h, err
	}
	if h.Parent != role {
		return h, forbiddenf("hosted session %s is not under %s (parent %q)", id, role, h.Parent)
	}
	return h, nil
}

// RoleSetDesired starts or stops role's own record id.
func (s *Service) RoleSetDesired(role, id, desired string) (wire.HostedSession, error) {
	if _, err := s.own(role, id); err != nil {
		return wire.HostedSession{}, err
	}
	return s.SetDesired(id, desired)
}

// RoleDelete removes role's own record id. Unlike the admin's Delete it
// keeps the session's history and adds who removed it.
func (s *Service) RoleDelete(role, id string) (wire.HostedSession, error) {
	h, err := s.own(role, id)
	if err != nil {
		return wire.HostedSession{}, err
	}
	res, err := s.db.Exec(`DELETE FROM hosted_sessions WHERE id=? AND parent=?`, id, role)
	if err != nil {
		return h, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return h, ErrNotFound
	}
	if err = s.deleteAutosOf(id); err != nil {
		return h, err
	}
	if err = s.revokeCredentials(func(c LiveCredential) bool { return c.Record == id }); err != nil {
		return h, err
	}
	return h, s.auditEvent(id, wire.HostedEventRemove, role)
}

// auditEvent stores who added or removed a record; unlike recordEvent it
// does not need the record to exist.
func (s *Service) auditEvent(id, kind, actor string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO hosted_events(session,at,kind,reason,restarts,actor) VALUES(?,?,?,'',0,?)`,
		id, s.clock().UnixMilli(), kind, actor)
	return err
}

// RunGrantCLI serves `host grant ROLE`, `host grant list` (action
// grant-list) and `host revoke-grant ROLE` against the service's database.
func (s *Service) RunGrantCLI(action string, g wire.HostGrant, jsonOut bool, stdout io.Writer) error {
	var err error
	switch action {
	case "grant-list":
		list, err := s.Grants()
		if err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(stdout).Encode(list)
		}
		printGrants(stdout, list)
		return nil
	case "grant":
		g, err = s.SetGrant(g)
	case "revoke-grant":
		g, err = s.RevokeGrant(g.Role)
	default:
		return fmt.Errorf("host: unknown grant action %q", action)
	}
	switch {
	case errors.Is(err, ErrNoGrant):
		return fmt.Errorf("no host grant for %s", g.Role)
	case err != nil:
		return err
	case jsonOut:
		return json.NewEncoder(stdout).Encode(g)
	case action == "revoke-grant":
		_, err = fmt.Fprintf(stdout, "revoked the host grant of %s\n", g.Role)
		return err
	}
	printGrants(stdout, []wire.HostGrant{g})
	return nil
}

func printGrants(w io.Writer, list []wire.HostGrant) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no host grants")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// The ADAPTERS column shows only when a grant limits them (#434), so
	// the table is as before otherwise.
	adapters := slices.ContainsFunc(list, func(g wire.HostGrant) bool { return len(g.Adapters) > 0 })
	groups := slices.ContainsFunc(list, func(g wire.HostGrant) bool { return len(g.Groups) > 0 })
	head := "ROLE\tBASES\tNODES\tDIR ROOTS"
	if adapters {
		head += "\tADAPTERS"
	}
	if groups {
		head += "\tGROUPS"
	}
	fmt.Fprintln(tw, head)
	for _, g := range list {
		row := fmt.Sprintf("%s\t%s\t%s\t%s", g.Role, strings.Join(g.Bases, ","), strings.Join(g.Nodes, ","), strings.Join(g.DirRoots, ","))
		if adapters {
			row += "\t" + dash(strings.Join(g.Adapters, ","))
		}
		if groups {
			row += "\t" + dash(strings.Join(g.Groups, ","))
		}
		fmt.Fprintln(tw, row)
	}
	tw.Flush()
}

// roleAdapter is the adapter whose role flags a role's record is held to:
// the named one when the host knows it, else the default. A role's record
// naming an adapter the host does not know carries no flags.
func roleAdapter(name string) agent.Adapter {
	if name == "" {
		return agent.Default()
	}
	if a, err := agent.Lookup(name); err == nil {
		return a
	}
	return noFlags{agent.Default()}
}

// noFlags is an adapter that allows a role's session no flags.
type noFlags struct{ agent.Adapter }

func (noFlags) RoleArgs() map[string]*regexp.Regexp { return nil }
