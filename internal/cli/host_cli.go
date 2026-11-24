package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// cliHost checks `host ACTION [ID] [options] [-- ARGS…]` and hands it to the
// binary's host command.
func cliHost(p parsedArgs, stdout io.Writer) (int, error) {
	action, id := p.pos[0], ""
	if len(p.pos) > 1 {
		id = p.pos[1]
	}
	if action == "auto" {
		return cliHostAuto(p, stdout)
	}
	if action == "grant" || action == "revoke-grant" {
		return cliHostGrant(p, stdout)
	}
	if action == "group" {
		return cliHostGroup(p, stdout)
	}
	if _, ok := p.values["--threshold"]; ok {
		return 0, usagef("host %s: --threshold is for host group", action)
	}
	if _, ok := p.values["--remaining"]; ok && action != "limit" {
		return 0, usagef("host %s: --remaining is for host limit", action)
	}
	if _, ok := p.values["--dir-root"]; ok {
		return 0, usagef("host %s: --dir-root is for host grant", action)
	}
	if len(p.pos) > 2 {
		return 0, usagef("host %s: unexpected argument %q", action, p.pos[2])
	}
	for _, o := range autoOnly {
		if _, ok := p.values[o]; ok {
			return 0, usagef("host %s: %s is for host auto", action, o)
		}
	}
	addOnly := []string{"--node", "--base", "--profile", "--dir", "--label", "--parent", "--restart", "--group", "--adapter", "--ask-permissions", "--create-role", "--another-boss"}
	var skip *bool
	if p.bool("--ask-permissions") {
		skip = new(bool)
	}
	if v, ok := p.values["--skip-permissions"]; ok && action != "set" {
		return 0, usagef("host %s: --skip-permissions is for host set (host add skips by default; --ask-permissions turns it off)", action)
	} else if ok && v != "on" && v != "off" {
		return 0, usagef("host set: --skip-permissions wants on or off, not %q", v)
	} else if ok {
		on := v == "on"
		skip = &on
	}
	switch action {
	case "add":
		if id != "" {
			return 0, usagef("host add: unexpected argument %q", id)
		}
		for _, o := range []string{"--node", "--base", "--profile", "--dir"} {
			if p.values[o] == "" {
				return 0, usagef("host add requires --node, --base, --profile and --dir (missing %s)", o)
			}
		}
	case "set":
		if id == "" {
			return 0, usagef("host set requires ID")
		}
		// With a boss role's token (no DB_PATH) set also changes what the
		// dashboard's session form does: label, parent, group, restart and
		// the agent arguments after --.
		bossSet := map[string]bool{}
		if !adminHost() {
			bossSet = map[string]bool{"--label": true, "--parent": true, "--group": true, "--adapter": true, "--restart": true}
		}
		changes := skip != nil || len(p.extra) > 0 && !adminHost()
		for _, o := range addOnly {
			if _, ok := p.values[o]; ok && !bossSet[o] {
				return 0, usagef("host set: %s is for host add", o)
			} else if ok {
				changes = true
			}
		}
		if adminHost() && skip == nil {
			return 0, usagef("host set requires --skip-permissions on|off")
		}
		if !changes {
			return 0, usagef("host set requires --skip-permissions on|off, or with a boss role's token --label, --parent, --group, --adapter, --restart or -- ARGS")
		}
		if adminHost() && len(p.extra) > 0 {
			return 0, usagef("host set: arguments after -- are for host add")
		}
	case "limit":
		rem, hasRem := p.values["--remaining"]
		switch {
		case strings.Contains(id, "/"):
			if !hasRem {
				return 0, usagef("host limit %s requires --remaining PERCENT|none", id)
			}
			id += "=" + rem
			if _, err := wire.ParseAccountLimit(id); err != nil {
				return 0, usagef("host limit: %v", err)
			}
		case hasRem:
			return 0, usagef("host limit: --remaining is for one account: host limit NODE/ADAPTER[/ACCOUNT] --remaining PERCENT|none")
		case id != "" && id != "on" && id != "off":
			return 0, usagef("host limit: want on, off, NODE/ADAPTER[/ACCOUNT] or nothing, not %q", id)
		}
		for _, o := range addOnly {
			if _, ok := p.values[o]; ok {
				return 0, usagef("host limit: %s is for host add", o)
			}
		}
		if len(p.extra) > 0 {
			return 0, usagef("host limit: arguments after -- are for host add")
		}
	case "list", "nodes", "start", "stop", "restart", "new-conversation", "remove", "events", "log", "revoke-node":
		if (action == "events" || action == "log" || action == "revoke-node") && adminHost() {
			return 0, usagef("host %s: a boss role's action through the server; unset DB_PATH", action)
		}
		listing := action == "list" || action == "nodes"
		if listing && id != "" {
			return 0, usagef("host %s: unexpected argument %q", action, id)
		}
		if !listing && id == "" {
			return 0, usagef("host %s requires ID", action)
		}
		for _, o := range addOnly {
			if _, ok := p.values[o]; ok {
				return 0, usagef("host %s: %s is for host add", action, o)
			}
		}
		if len(p.extra) > 0 {
			return 0, usagef("host %s: arguments after -- are for host add", action)
		}
	default:
		return 0, usagef("host: unknown action %q (add, list, nodes, set, start, stop, restart, new-conversation, remove, events, log, revoke-node, limit, grant, revoke-grant, group)", action)
	}
	add := wire.HostedSessionCreate{
		Node: p.values["--node"], Base: p.values["--base"], Profile: p.values["--profile"], Directory: p.values["--dir"],
		Label: p.values["--label"], Parent: p.values["--parent"], Restart: p.values["--restart"], Group: p.values["--group"], Adapter: p.values["--adapter"], Args: p.extra,
		SkipPermissions: skip, CreateRole: p.bool("--create-role"), AnotherBoss: p.bool("--another-boss"),
	}
	if action == "nodes" {
		// The node list is a boss role's view through the server, with or
		// without DB_PATH (#397).
		return hostNodes(p.bool("--json"), stdout)
	}
	if !adminHost() {
		change := wire.HostedSessionChange{SkipPermissions: skip}
		for o, f := range map[string]**string{"--label": &change.Label, "--parent": &change.Parent, "--group": &change.Group, "--adapter": &change.Adapter, "--restart": &change.Restart} {
			if v, ok := p.values[o]; ok {
				*f = &v
			}
		}
		if len(p.extra) > 0 {
			args := append([]string{}, p.extra...)
			change.Args = &args
		}
		code, err := hostAsRole(action, id, add, change, p, stdout)
		return code, addRetry(action, err, p)
	}
	code, err := host(action, id, add, p.bool("--json"), stdout)
	return code, addRetry(action, err, p)
}

