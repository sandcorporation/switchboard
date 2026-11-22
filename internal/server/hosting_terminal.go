package server

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"

	"github.com/sandcorporation/switchboard/internal/config"
)

// Browser terminal access (#367, #366 §1). Every decision about where the
// terminal's ticket and socket routes answer is made here:
//
//   - SWITCHBOARD_HOSTING_TERMINAL (config.HostingTerminal): loopback (the
//     default, config.DefaultHostingTerminal) answers loopback peers that
//     carry no proxy forwarding header; on answers any listener and address;
//     off answers 404 everywhere.
//   - mode on needs admin sign-in to be configured (builtin, oidc or the
//     proxy secret), or hosting does not start; off and loopback need none
//     (terminalStart). This is the only admin-auth condition of the hosting
//     start (hostingStart, #366 §2).
//   - SWITCHBOARD_HOSTING_TERMINAL_ALLOW (optional CIDR list): a client
//     address outside it is refused 403. The client address follows the
//     admin login rule: the peer, or with SWITCHBOARD_TRUST_FORWARDED=1 the
//     address the nearest proxy forwarded (adminauth ClientIP).
//
// The socket also keeps its Origin check (live.go liveOriginAllowed) and
// its single-use ticket (internal/hosting).

// terminalAccess is the terminal setting in force. The zero value is the
// default mode with no address limit.
type terminalAccess struct {
	mode  string         // config.TerminalLoopback, TerminalOn or TerminalOff; "" is the default
	allow []netip.Prefix // nil: no limit
}

// terminalSettings reads SWITCHBOARD_HOSTING_TERMINAL and …_ALLOW.
func terminalSettings(getenv func(string) string) (terminalAccess, error) {
	mode, _, err := config.HostingTerminal(getenv)
	if err != nil {
		return terminalAccess{}, err
	}
	allow, _, err := config.HostingTerminalAllow(getenv)
	if err != nil {
		return terminalAccess{}, err
	}
	return terminalAccess{mode: mode, allow: allow}, nil
}

// terminalStart is hostingStart's check of the terminal settings: valid
// values, and admin sign-in configured when the mode is on. The error names
// every failing condition, joined by "; ".
func terminalStart(getenv func(string) string) error {
	var failed []string
	mode, from, err := config.HostingTerminal(getenv)
	if err != nil {
		failed = append(failed, err.Error())
	}
	if _, _, err := config.HostingTerminalAllow(getenv); err != nil {
		failed = append(failed, err.Error())
	}
	if err == nil && mode == config.TerminalOn && !adminSignInConfigured(getenv) {
		failed = append(failed, from+"="+mode+" needs admin sign-in: set "+config.EnvPrefix+"ADMIN_AUTH (builtin or oidc) or "+config.EnvPrefix+"ADMIN_PROXY_TOKEN")
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

// adminSignInConfigured reports whether some admin sign-in is set: a mode
// that signs admins in, or the transition rule's proxy secret.
func adminSignInConfigured(getenv func(string) string) bool {
	mode := strings.TrimSpace(getenv(config.EnvPrefix + "ADMIN_AUTH"))
	if mode != "" {
		return true // Configure refuses a mode it cannot sign in with
	}
	v, _ := config.EnvAliasFrom(getenv, "ADMIN_PROXY_TOKEN")
	return v != ""
}

// terminalPath reports whether an /api/admin/ path (prefix removed) is the
// terminal's ticket or socket route, or its session folder's files (#447),
// which the terminal setting guards the same way.
func terminalPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "terminals/")
	if !ok {
		return false
	}
	id, sub, ok := strings.Cut(rest, "/")
	return ok && id != "" && (sub == "ticket" || sub == "socket" || sub == "files" || sub == "files/raw")
}

// Terminal refusal reasons, as /api/admin/me reports them ("" serves).
const (
	terminalOff      = "off"      // SWITCHBOARD_HOSTING_TERMINAL=off
	terminalLoopback = "loopback" // loopback only, and this request is not
	terminalAllow    = "allow"    // the client address is outside …_ALLOW
)

// terminalRefusal is why the terminal routes refuse r, or "".
func (s *Server) terminalRefusal(r *http.Request) string {
	switch s.terminal.mode {
	case config.TerminalOff:
		return terminalOff
	case config.TerminalOn:
	default: // loopback, the default
		if !loopbackPeer(r.RemoteAddr) || forwardedRequest(r) {
			return terminalLoopback
		}
	}
	if len(s.terminal.allow) > 0 {
		ip, err := netip.ParseAddr(s.adminAuth().ClientIP(r))
		if err != nil || !config.AddrAllowed(ip, s.terminal.allow) {
			return terminalAllow
		}
	}
	return ""
}

// terminalGate is the status the terminal routes answer r with when refused
// (404 for the mode, 403 for the address list), or 0.
func (s *Server) terminalGate(r *http.Request) int {
	switch s.terminalRefusal(r) {
	case "":
		return 0
	case terminalAllow:
		return http.StatusForbidden
	}
	return http.StatusNotFound
}

func loopbackPeer(remoteAddr string) bool {
	ap, err := netip.ParseAddrPort(remoteAddr)
	return err == nil && ap.Addr().Unmap().IsLoopback()
}
