package adminauth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Per-login permissions (#476). A dashboard login may do on the work screen
// what its permissions name: view (terminals and files, read only), type
// (input into a terminal), files (save, delete, rename, upload), sessions
// (start, stop, restart, change or delete a hosted session) and discard
// (drop an item of the left ledger). They are set with `admin allow NAME
// LIST` only and kept in admin_perms; a login without a row has them all,
// so every login that existed before them keeps what it could do.
const (
	PermView     = wire.PermView
	PermType     = wire.PermType
	PermFiles    = wire.PermFiles
	PermSessions = wire.PermSessions
	PermDiscard  = wire.PermDiscard
)

// AllPerms is every permission, in the order lists show them.
var AllPerms = wire.AllPerms

// Perms is a set of permissions, in AllPerms order.
type Perms []string

// MarshalJSON writes an empty set as [], never null.
func (p Perms) MarshalJSON() ([]byte, error) {
	if p == nil {
		p = Perms{}
	}
	return json.Marshal([]string(p))
}

// Can reports whether p holds perm.
func (p Perms) Can(perm string) bool { return slices.Contains(p, perm) }

// String is the list as `admin allow` takes it: "view,type", or "none".
func (p Perms) String() string {
	if len(p) == 0 {
		return "none"
	}
	return strings.Join(p, ",")
}

// ParsePerms reads `admin allow`'s list: names joined by commas, "all" or
// "none". The result is in AllPerms order without repeats.
func ParsePerms(s string) (Perms, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "all":
		return slices.Clone(AllPerms), nil
	case "none":
		return Perms{}, nil
	case "":
		return nil, errors.New("no permissions named (view, type, files, sessions, discard; all; none)")
	}
	want := map[string]bool{}
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if !slices.Contains(AllPerms, f) {
			return nil, fmt.Errorf("unknown permission %q (view, type, files, sessions, discard; all; none)", f)
		}
		want[f] = true
	}
	out := Perms{}
	for _, p := range AllPerms {
		if want[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// permsOf reads a stored list; NULL (no row) is every permission.
func permsOf(stored sql.NullString) Perms {
	if !stored.Valid {
		return slices.Clone(AllPerms)
	}
	if stored.String == "" {
		return Perms{}
	}
	p, err := ParsePerms(stored.String)
	if err != nil {
		// A row this build cannot read grants nothing.
		return Perms{}
	}
	return p
}

// loadPerms is user's permissions now.
func loadPerms(db *sql.DB, user string) (Perms, error) {
	var stored sql.NullString
	err := db.QueryRow(`SELECT perms FROM admin_perms WHERE name=?`, user).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return permsOf(sql.NullString{}), nil
	}
	if err != nil {
		return nil, err
	}
	return permsOf(stored), nil
}

// Allowed reports whether user may do perm now, read from the database on
// every call so a change takes effect on sockets already open. A failed
// read allows nothing.
func (s *Service) Allowed(user, perm string) bool {
	p, err := loadPerms(s.db, user)
	return err == nil && p.Can(perm)
}

// allowAdmin serves `admin allow NAME PERMS`: perms replaces what name may
// do. name need not be a built-in user: an oidc login has no admin_users
// row and is named by its e-mail. Sessions stay; every request and every
// typed frame reads the permissions again.
func allowAdmin(name string, perms Perms, jsonOut bool, stdout io.Writer) (int, error) {
	db, err := store.Open(config.Env("DB_PATH", "/data/mail.db"))
	if err != nil {
		return 0, err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO admin_perms(name,perms,updated_at) VALUES(?,?,?)
ON CONFLICT(name) DO UPDATE SET perms=excluded.perms, updated_at=excluded.updated_at`, name, strings.Join(perms, ","), now); err != nil {
		return 0, err
	}
	var builtin int
	if err := db.QueryRow(`SELECT COUNT(*) FROM admin_users WHERE name=?`, name).Scan(&builtin); err != nil {
		return 0, err
	}
	if jsonOut {
		b, _ := json.Marshal(struct {
			Name   string `json:"name"`
			Action string `json:"action"`
			Perms  Perms  `json:"perms"`
		}{name, "allow", perms})
		fmt.Fprintln(stdout, string(b))
		return 0, nil
	}
	fmt.Fprintf(stdout, "%s may: %s\n", name, perms)
	if builtin == 0 {
		fmt.Fprintf(stdout, "no built-in user %s: this applies to an oidc login of that name\n", name)
	}
	return 0, nil
}