// addRetry adds to a refused `host add` the command that goes on (#562):
// with --create-role for a base that is not a role, with --another-boss for
// a boss base.
func addRetry(action string, err error, p parsedArgs) error {
	if action != "add" || err == nil {
		return err
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "host add --create-role registers it") && !p.bool("--create-role"):
		return fmt.Errorf("%w\nto register it with the session: %s", err, addCommand(p, "--create-role"))
	case strings.Contains(msg, "is a boss role; for a session of another role") && !p.bool("--another-boss"):
		return fmt.Errorf("%w\nanother role: %s\none more boss session: %s", err, addCommand(p, "--create-role", "<new role>"), addCommand(p, "--another-boss"))
	}
	return err
}

// addCommand is the `host add` p gives with flag added; base, when given,
// replaces --base.
func addCommand(p parsedArgs, flag string, base ...string) string {
	parts := []string{progName(), "host", "add"}
	for _, o := range []string{"--node", "--base", "--profile", "--dir", "--label", "--parent", "--restart", "--group", "--adapter"} {
		if o == "--base" && len(base) > 0 {
			parts = append(parts, o, base[0]) // a placeholder, as written
		} else if v, ok := p.values[o]; ok {
			parts = append(parts, o, shellWord(v))
		}
	}
	for _, o := range []string{"--ask-permissions", "--create-role", "--another-boss"} {
		if p.bool(o) || o == flag {
			parts = append(parts, o)
		}
	}
	if len(p.extra) > 0 {
		parts = append(parts, "--")
		for _, a := range p.extra {
			parts = append(parts, shellWord(a))
		}
	}
	return strings.Join(parts, " ")
}

