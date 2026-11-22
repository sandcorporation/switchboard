package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sandcorporation/switchboard/internal/agent"
	"os"
	"path/filepath"

	"github.com/sandcorporation/switchboard/internal/config"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// checkDirRoot is the node's half of a role's grant (#369): the host
// compared r's directory with its grant root by spelling only, so here,
// where the files are, both are resolved through their symbolic links and
// the directory must still be the root or under it. Records the admin added
// have no root and pass.
func checkDirRoot(r Record) error {
	if r.DirRoot == "" {
		return nil
	}
	root, err := filepath.EvalSymlinks(r.DirRoot)
	if err != nil {
		return fmt.Errorf("grant root %s: %w", r.DirRoot, err)
	}
	dir, err := filepath.EvalSymlinks(r.Directory)
	if err != nil {
		return fmt.Errorf("directory %s: %w", r.Directory, err)
	}
	if !wire.DirWithin(dir, root) {
		return fmt.Errorf("directory %s resolves to %s, outside its grant root %s (%s)", r.Directory, dir, r.DirRoot, root)
	}
	return nil
}

// checkRoleProfile keeps a role's record (#369) to its base role's
// identity. A node that takes host-issued credentials launches it with one
// of the base role (Profiles.apply names it); otherwise the node reads the
// profile the record names, under the client config directories
// (configDirs, the first holding it wins, as the CLI reads it), and launches
// only when that profile's role is the base or a numbered slot of it, so a
// role cannot name a profile of another role.
func checkRoleProfile(r Record, configDirs []string) error {
	if r.DirRoot == "" || r.Profile == wire.HostedProfileName(r.ID) {
		return nil
	}
	file := r.Profile + ".json"
	if r.Profile == "default" {
		file = "config.json"
	}
	for _, d := range configDirs {
		b, err := os.ReadFile(filepath.Join(d, file))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("profile %s: %w", r.Profile, err)
		}
		var p struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return fmt.Errorf("profile %s: %w", r.Profile, err)
		}
		if base, _, ok := wire.SplitSlot(p.Role); p.Role == r.Base || ok && base == r.Base {
			return nil
		}
		return fmt.Errorf("profile %s is role %s, not the session's base %s", r.Profile, p.Role, r.Base)
	}
	return fmt.Errorf("profile %s not found", r.Profile)
}

// profileDirs are the directories the CLI reads a profile from: the
// configured one, else the user's switchboard directory and then its legacy
// one.
func profileDirs() []string {
	if d := config.EnvAlias("CONFIG_DIR"); d != "" {
		return []string{d}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(d, config.ConfigDirName), filepath.Join(d, config.LegacyConfigDir)}
}

// checkRoleArgs holds a stored role record (#377) to the same flag
// allow-list the host applied when it was added. Admin records have no
// grant root and keep the deny-list the host applies to them.
func checkRoleArgs(r Record) error { return checkRoleArgsFor(r, agent.Default()) }

// checkRoleArgsFor is checkRoleArgs with the record's adapter's flags.
func checkRoleArgsFor(r Record, a agent.Adapter) error {
	if r.DirRoot == "" {
		return nil
	}
	return agent.CheckRoleArgs(a, r.Args)
}
