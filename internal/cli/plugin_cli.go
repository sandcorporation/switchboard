package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// The agent plugin the binary carries (#477): `plugin install|status`, the
// install after join, and the install on the first run of a new version.

// pluginMarker is the file in the config folder holding the version that
// last installed the plugin; a run of another version installs it once.
const pluginMarker = "plugin-version"

// pluginAttemptFile holds the last install's wire.PluginAttempt (#483).
const pluginAttemptFile = "plugin-attempt" // no .json: the folder's *.json are profiles

// pluginRetryAfter is how long a failed install of a version waits before
// a run tries it again (#483).
const pluginRetryAfter = 10 * time.Minute

// pluginLockStale is how old a leftover lock may be before a run takes it
// over (a run killed in the middle of an install).
const pluginLockStale = 2 * time.Minute

// noPluginFlag is --no-plugin (global, or join's).
var noPluginFlag bool

// noAutoPlugin are the commands that never install on their own: the plugin
// command itself, join (it installs after saving the profile), version and
// job (the plugin's hooks run them under a short limit: `job ping` in 3
// seconds, which an install outlasts, so a killed run left the lock behind
// and the runs after it skipped, #483), and the long-running server and
// node (the node installs per hosted session).
var noAutoPlugin = map[string]bool{"plugin": true, "join": true, "version": true, "job": true, "up": true, "node": true, "serve": true, "admin": true}

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
// A development build (any version that is not a release tag) never installs
// on its own: the plugin lives in the user's agent settings whatever
// SWITCHBOARD_CONFIG_DIR says, so only `plugin install` may replace it (#498).
func pluginAuto() bool {
	if binary.PluginInstall == nil || noPluginFlag || !isReleaseVersion(version) {
		return false
	}
	v, _ := config.EnvFrom(os.Getenv, config.PluginName)
	return !config.PluginOff(v)
}

var releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

func isReleaseVersion(v string) bool {
	return releaseVersion.MatchString(v) && !strings.HasPrefix(strings.TrimPrefix(v, "v"), "0.0.0-dev")
}

func cliPlugin(p parsedArgs, stdout io.Writer) (int, error) {
	if binary.PluginInstall == nil || binary.PluginStatus == nil {
		return 0, errors.New("plugin is not available in this build")
	}
	dir := configDir()
	switch p.pos[0] {
	case "install":
		var out bytes.Buffer
		err := binary.PluginInstall(version, dir, false, io.MultiWriter(stdout, &out))
		recordPlugin(dir, err, out.String())
		if err != nil {
			return 0, err
		}
		return ExitOK, nil
	case "status":
		if err := binary.PluginStatus(version, dir, p.bool("--json"), pluginSyncOf(dir), stdout); err != nil {
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
	// A failed install of this version waits pluginRetryAfter (#483): it is
	// not marked, so it is tried again, but not on every command.
	if a, ok := lastPluginAttempt(dir); ok && a.Version == version && !a.OK {
		if at, err := time.Parse(time.RFC3339, a.At); err == nil && time.Since(at) < pluginRetryAfter && time.Since(at) > -time.Minute {
			return
		}
	}
	release, ok := lockPlugin(dir)
	if !ok {
		return // another run is installing it
	}
	defer release()
	installQuietly(dir, stderr)
}

// installQuietly installs, prints only what changed or failed, and records
// the attempt: the marker on success only (#483), so a failure is tried
// again after pluginRetryAfter. An agent that is not installed is a
// success (its line says nothing was installed).
func installQuietly(dir string, stderr io.Writer) {
	var out bytes.Buffer
	err := binary.PluginInstall(version, dir, true, &out)
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			fmt.Fprintln(stderr, line)
		}
	}
	recordPlugin(dir, err, out.String())
	if err != nil {
		fmt.Fprintf(stderr, "plugin: tried again in %d minutes; '%s plugin install' retries now\n", int(pluginRetryAfter/time.Minute), progName())
	}
}

// recordPlugin stores an install's attempt, and on success the marker.
func recordPlugin(dir string, err error, out string) {
	a := wire.PluginAttempt{Version: version, At: time.Now().UTC().Format(time.RFC3339), OK: err == nil}
	if err != nil {
		a.Reason = err.Error()
	} else {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.Contains(line, ": up to date") {
				a.Reason = line
				break
			}
		}
	}
	if r := []rune(a.Reason); len(r) > 300 {
		a.Reason = string(r[:300]) + "…"
	}
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	if b, jerr := json.Marshal(a); jerr == nil {
		_ = os.WriteFile(filepath.Join(dir, pluginAttemptFile), append(b, '\n'), 0o600)
	}
	if err == nil {
		markPlugin(dir)
	}
}

// lastPluginAttempt reads the attempt file; false when there is none.
func lastPluginAttempt(dir string) (wire.PluginAttempt, bool) {
	var a wire.PluginAttempt
	b, err := os.ReadFile(filepath.Join(dir, pluginAttemptFile))
	if err != nil || json.Unmarshal(b, &a) != nil || a.Version == "" {
		return wire.PluginAttempt{}, false
	}
	return a, true
}

// pluginSyncOf is what `plugin status` shows of the marker and the last
// attempt in dir.
func pluginSyncOf(dir string) wire.PluginSync {
	var s wire.PluginSync
	if b, err := os.ReadFile(filepath.Join(dir, pluginMarker)); err == nil {
		s.Marked = strings.TrimSpace(string(b))
	}
	if a, ok := lastPluginAttempt(dir); ok {
		s.LastAttempt = &a
	}
	return s
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
