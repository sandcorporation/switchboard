// Package config reads the environment: both the new SWITCHBOARD_* and the
// old AGENT_MAIL_* names, and the profile-directory names.
package config

// Switchboard is the product's name; LegacyConfigDir holds the old one. Every
// environment variable and the profile directory still answer to both names
// (#427: callers outside this repository use them). The new name wins when
// both are set. Old environment names read are listed so they can be removed.

import (
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
)

const (
	EnvPrefix       = "SWITCHBOARD_"
	legacyEnvPrefix = "AGENT_MAIL_"
	HeaderPrefix    = "X-Switchboard-"
	ConfigDirName   = "switchboard"
	LegacyConfigDir = "agent-mail" // kept (#427): dev-kit plugins and shell setups still run the old binary name and use its profile directory
)

// VersionHeaderName is HeaderPrefix + this: the CLI build version every
// client request carries (#308). It has no old-prefix alias; a request
// without it is an older CLI.
const VersionHeaderName = "Version"

// NodeCredentialsHeaderName is HeaderPrefix + this on GET /api/node/socket
// (#343): a node that takes role credentials sends it with the credential
// ids it holds, comma separated, or "-" for none. Without it the host sends
// no credentials and the node's sessions use the profiles their records name.
const NodeCredentialsHeaderName = "Node-Credentials"

// NodeFilesHeaderName is HeaderPrefix + this on GET /api/node/socket
// (#447): a node that answers file frames sends it ("1"). The host sends
// file frames only to such a node; the dashboard shows files as not
// supported on the others.
const NodeFilesHeaderName = "Node-Files"

// NodeFileWriteHeaderName is HeaderPrefix + this on GET /api/node/socket
// (#448): a node that also writes files (wire.FileWrite) sends it ("1").
// The host sends no write to the others; a save there is refused.
const NodeFileWriteHeaderName = "Node-File-Write"

// NodeFileOpsHeaderName is HeaderPrefix + this on GET /api/node/socket: a
// node that also deletes, renames and takes uploads (wire.FileDelete,
// FileRename, FileUpload; #473–#475) sends it ("1"). The host sends those
// to no other node; the dashboard's request is refused (501).
const NodeFileOpsHeaderName = "Node-File-Ops"

// FileFingerprintHeaderName is HeaderPrefix + this (#448): on a whole read
// of a text file (GET …/files/raw, at most wire.FileTextMax) the content's
// wire.FileFingerprint, and on a save (PUT …/files/raw) the one the file is
// expected to have, or "*" to save whatever it is now.
const FileFingerprintHeaderName = "File-Fingerprint"

// LeftHeaderName is HeaderPrefix + this (#470): a CLI that records what an
// answer ending a directive left undone (--left, --left-none) sends it ("1")
// on every request. The server refuses such a client's ending answer that
// says neither; a request without it is an older CLI, whose ending answer is
// taken and leaves an open "unrecorded" item instead.
const LeftHeaderName = "Left"

// CLIMinName is SWITCHBOARD_CLI_MIN without the prefix: the minimum client
// major.minor (#308), read with EnvFrom. Unset or invalid: the server's own.
const CLIMinName = "CLI_MIN"

// PluginName is SWITCHBOARD_PLUGIN without the prefix (#477), read with
// EnvFrom: "off" (or 0, false, no) stops the client and the node from
// installing the agent plugin the binary carries on their own (join, the
// first run of a new version, a hosted session's launch). `plugin install`
// still installs it when asked.
const PluginName = "PLUGIN"

// PluginOff reports whether a SWITCHBOARD_PLUGIN value turns the automatic
// plugin install off.
func PluginOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// ProductName is the only place the product name lives (dashboard, login
// pages, TOTP issuer), so a rename is a one-line change.
const ProductName = "Switchboard"

// oldEnvRead holds the old environment names read since start.
var oldEnvRead = struct {
	sync.Mutex
	names map[string]bool
}{names: map[string]bool{}}

// EnvAlias returns SWITCHBOARD_<name>, else AGENT_MAIL_<name>.
func EnvAlias(name string) string {
	v, _ := EnvAliasFrom(os.Getenv, name)
	return v
}

// EnvAliasFrom reads <name> through getenv under the new prefix, then the old
// one. from is the variable that held the value (the new name when neither
// is set), for messages. Reading an old name records it.
func EnvAliasFrom(getenv func(string) string, name string) (value, from string) {
	if v := getenv(EnvPrefix + name); v != "" {
		return v, EnvPrefix + name
	}
	if v := getenv(legacyEnvPrefix + name); v != "" {
		oldEnvRead.Lock()
		oldEnvRead.names[legacyEnvPrefix+name] = true
		oldEnvRead.Unlock()
		return v, legacyEnvPrefix + name
	}
	return "", EnvPrefix + name
}

