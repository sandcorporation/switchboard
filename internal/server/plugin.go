package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/sandcorporation/switchboard/internal/agent"
	_ "github.com/sandcorporation/switchboard/internal/agent/builtin"
)

// PluginDir is the folder an adapter's carried plugin is unpacked under
// (#477): <dir>/plugins/<adapter>, dir being the client's config folder.
func PluginDir(dir string, a agent.Adapter) string {
	return filepath.Join(dir, "plugins", a.Name())
}

// installers lists the registered adapters that carry a plugin.
func installers() []agent.Adapter {
	var out []agent.Adapter
	for _, a := range agent.All() {
		if _, ok := a.(agent.PluginInstaller); ok {
			out = append(out, a)
		}
	}
	return out
}

// PluginInstallCLI installs every carried plugin (`plugin install`, join,
// the first run of a new version) and writes one line per adapter to w: what
// it changed in the agent's settings, that everything already matched, or
// why nothing was done (the agent is not installed, which is not an error).
// quiet leaves out the agents where everything already matched.
func PluginInstallCLI(version, dir string, quiet bool, w io.Writer) error {
	var errs []string
	for _, a := range installers() {
		rep, err := a.(agent.PluginInstaller).InstallPlugin(context.Background(), agent.PluginOptions{Version: version, Dir: PluginDir(dir, a)})
		switch {
		case err != nil:
			errs = append(errs, err.Error())
			fmt.Fprintf(w, "plugin: %s: install failed: %v\n", a.Name(), err)
		case rep.Skipped != "":
			fmt.Fprintf(w, "plugin: %s: %s; nothing installed\n", a.Name(), rep.Skipped)
		case len(rep.Changes) == 0:
			if quiet {
				continue
			}
			fmt.Fprintf(w, "plugin: %s: up to date (%s)\n", a.Name(), rep.Where)
		default:
			fmt.Fprintf(w, "plugin: %s: %s (changed %s)\n", a.Name(), strings.Join(rep.Changes, "; "), rep.Where)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("plugin install: %s", strings.Join(errs, "; "))
	}
	return nil
}

// PluginStatusCLI prints every carried plugin's state (`plugin status`).
func PluginStatusCLI(version, dir string, asJSON bool, w io.Writer) error {
	states := []agent.PluginState{}
	for _, a := range installers() {
		st, err := a.(agent.PluginInstaller).PluginStatus(context.Background(), agent.PluginOptions{Version: version, Dir: PluginDir(dir, a)})
		if err != nil {
			return err
		}
		states = append(states, st)
	}
	if asJSON {
		b, err := json.MarshalIndent(states, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	}
	for _, st := range states {
		if !st.Available {
			fmt.Fprintf(w, "%s: not installed here (wants plugin %s)\n", st.Agent, st.Want)
			continue
		}
		state := "not installed"
		if st.Installed != "" {
			state = "installed " + st.Installed + ", disabled"
			if st.Enabled {
				state = "installed " + st.Installed + ", enabled"
			}
		}
		fmt.Fprintf(w, "%s: plugin %s, wants %s (%s)\n", st.Agent, state, st.Want, st.Where)
		for _, l := range st.Legacy {
			on := "disabled"
			if l.Enabled {
				on = "enabled"
			}
			where := l.Scope
			if l.Project != "" {
				where += " " + l.Project
			}
			fmt.Fprintf(w, "  old %s %s (%s): %s\n", l.ID, l.Version, where, on)
		}
	}
	return nil
}
