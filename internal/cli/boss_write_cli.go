package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The boss role's writes (#399): the dashboard's write actions with the
// profile's token, at /api/boss/<admin path>. A global boss writes as the
// dashboard admin, a group boss inside its groups; the server records each
// write. Writes that are hard to undo need --yes.

// optYes is the confirmation of a write that is hard to undo.
var optYes = option{"--yes", "", "confirm a write that is hard to undo (without it the command says what it would do and exits 2)"}

// bossSend makes a boss write and prints done, or the server's JSON with
// jsonOut.
func bossSend(c Config, method, path string, body any, jsonOut bool, stdout io.Writer, done string) (int, error) {
	b, err := slotCall(c, method, "/api/boss/"+path, body)
	if err != nil {
		return 0, err
	}
	if jsonOut {
		_, err = fmt.Fprintln(stdout, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	_, err = fmt.Fprintln(stdout, done)
	return ExitOK, err
}

// confirmed reports whether --yes was given; without it it prints what the
// write would do and the caller exits ExitUsage.
func confirmed(p parsedArgs, stdout io.Writer, would string) bool {
	if p.bool("--yes") {
		return true
	}
	fmt.Fprintf(stdout, "would %s; add --yes to do it\n", would)
	return false
}

// escapePath escapes each segment of a name that may hold slashes (a role,
// a question setting's target).
func escapePath(name string) string {
	parts := strings.Split(name, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func positiveID(cmd, v string) (string, error) {
	if n, err := strconv.ParseInt(v, 10, 64); err != nil || n <= 0 {
		return "", usagef("%s: invalid ID %q", cmd, v)
	}
	return v, nil
}

// mailClose runs `mail close ID`: close a directive as the dashboard does.
func mailClose(c Config, id string, jsonOut bool) (int, error) {
	id, err := positiveID("mail close", id)
	if err != nil {
		return 0, err
	}
	return bossSend(c, "POST", "messages/"+id+"/close", nil, jsonOut, os.Stdout, "closed #"+id)
}

// jobsWrite runs `jobs show ID`, `jobs stop ID`, `jobs rm ID` and
// `jobs rm-schedule ID`.
func jobsWrite(c Config, p parsedArgs) (int, error) {
	if len(p.pos) != 2 {
		return 0, usagef("jobs %s requires ID", p.pos[0])
	}
	id, err := positiveID("jobs "+p.pos[0], p.pos[1])
	if err != nil {
		return 0, err
	}
	jsonOut := p.bool("--json")
	switch p.pos[0] {
	case "show":
		// The command opens one job at a time; the server records the read.
		b, err := bossView(c, "jobs/"+id, jsonOut)
		if b == nil || err != nil {
			return ExitOK, err
		}
		var j wire.Job
		if err := json.Unmarshal(b, &j); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		printJob(j)
		return ExitOK, nil
	case "stop":
		return bossSend(c, "POST", "jobs/"+id+"/stop", nil, jsonOut, os.Stdout, "stop requested for job #"+id)
	case "rm":
		if !confirmed(p, os.Stdout, "remove the record of ended job #"+id) {
			return ExitUsage, nil
		}
		return bossSend(c, "DELETE", "jobs/"+id, nil, jsonOut, os.Stdout, "removed job #"+id)
	case "rm-schedule":
		if !confirmed(p, os.Stdout, "remove schedule #"+id) {
			return ExitUsage, nil
		}
		return bossSend(c, "DELETE", "schedules/"+id, nil, jsonOut, os.Stdout, "removed schedule #"+id)
	}
	return 0, usagef("jobs: unknown action %q (show, stop, rm, rm-schedule)", p.pos[0])
}

// rolesWrite runs `roles revoke ROLE` and `roles restore ROLE`.
func rolesWrite(c Config, p parsedArgs) (int, error) {
	if len(p.pos) != 2 || p.pos[1] == "" {
		return 0, usagef("roles %s requires ROLE", p.pos[0])
	}
	role, jsonOut := p.pos[1], p.bool("--json")
	switch p.pos[0] {
	case "revoke":
		would := "revoke " + role + ": its token and every session on it stop working"
		if use := recentRoleUse(c, role); use != "" {
			would += "; " + use
		}
		if !confirmed(p, os.Stdout, would) {
			return ExitUsage, nil
		}
		return bossSend(c, "POST", "roles/"+escapePath(role)+"/revoke?force=1", nil, jsonOut, os.Stdout, "revoked "+role)
	case "restore":
		return bossSend(c, "POST", "roles/"+escapePath(role)+"/restore", nil, jsonOut, os.Stdout, "restored "+role)
	}
	return 0, usagef("roles: unknown action %q (revoke, restore)", p.pos[0])
}

// settingsWrite runs `settings set TARGET` (options left out keep the
// current value) and `settings rm TARGET`.
func settingsWrite(c Config, p parsedArgs) (int, error) {
	if len(p.pos) != 2 || p.pos[1] == "" {
		return 0, usagef("settings %s requires TARGET (a role, PREFIX/* or *)", p.pos[0])
	}
	target, jsonOut := p.pos[1], p.bool("--json")
	switch p.pos[0] {
	case "rm":
		if !confirmed(p, os.Stdout, "remove the question setting of "+target) {
			return ExitUsage, nil
		}
		return bossSend(c, "DELETE", "question-settings/"+escapePath(target), nil, jsonOut, os.Stdout, "removed the question setting of "+target)
	case "set":
	default:
		return 0, usagef("settings: unknown action %q (set, rm)", p.pos[0])
	}
	onOff := func(flag string, cur bool) (bool, error) {
		v, ok := p.values[flag]
		switch {
		case !ok:
			return cur, nil
		case v == "on":
			return true, nil
		case v == "off":
			return false, nil
		}
		return false, usagef("settings set: %s wants on or off, not %q", flag, v)
	}
	b, err := slotCall(c, "GET", "/api/boss/settings", nil)
	if err != nil {
		return 0, err
	}
	var v bossSettingsView
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	body := struct {
		CanAsk   bool     `json:"can_ask"`
		Watchers []string `json:"watchers"`
		Discord  bool     `json:"discord"`
	}{Watchers: []string{}}
	for _, q := range v.QuestionSettings {
		if q.Target == target {
			body.CanAsk, body.Discord = q.CanAsk, q.Discord
			body.Watchers = append(body.Watchers, q.Watchers...)
		}
	}
	if body.CanAsk, err = onOff("--can-ask", body.CanAsk); err != nil {
		return 0, err
	}
	if body.Discord, err = onOff("--discord", body.Discord); err != nil {
		return 0, err
	}
	if w, ok := p.values["--watchers"]; ok {
		body.Watchers = []string{}
		for _, r := range strings.Split(w, ",") {
			if r = strings.TrimSpace(r); r != "" && r != "-" {
				body.Watchers = append(body.Watchers, r)
			}
		}
	}
	return bossSend(c, "PUT", "question-settings/"+escapePath(target), body, jsonOut, os.Stdout,
		fmt.Sprintf("%s  can_ask=%t discord=%t watchers=%s", target, body.CanAsk, body.Discord, dash(strings.Join(body.Watchers, ","))))
}

// hostBossActions are the host actions a boss role makes through
// /api/boss/terminals when no DB_PATH names the database.
var hostBossActions = map[string]bool{"set": true, "restart": true, "new-conversation": true, "limit": true, "events": true, "log": true, "revoke-node": true}

// hostAsBoss runs a host action with a boss role's token: the dashboard's
// terminal writes and reads that are no list.
func hostAsBoss(c Config, action, id string, change wire.HostedSessionChange, add wire.HostedSessionCreate, p parsedArgs, stdout io.Writer) (int, error) {
	jsonOut := p.bool("--json")
	term := "terminals/" + url.PathEscape(id)
	switch action {
	case "add":
		return bossSend(c, "POST", "terminals", add, jsonOut, stdout, "added a session of "+add.Base)
	case "start", "stop":
		desired := map[string]string{"start": "running", "stop": "stopped"}[action]
		return bossSend(c, "PUT", term, wire.HostedSessionChange{Desired: &desired}, jsonOut, stdout, id+" "+desired)
	case "set":
		return bossSend(c, "PUT", term, change, jsonOut, stdout, "changed "+id)
	case "restart":
		return bossSend(c, "POST", term+"/restart", nil, jsonOut, stdout, "restarted "+id)
	case "new-conversation":
		if !confirmed(p, stdout, "start "+id+" in a new conversation (the current one is left)") {
			return ExitUsage, nil
		}
		return bossSend(c, "POST", term+"/new-conversation", nil, jsonOut, stdout, id+" in a new conversation")
	case "remove":
		if !confirmed(p, stdout, "remove hosted session "+id) {
			return ExitUsage, nil
		}
		return bossSend(c, "DELETE", term, nil, jsonOut, stdout, "removed "+id)
	case "revoke-node":
		if !confirmed(p, stdout, "revoke node "+id+": it is cut off at once and its token never works again") {
			return ExitUsage, nil
		}
		return bossSend(c, "DELETE", "nodes/"+url.PathEscape(id), nil, jsonOut, stdout, "revoked node "+id)
	case "limit":
		if id == "" {
			b, err := slotCall(c, "GET", "/api/boss/terminals/limit", nil)
			if err != nil {
				return 0, err
			}
			_, err = fmt.Fprintln(stdout, strings.TrimSpace(string(b)))
			return ExitOK, err
		}
		if strings.Contains(id, "/") {
			a, err := wire.ParseAccountLimit(id)
			if err != nil {
				return 0, usagef("host limit: %v", err)
			}
			key, val, _ := strings.Cut(id, "=")
			return bossSend(c, "PUT", "terminals/limit/accounts", a, jsonOut, stdout, "remaining limit of "+key+" set to "+val)
		}
		return bossSend(c, "PUT", "terminals/limit", map[string]bool{"limited": id == "on"}, jsonOut, stdout, "subscription limit "+id)
	case "events", "log":
		b, err := slotCall(c, "GET", "/api/boss/"+term+"/"+action, nil)
		if err != nil {
			return 0, err
		}
		_, err = fmt.Fprintln(stdout, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	return 0, usagef("host %s: not a boss action", action)
}

// hostAutoAsBoss runs `host auto show|run|pause|resume|rm AUTO-ID` with a
// boss role's token; the session is found in the boss view of the runs.
func hostAutoAsBoss(c Config, action, ref string, runs int, p parsedArgs, stdout io.Writer) (int, error) {
	b, err := slotCall(c, "GET", "/api/boss/autos", nil)
	if err != nil {
		return 0, err
	}
	var list []wire.HostAuto
	if err := json.Unmarshal(b, &list); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	session := ""
	for _, a := range list {
		if strconv.FormatInt(a.ID, 10) == ref {
			session = a.Session
		}
	}
	if session == "" {
		return 0, fmt.Errorf("host auto %s: no scheduled run %s in view", action, ref)
	}
	path := "terminals/" + url.PathEscape(session) + "/autos/" + url.PathEscape(ref)
	jsonOut := p.bool("--json")
	switch action {
	case "show":
		b, err := slotCall(c, "GET", "/api/boss/"+path+"?runs="+strconv.Itoa(runs), nil)
		if err != nil {
			return 0, err
		}
		_, err = fmt.Fprintln(stdout, strings.TrimSpace(string(b)))
		return ExitOK, err
	case "rm":
		if !confirmed(p, stdout, "remove scheduled run "+ref+" and its run history") {
			return ExitUsage, nil
		}
		return bossSend(c, "DELETE", path, nil, jsonOut, stdout, "removed scheduled run "+ref)
	default:
		return bossSend(c, "POST", path+"/"+action, nil, jsonOut, stdout, "scheduled run "+ref+" "+action)
	}
}

// recentRoleUse says when and where role last reported if that was within
// 24 hours, else "" (also when the list cannot be read: the server still
// refuses a revoke of a role in use without force).
func recentRoleUse(c Config, role string) string {
	b, err := slotCall(c, "GET", "/api/boss/roles", nil)
	if err != nil {
		return ""
	}
	var out struct {
		Roles []struct {
			Role        string `json:"role"`
			LastUsedAt  string `json:"last_used_at"`
			LastMachine string `json:"last_machine"`
		} `json:"roles"`
	}
	if json.Unmarshal(b, &out) != nil {
		return ""
	}
	for _, v := range out.Roles {
		if v.Role != role || v.LastUsedAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, v.LastUsedAt)
		if err != nil || time.Since(at) > 24*time.Hour {
			return ""
		}
		return "last used " + v.LastUsedAt + " on " + v.LastMachine
	}
	return ""
}