// OldEnvNames lists the old environment names read since start, sorted.
func OldEnvNames() []string {
	oldEnvRead.Lock()
	defer oldEnvRead.Unlock()
	out := make([]string, 0, len(oldEnvRead.names))
	for n := range oldEnvRead.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Env returns the environment variable k, else d.
func Env(k, d string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return d
}

// RulesFooterName is SWITCHBOARD_RULES_FOOTER without the prefix (#408):
// "off" stops adding the rules in force to a boss role's directive mail.
const RulesFooterName = "RULES_FOOTER"

// RulesFooterFrom reports whether the directive mail footer is on: every
// value but "off" (the default, empty, included).
func RulesFooterFrom(getenv func(string) string) bool {
	v, _ := EnvFrom(getenv, RulesFooterName)
	return strings.TrimSpace(v) != "off"
}

// HostingName is SWITCHBOARD_HOSTING without the prefix: terminal hosting
// (#173). The operations instance never sets it; a test keeps it out of the
// deployment files.
const HostingName = "HOSTING"

// HostingFrom reports whether terminal hosting is on: only the value "on"
// (under either prefix, new name first) enables it. from names the variable
// that held the value, for messages.
func HostingFrom(getenv func(string) string) (on bool, from string) {
	v, from := EnvAliasFrom(getenv, HostingName)
	return strings.TrimSpace(v) == "on", from
}

// EnvFrom reads SWITCHBOARD_<name> through getenv, with no old-name alias:
// variables added after the rename (hosting, #173) answer only to the new
// name. from is the full variable name, for messages.
func EnvFrom(getenv func(string) string, name string) (value, from string) {
	return getenv(EnvPrefix + name), EnvPrefix + name
}

// HostingAllowChecksName is SWITCHBOARD_HOSTING_ALLOW_CHECKS: "on" lets a
// node run scheduled runs' prechecks (#334), like `node --allow-checks`;
// `up`'s own node reads it. Off by default.
const HostingAllowChecksName = "HOSTING_ALLOW_CHECKS"

// HostingAllowChecks reads SWITCHBOARD_HOSTING_ALLOW_CHECKS through getenv.
func HostingAllowChecks(getenv func(string) string) bool {
	v, _ := EnvFrom(getenv, HostingAllowChecksName)
	return strings.TrimSpace(v) == "on"
}

// DefaultHostingMax is the running-session cap per node when
// SWITCHBOARD_HOSTING_MAX is unset (#190).
const DefaultHostingMax = 4

// Remote nodes (#329): a hosting host may open a second listener for nodes on
// other machines. With SWITCHBOARD_HOSTING_REMOTE_ALLOW set, both sides accept
// only addresses inside its ranges; unset, they accept any address (the node
// channel is authenticated by the node's token).
const (
	// HostingRemoteListenName is the host's second listener (IP:port, unset:
	// none). It serves the node channel and the role API only.
	HostingRemoteListenName = "HOSTING_REMOTE_LISTEN"
	// HostingRemoteAllowName lists the CIDR ranges (comma-separated) the
	// remote listener's address and peers, and a node's --host, must be in.
	// Unset or blank: no limit.
	HostingRemoteAllowName = "HOSTING_REMOTE_ALLOW"
)

// HostingRemoteAllow reads SWITCHBOARD_HOSTING_REMOTE_ALLOW through getenv: a
// CIDR list, or nil (no limit) when unset or blank. from names the variable,
// for messages.
func HostingRemoteAllow(getenv func(string) string) (allow []netip.Prefix, from string, err error) {
	v, from := EnvFrom(getenv, HostingRemoteAllowName)
	if strings.TrimSpace(v) == "" {
		return nil, from, nil
	}
	allow, err = parsePrefixes(v, from)
	return allow, from, err
}

// Browser terminal access (#367): SWITCHBOARD_HOSTING_TERMINAL says where the
// terminal's ticket and socket routes answer, and the optional
// SWITCHBOARD_HOSTING_TERMINAL_ALLOW narrows the client addresses.
const (
	HostingTerminalName      = "HOSTING_TERMINAL"
	HostingTerminalAllowName = "HOSTING_TERMINAL_ALLOW"

	TerminalLoopback = "loopback" // loopback peers only, without proxy forwarding headers
	TerminalOn       = "on"       // any listener and address, for a signed-in admin
	TerminalOff      = "off"      // the routes answer 404

	// DefaultHostingTerminal is the value when SWITCHBOARD_HOSTING_TERMINAL
	// is unset.
	DefaultHostingTerminal = TerminalLoopback
)

// HostingTerminal reads SWITCHBOARD_HOSTING_TERMINAL through getenv:
// loopback, on or off, DefaultHostingTerminal when unset.
func HostingTerminal(getenv func(string) string) (mode, from string, err error) {
	v, from := EnvFrom(getenv, HostingTerminalName)
	switch v = strings.TrimSpace(v); v {
	case "":
		return DefaultHostingTerminal, from, nil
	case TerminalLoopback, TerminalOn, TerminalOff:
		return v, from, nil
	}
	return "", from, fmt.Errorf("%s must be %s, %s or %s (got %q)", from, TerminalLoopback, TerminalOn, TerminalOff, v)
}

// HostingTerminalAllow reads SWITCHBOARD_HOSTING_TERMINAL_ALLOW through
// getenv: a CIDR list, or nil (no limit) when unset or blank.
func HostingTerminalAllow(getenv func(string) string) (allow []netip.Prefix, from string, err error) {
	v, from := EnvFrom(getenv, HostingTerminalAllowName)
	if strings.TrimSpace(v) == "" {
		return nil, from, nil
	}
	allow, err = parsePrefixes(v, from)
	return allow, from, err
}

// parsePrefixes parses a comma-separated CIDR list; from names the variable.
func parsePrefixes(v, from string) (allow []netip.Prefix, err error) {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, perr := netip.ParsePrefix(part)
		if perr != nil {
			return nil, fmt.Errorf("%s: %q is not a CIDR range such as 192.0.2.0/24", from, part)
		}
		allow = append(allow, p.Masked())
	}
	if len(allow) == 0 {
		return nil, fmt.Errorf("%s names no CIDR range", from)
	}
	return allow, nil
}

