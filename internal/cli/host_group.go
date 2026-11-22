package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// HostGroupFunc runs `host group add|set|rm NAME` and `host group list`
// (#435) against the server's database, which the client never opens, so
// the binary supplies it. threshold nil is none.
type HostGroupFunc func(action, name string, threshold *int, jsonOut bool, stdout io.Writer) (int, error)

var hostGroup HostGroupFunc // the host group command, from Run

// cliHostGroup checks `host group ACTION [NAME] [--threshold N|none]`: the
// admin's on the host machine (DB_PATH), else a boss role's through the
// server.
func cliHostGroup(p parsedArgs, stdout io.Writer) (int, error) {
	if len(p.pos) < 2 {
		return 0, usagef("host group requires ACTION (add, set, rm, list)")
	}
	action, name := p.pos[1], ""
	if len(p.pos) > 2 {
		name = p.pos[2]
	}
	if len(p.extra) > 0 {
		return 0, usagef("host group: arguments after -- are for host add")
	}
	tv, hasThreshold := p.values["--threshold"]
	for o := range p.values {
		if o != "--json" && o != "--yes" && (o != "--threshold" || action == "rm" || action == "list") {
			return 0, usagef("host group %s: %s is not one of its options", action, o)
		}
	}
	switch action {
	case "list":
		if name != "" {
			return 0, usagef("host group list: unexpected argument %q", name)
		}
	case "add", "rm":
		if name == "" {
			return 0, usagef("host group %s requires NAME", action)
		}
	case "set":
		if name == "" || !hasThreshold {
			return 0, usagef("host group set requires NAME and --threshold PERCENT|none")
		}
	default:
		return 0, usagef("host group: unknown action %q (add, set, rm, list)", action)
	}
	var threshold *int
	if hasThreshold && tv != "none" {
		n, err := strconv.Atoi(tv)
		if err != nil || n < 0 || n > 100 {
			return 0, usagef("host group %s: --threshold wants 0 to 100 or none, not %q", action, tv)
		}
		threshold = &n
	}
	jsonOut := p.bool("--json")
	if adminHost() {
		if hostGroup == nil {
			return 0, usagef("host group: not available in this binary")
		}
		return hostGroup(action, name, threshold, jsonOut, stdout)
	}
	c, err := loadConfig()
	if err != nil {
		return 0, fmt.Errorf("host group: %w (or, on the host machine, set DB_PATH to name this instance's own database)", err)
	}
	path := "terminals/work-groups"
	switch action {
	case "list":
		b, err := slotCall(c, "GET", "/api/boss/"+path, nil)
		if err != nil {
			return 0, err
		}
		if jsonOut {
			_, err = stdout.Write(b)
			return ExitOK, err
		}
		var list []wire.WorkGroup
		if err := json.Unmarshal(b, &list); err != nil {
			return 0, err
		}
		printWorkGroups(stdout, list)
		return ExitOK, nil
	case "add":
		return bossSend(c, "POST", path, wire.WorkGroup{Name: name, Threshold: threshold}, jsonOut, stdout, "added work group "+name+" ("+thresholdText(threshold)+")")
	case "set":
		return bossSend(c, "PUT", path+"/"+url.PathEscape(name), wire.WorkGroupChange{Threshold: threshold}, jsonOut, stdout, "work group "+name+" threshold "+thresholdText(threshold))
	}
	if !confirmed(p, stdout, "remove work group "+name) {
		return ExitUsage, nil
	}
	return bossSend(c, "DELETE", path+"/"+url.PathEscape(name), nil, jsonOut, stdout, "removed work group "+name)
}

func thresholdText(t *int) string {
	if t == nil {
		return "-"
	}
	return strconv.Itoa(*t) + "%"
}

func printWorkGroups(w io.Writer, list []wire.WorkGroup) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no work groups")
		return
	}
	fmt.Fprintf(w, "%-20s %-9s %s\n", "NAME", "THRESHOLD", "SESSIONS")
	for _, g := range list {
		fmt.Fprintf(w, "%-20s %-9s %d\n", g.Name, thresholdText(g.Threshold), g.Sessions)
	}
}