// shellWord quotes v for a POSIX shell unless it needs none.
func shellWord(v string) string {
	if v != "" && strings.Trim(v, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=@,+-") == "" {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// autoOnly are the options of `host auto` alone.
var autoOnly = []string{"--cron", "--prompt", "--resume", "--notify", "--max-run", "--runs", "--check", "--check-timeout"}

// cliHostAuto checks `host auto ACTION [REF] [options]` and hands it to the
// binary's host auto command.
func cliHostAuto(p parsedArgs, stdout io.Writer) (int, error) {
	if len(p.pos) < 2 {
		return 0, usagef("host auto requires ACTION (add, list, show, edit, run, pause, resume, rm)")
	}
	action, ref := p.pos[1], ""
	if len(p.pos) > 2 {
		ref = p.pos[2]
	}
	if len(p.extra) > 0 {
		return 0, usagef("host auto: arguments after -- are for host add")
	}
	for _, o := range []string{"--node", "--base", "--profile", "--dir", "--parent", "--restart", "--group", "--threshold", "--remaining", "--adapter", "--ask-permissions", "--skip-permissions", "--create-role", "--another-boss"} {
		if _, ok := p.values[o]; ok {
			return 0, usagef("host auto: %s is for host add", o)
		}
	}
	allowed := map[string]bool{}
	switch action {
	case "add":
		if ref == "" {
			return 0, usagef("host auto add requires the hosted session ID")
		}
		if p.values["--cron"] == "" || p.values["--prompt"] == "" {
			return 0, usagef("host auto add requires --cron and --prompt")
		}
		for _, o := range []string{"--cron", "--prompt", "--label", "--resume", "--notify", "--max-run", "--check", "--check-timeout"} {
			allowed[o] = true
		}
	case "list":
		if ref != "" {
			return 0, usagef("host auto list: unexpected argument %q", ref)
		}
	case "edit":
		if ref == "" {
			return 0, usagef("host auto edit requires AUTO-ID")
		}
		given := false
		for _, o := range []string{"--cron", "--prompt", "--label"} {
			_, ok := p.values[o]
			allowed[o], given = true, given || ok
		}
		if !given {
			return 0, usagef("host auto edit needs --cron, --prompt or --label")
		}
	case "show", "run", "pause", "resume", "rm":
		if ref == "" {
			return 0, usagef("host auto %s requires AUTO-ID", action)
		}
		allowed["--runs"] = action == "show"
	default:
		return 0, usagef("host auto: unknown action %q (add, list, show, edit, run, pause, resume, rm)", action)
	}
	for _, o := range append([]string{"--label"}, autoOnly...) {
		if _, ok := p.values[o]; ok && !allowed[o] {
			return 0, usagef("host auto %s: %s is not one of its options", action, o)
		}
	}
	runs := 20
	if v, ok := p.values["--runs"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			return 0, usagef("host auto show: --runs wants 1 to 500, not %q", v)
		}
		runs = n
	}
	c := wire.HostAutoCreate{Cron: p.values["--cron"], Prompt: p.values["--prompt"], Label: p.values["--label"], Resume: p.bool("--resume")}
	if v, ok := p.values["--max-run"]; ok {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > 7*24*time.Hour {
			return 0, usagef("host auto add: --max-run wants a duration from 1m to 168h, not %q", v)
		}
		c.MaxRunMS = d.Milliseconds()
	}
	c.Check = p.values["--check"]
	if v, ok := p.values["--check-timeout"]; ok {
		d, err := time.ParseDuration(v)
		if err != nil || d.Milliseconds() < wire.MinCheckTimeoutMS || d.Milliseconds() > wire.MaxCheckTimeoutMS {
			return 0, usagef("host auto add: --check-timeout wants 1s to 10m, not %q", v)
		}
		if c.Check == "" {
			return 0, usagef("host auto add: --check-timeout needs --check")
		}
		c.CheckTimeoutMS = d.Milliseconds()
	}
	for _, r := range p.multi["--notify"] {
		for _, role := range strings.Split(r, ",") {
			if role = strings.TrimSpace(role); role != "" {
				c.Notify = append(c.Notify, role)
			}
		}
	}
	if !adminHost() {
		return hostAutoAsRole(action, ref, runs, p, stdout)
	}
	if hostAuto == nil {
		return 0, usagef("host auto: not available in this binary")
	}
	return hostAuto(action, ref, c, autoEditOf(p), runs, p.bool("--json"), stdout)
}