// AddrAllowed reports whether a is inside one of allow (an IPv4-mapped IPv6
// address counts as its IPv4 address; a zone is ignored).
func AddrAllowed(a netip.Addr, allow []netip.Prefix) bool {
	a = a.Unmap().WithZone("")
	for _, p := range allow {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Node-side hosting variables (#173), read with EnvFrom.
const (
	// HostingAgentName names the agent CLI the node launches (default: the
	// default adapter's executable), resolved through PATH at node start.
	HostingAgentName = "HOSTING_AGENT"
	// HostingAdaptersName is the path of the node's configured agent
	// adapters file (#434; internal/agent/generic); unset, none.
	HostingAdaptersName = "HOSTING_ADAPTERS"
	// HostingMachineName is the machine name given to hosted sessions
	// (default os.Hostname()).
	HostingMachineName = "HOSTING_MACHINE"
	// HostingHistoryName is the scrollback lines kept per session.
	HostingHistoryName = "HOSTING_HISTORY"
	// HostingMaxName caps the sessions a node runs at once (#190, default
	// DefaultHostingMax); the node tells the host in its hello.
	HostingMaxName = "HOSTING_MAX"
	// FakeAgentName set to "1" turns a test binary into the fake agent.
	FakeAgentName = "FAKE_AGENT"
	// FakeAgentDirName is where the fake agent keeps its transcripts.
	FakeAgentDirName = "FAKE_AGENT_DIR"
	// FakeAgentScenesName is the directory of *.vt scenes "draw" paints.
	FakeAgentScenesName = "FAKE_AGENT_SCENES"
)

// Variables a hosted session's child gets from the node (#173). Each is
// EnvPrefix + the name.
const (
	ChildProfileName  = "PROFILE"
	ChildEndpointName = "ENDPOINT"
	ChildMachineName  = "MACHINE"
	ChildLabelName    = "LABEL"
	ChildParentName   = "PARENT"
	TokenName         = "TOKEN"
)

// IsOwnEnv reports whether the environment variable name is one this package
// reads: anything under the new or the old prefix. Windows names are
// case-insensitive, so the match ignores case. Tests use it to keep a
// machine's own setup out of the processes they start (#246).
func IsOwnEnv(name string) bool {
	n := strings.ToUpper(name)
	return strings.HasPrefix(n, EnvPrefix) || strings.HasPrefix(n, legacyEnvPrefix)
}

// WithoutOwnEnv returns environ (KEY=value entries) without the variables
// IsOwnEnv matches.
func WithoutOwnEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if name, _, _ := strings.Cut(kv, "="); !IsOwnEnv(name) {
			out = append(out, kv)
		}
	}
	return out
}

// LegacyEnv returns the old-prefix spelling of name, so callers that must
// strip a variable under both names (a credential kept out of a child) can
// do so without hard-coding the prefix.
func LegacyEnv(name string) string { return legacyEnvPrefix + name }
