package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// HostGrantFunc runs `host grant ROLE …`, `host grant list` and `host
// revoke-grant ROLE` (action grant, grant-list, revoke-grant) against the
// server's database, which the client never opens, so the binary supplies it.
type HostGrantFunc func(action string, g wire.HostGrant, jsonOut bool, stdout io.Writer) (int, error)

var hostGrant HostGrantFunc // the host grant command, from Run

// adminHost reports whether `host` runs the admin's way: on the host machine
// against DB_PATH. Without DB_PATH it runs as the profile's role through the
// server (#369).
func adminHost() bool { return os.Getenv("DB_PATH") != "" }

// roleActions are the host actions a role token may run.
var roleActions = map[string]bool{"add": true, "list": true, "start": true, "stop": true, "remove": true}

// cliHostGrant checks `host grant ROLE --base … --node … --dir-root …`,
// `host grant list` and `host revoke-grant ROLE`, the admin's.
func cliHostGrant(p parsedArgs, stdout io.Writer) (int, error) {
	action, role := p.pos[0], ""
	if len(p.pos) > 1 {
		role = p.pos[1]
	}
	if len(p.pos) > 2 {
		return 0, usagef("host %s: unexpected argument %q", action, p.pos[2])
	}
	if len(p.extra) > 0 {
		return 0, usagef("host %s: arguments after -- are for host add", action)
	}
	grantOpts := map[string]bool{"--base": true, "--node": true, "--dir-root": true, "--adapter": true, "--json": true}
	for o := range p.values {
		if !grantOpts[o] || (o != "--json" && (action == "revoke-grant" || role == "list")) {
			return 0, usagef("host %s: %s is not one of its options", action, o)
		}
	}
	g := wire.HostGrant{Role: role, Bases: p.multi["--base"], Nodes: p.multi["--node"], DirRoots: p.multi["--dir-root"], Adapters: p.multi["--adapter"]}
	switch {
	case role == "":
		return 0, usagef("host %s requires ROLE", action)
	case action == "grant" && role == "list":
		action = "grant-list"
	case action == "grant" && (len(g.Bases) == 0 || len(g.Nodes) == 0 || len(g.DirRoots) == 0):
		return 0, usagef("host grant requires --base, --node and --dir-root")
	}
	if !adminHost() {
		return 0, fmt.Errorf("host %s is the admin's: run it on the host machine with DB_PATH naming this instance's database", p.pos[0])
	}
	if hostGrant == nil {
		return 0, usagef("host %s: not available in this binary", p.pos[0])
	}
	return hostGrant(action, g, p.bool("--json"), stdout)
}

// hostAsRole runs a checked `host ACTION [ID]` as the profile's role through
// the server: add, list, start, stop and remove of the sessions under it,
// within the admin's grant.
//
// A boss role (#399) also makes the dashboard's other terminal actions
// (hostBossActions) through /api/boss/terminals, and add, start, stop and
// remove there when the grant route refuses them (403 or 404).
func hostAsRole(action, id string, add wire.HostedSessionCreate, change wire.HostedSessionChange, p parsedArgs, stdout io.Writer) (int, error) {
	jsonOut := p.bool("--json")
	if !roleActions[action] && !hostBossActions[action] {
		return 0, fmt.Errorf("host %s is the admin's: run it on the host machine with DB_PATH naming this instance's database (a role token can add, list, start, stop and remove)", action)
	}
	c, err := loadConfig()
	if err != nil {
		return 0, fmt.Errorf("host: %w (or, on the host machine, set DB_PATH to name this instance's own database)", err)
	}
	if hostBossActions[action] {
		return hostAsBoss(c, action, id, change, add, p, stdout)
	}
	url := strings.TrimRight(c.Endpoint, "/") + "/api/host/terminals"
	method := http.MethodGet
	var body io.Reader
	switch action {
	case "add":
		method = http.MethodPost
		b, _ := json.Marshal(add)
		body = bytes.NewReader(b)
	case "start", "stop":
		method, url = http.MethodPost, url+"/"+id+"/"+action
	case "remove":
		method, url = http.MethodDelete, url+"/"+id
	}
	var b []byte
	if action == "list" {
		// A boss role lists every record in view (#397); a role that is
		// none, or a server without the view, lists those under it.
		var refused bool
		if b, refused, err = hostBossCall(c, "terminals"); err != nil && !refused {
			return 0, err
		}
	}
	if b == nil {
		if b, err = roleHostCall(c, method, url, body); err != nil {
			if action != "list" && refusal(err) {
				if code, bossErr := hostAsBoss(c, action, id, change, add, p, stdout); !refusal(bossErr) {
					return code, bossErr
				}
			}
			return 0, err
		}
	}
	if jsonOut {
		_, err = stdout.Write(b)
		return 0, err
	}
	if action == "list" {
		var list []wire.HostedSession
		if err := json.Unmarshal(b, &list); err != nil {
			return 0, err
		}
		printRoleHosted(stdout, list)
		return 0, nil
	}
	var h wire.HostedSession
	if err := json.Unmarshal(b, &h); err != nil {
		return 0, err
	}
	switch action {
	case "add":
		fmt.Fprintln(stdout, h.ID)
	case "remove":
		fmt.Fprintf(stdout, "removed %s\n", h.ID)
	default:
		fmt.Fprintf(stdout, "%s %s\n", h.ID, h.Desired)
	}
	return 0, nil
}

// hostAutoAsRole runs `host auto ACTION` with a role token: list is a boss
// role's view (#397); the server refuses every other action (they are the
// admin's) and the refusal is the error.
//
// show, run, pause, resume and rm are a boss role's (#399), through
// /api/boss/terminals.
func hostAutoAsRole(action, ref string, runs int, p parsedArgs, stdout io.Writer) (int, error) {
	jsonOut := p.bool("--json")
	c, err := loadConfig()
	if err != nil {
		return 0, fmt.Errorf("host auto: %w (or, on the host machine, set DB_PATH to name this instance's own database)", err)
	}
	switch action {
	case "show", "run", "pause", "resume", "rm":
		return hostAutoAsBoss(c, action, ref, runs, p, stdout)
	}
	if action == "list" {
		// A role that is no boss (or a server without the view) gets the
		// server's refusal below, as before.
		if b, refused, err := hostBossCall(c, "autos"); err == nil {
			return printBossAutos(b, jsonOut, stdout)
		} else if !refused {
			return 0, err
		}
	}
	_, err = roleHostCall(c, http.MethodGet, strings.TrimRight(c.Endpoint, "/")+"/api/host/auto", nil)
	if err == nil {
		err = fmt.Errorf("host auto: the server did not refuse a role token")
	}
	return 0, err
}

// refusal reports whether err is the server's 403 or 404.
func refusal(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "server returned 403") || strings.Contains(err.Error(), "server returned 404"))
}

func roleHostCall(c Config, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	authorize(c, req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func printRoleHosted(w io.Writer, list []wire.HostedSession) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no hosted sessions")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNODE\tBASE\tCLASS\tLABEL\tDESIRED\tSTATE\tDIRECTORY")
	for _, h := range list {
		label := h.Label
		if label == "" {
			label = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", h.ID, h.Node, h.Base, h.Class, label, h.Desired, h.Runtime.State, h.Directory)
	}
	tw.Flush()
}
