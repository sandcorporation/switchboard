package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
)

// The agent plugin the binary carries (#477): `plugin install|status`, the
// install after join, and the install on the first run of a new version.

// pluginMarker is the file in the config folder holding the version that
// last installed the plugin; a run of another version installs it once.
const pluginMarker = "plugin-version"

// pluginLockStale is how old a leftover lock may be before a run takes it
// over (a run killed in the middle of an install).
const pluginLockStale = 2 * time.Minute

// noPluginFlag is --no-plugin (global, or join's).
var noPluginFlag bool

// noAutoPlugin are the commands that never install on their own: the plugin
// command itself, join (it installs after saving the profile), version (the
// plugin's hooks run it under a short limit), and the long-running server
// and node (the node installs per hosted session).
var noAutoPlugin = map[string]bool{"plugin": true, "join": true, "version": true, "up": true, "node": true, "serve": true, "admin": true}

// configDir is the client's config folder: SWITCHBOARD_CONFIG_DIR, else
// <user config>/switchboard.
func configDir() string {
	if d := config.EnvAlias("CONFIG_DIR"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, config.ConfigDirName)
}

// pluginAuto reports whether this run may install the plugin on its own.
func pluginAuto() bool {
	if binary.PluginInstall == nil || noPluginFlag {
		return false
	}
	v, _ := config.EnvFrom(os.Getenv, config.PluginName)
	return !config.PluginOff(v)
}

func cliPlugin(p parsedArgs, stdout io.Writer) (int, error) {
	if binary.PluginInstall == nil || binary.PluginStatus == nil {
		return 0, errors.New("plugin is not available in this build")
	}
	dir := configDir()
	switch p.pos[0] {
	case "install":
		err := binary.PluginInstall(version, dir, false, stdout)
		markPlugin(dir)
		if err != nil {
			return 0, err
		}
		return ExitOK, nil
	case "status":
		if err := binary.PluginStatus(version, dir, p.bool("--json"), stdout); err != nil {
			return 0, err
		}
		return ExitOK, nil
	}
	return 0, usagef("plugin: unknown action %q (install or status)", p.pos[0])
}

// installPluginAfterJoin installs the plugin once join has saved the
// profile. A failure is a line on stderr; join has succeeded.
func installPluginAfterJoin(stderr io.Writer) {
	if !pluginAuto() {
		return
	}
	dir := configDir()
	release, ok := lockPlugin(dir)
	if !ok {
		return
	}
	defer release()
	installQuietly(dir, stderr)
	markPlugin(dir)
}

// syncPluginOnce installs the plugin when the marker names another version
// than this binary's: the first run after the CLI was upgraded. The fast path
// (same version) reads one file. Whatever happens, the command runs next;
// the only output is a line on stderr when something changed or failed.
func syncPluginOnce(stderr io.Writer) {
	if !pluginAuto() {
		return
	}
	dir := configDir()
	if b, err := os.ReadFile(filepath.Join(dir, pluginMarker)); err == nil && strings.TrimSpace(string(b)) == version {
		return
	}
	release, ok := lockPlugin(dir)
	if !ok {
		return // another run is installing it
	}
	defer release()
	installQuietly(dir, stderr)
	// Marked even after a failure, so a broken install does not slow every
	// command; the line says how to retry.
	markPlugin(dir)
}

func installQuietly(dir string, stderr io.Writer) {
	var out bytes.Buffer
	err := binary.PluginInstall(version, dir, true, &out)
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			fmt.Fprintln(stderr, line)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "plugin: run '%s plugin install' to retry\n", progName())
	}
}

func markPlugin(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, pluginMarker), []byte(version+"\n"), 0o600)
}

// lockPlugin takes the install lock in dir; false when another run holds a
// fresh one.
func lockPlugin(dir string) (func(), bool) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false
	}
	lock := filepath.Join(dir, pluginMarker+".lock")
	for range 2 {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lock) }, true
		}
		st, serr := os.Stat(lock)
		if serr != nil || time.Since(st.ModTime()) < pluginLockStale {
			return nil, false
		}
		_ = os.Remove(lock)
	}
	return nil, false
}
