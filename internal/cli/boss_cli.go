package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The boss role's read-only views (#397): GET /api/boss/<view> with the
// profile's token, the dashboard's mail, jobs, terminal, role and settings
// tabs. The writes are in boss_write_cli.go (#399).

// bossView reads GET /api/boss/<path>; with jsonOut it prints the body as the
// server sent it and returns nil.
func bossView(c Config, path string, jsonOut bool) ([]byte, error) {
	b, err := slotCall(c, "GET", "/api/boss/"+path, nil)
	if err != nil {
		return nil, err
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(b)))
		return nil, nil
	}
	return b, nil
}

// cliMail runs `mail list` and `mail show ID`.
func cliMail(c Config, p parsedArgs) (int, error) {
	action, id := p.pos[0], ""
	if len(p.pos) > 1 {
		id = p.pos[1]
	}
	jsonOut := p.bool("--json")
	switch action {
	case "list":
		if id != "" {
			return 0, usagef("mail list: unexpected argument %q", id)
		}
		q := url.Values{}
		if p.bool("--open") {
			q.Set("open", "1")
		}
		for _, o := range []struct{ flag, param string }{{"--from", "from"}, {"--to", "to"}, {"--limit", "limit"}} {
			if v := p.values[o.flag]; v != "" {
				q.Set(o.param, v)
			}
		}
		if v := p.values["--since"]; v != "" {
			d, err := parseDays(v)
			if err != nil || d <= 0 {
				return 0, usagef("mail list: --since wants a duration such as 90m, 24h or 7d, not %q", v)
			}
			q.Set("since", time.Now().Add(-d).UTC().Format(time.RFC3339))
		}
		path := "messages"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		b, err := bossView(c, path, jsonOut)
		if b == nil || err != nil {
			return ExitOK, err
		}
		var list []wire.Message
		if err := json.Unmarshal(b, &list); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		_, err = io.WriteString(os.Stdout, formatMailList(list))
		return ExitOK, err
	case "show":
		if n, e := strconv.ParseInt(id, 10, 64); e != nil || n <= 0 {
			return 0, usagef("mail show: invalid message ID %q", id)
		}
		for _, o := range []string{"--open", "--from", "--to", "--since", "--limit"} {
			if _, ok := p.values[o]; ok {
				return 0, usagef("mail show: %s is for mail list", o)
			}
		}
		b, err := bossView(c, "messages/"+id, jsonOut)
		if b == nil || err != nil {
			return ExitOK, err
		}
		var m any
		if err := json.Unmarshal(b, &m); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		prettyTo(os.Stdout, m)
		return ExitOK, nil
	case "close":
		return mailClose(c, id, jsonOut)
	}
	return 0, usagef("mail: unknown action %q (list, show ID, close ID)", action)
}

// formatMailList is one line per message, newest first: ID, state, sender →
// recipient, time and subject (bodies stay in mail show).
func formatMailList(ms []wire.Message) string {
	if len(ms) == 0 {
		return "no mail\n"
	}
	var b strings.Builder
	for _, m := range ms {
		mark := ""
		if m.Priority == "urgent" {
			mark = "[urgent] "
		}
		fmt.Fprintf(&b, "#%d %s · %s → %s · %s · %s%s\n", m.ID, m.Status, m.Sender, m.Recipient, m.CreatedAt.Local().Format("01-02 15:04"), mark, m.Subject)
	}
	return b.String()
}

// bossJobsView is GET /api/boss/jobs.
type bossJobsView struct {
	Jobs      []wire.Job      `json:"jobs"`
	Schedules []wire.Schedule `json:"schedules"`
}

// cliJobs runs `jobs`: the server's job ledger and schedules of every role in
// view.
func cliJobs(c Config, p parsedArgs) (int, error) {
	if len(p.pos) > 0 {
		return jobsWrite(c, p)
	}
	path := "jobs"
	if s := p.values["--state"]; s != "" {
		path += "?" + url.Values{"state": {s}}.Encode()
	}
	b, err := bossView(c, path, p.bool("--json"))
	if b == nil || err != nil {
		return ExitOK, err
	}
	var v bossJobsView
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if len(v.Jobs) == 0 {
		fmt.Println("no jobs")
	}
	for _, j := range v.Jobs {
		fmt.Printf("#%d  %s  %s@%s  %s\n", j.ID, stateText(j), j.Role, j.Machine, j.Label)
	}
	if len(v.Schedules) > 0 {
		fmt.Println("\nschedules:")
	}
	for _, s := range v.Schedules {
		fmt.Printf("#%d  %s  %s  %s  %s\n", s.ID, scheduleState(s), s.Role, s.Cron, s.Label)
	}
	return ExitOK, nil
}

// bossRolesView is GET /api/boss/roles.
type bossRolesView struct {
	Roles []struct {
		Role      string  `json:"role"`
		RevokedAt *string `json:"revoked_at"`
		RetiredAt *string `json:"retired_at"`
		Group     string  `json:"group"`
	} `json:"roles"`
	Slots  []wire.SlotView `json:"slots"`
	Groups []struct {
		Prefix    string  `json:"prefix"`
		BossRole  string  `json:"boss_role"`
		RevokedAt *string `json:"revoked_at"`
	} `json:"groups"`
}

// cliRoles runs `roles`: roles with their group and whether revoked, the
// leased numbered slots and the groups.
func cliRoles(c Config, p parsedArgs) (int, error) {
	if len(p.pos) > 0 {
		return rolesWrite(c, p)
	}
	b, err := bossView(c, "roles", p.bool("--json"))
	if b == nil || err != nil {
		return ExitOK, err
	}
	var v bossRolesView
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ROLE\tGROUP\tSTATE")
	for _, r := range v.Roles {
		state := revokedText(r.RevokedAt)
		if r.RetiredAt != nil {
			state = "retired"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Role, dash(r.Group), state)
	}
	if len(v.Slots) > 0 {
		fmt.Fprintln(tw, "\nSLOT\tMACHINE\tDIRECTORY")
		for _, s := range v.Slots {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Role, s.Machine, s.Directory)
		}
	}
	if len(v.Groups) > 0 {
		fmt.Fprintln(tw, "\nGROUP\tBOSS ROLE\tSTATE")
		for _, g := range v.Groups {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", g.Prefix, g.BossRole, revokedText(g.RevokedAt))
		}
	}
	return ExitOK, tw.Flush()
}

func revokedText(at *string) string {
	if at != nil {
		return "revoked"
	}
	return "active"
}

// bossSettingsView is GET /api/boss/settings; the server summary is printed
// as the server sent it.
type bossSettingsView struct {
	QuestionSettings []struct {
		Target   string   `json:"target"`
		CanAsk   bool     `json:"can_ask"`
		Watchers []string `json:"watchers"`
		Discord  bool     `json:"discord"`
	} `json:"question_settings"`
	QuestionRefusals []wire.QuestionRefusal `json:"question_refusals"`
	Server           json.RawMessage        `json:"server"`
	AdminAuth        string                 `json:"admin_auth"`
	Secrets          []struct {
		Name string `json:"name"`
		Set  bool   `json:"set"`
	} `json:"secrets"`
}

// cliSettings runs `settings`: question settings, recent refusals and, for a
// global boss, the server summary and the secret settings by name (never
// their values: the server does not send them).
func cliSettings(c Config, p parsedArgs) (int, error) {
	if len(p.pos) > 0 {
		return settingsWrite(c, p)
	}
	b, err := bossView(c, "settings", p.bool("--json"))
	if b == nil || err != nil {
		return ExitOK, err
	}
	var v bossSettingsView
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Println("questions:")
	for _, q := range v.QuestionSettings {
		fmt.Printf("  %s  can_ask=%t discord=%t watchers=%s\n", q.Target, q.CanAsk, q.Discord, dash(strings.Join(q.Watchers, ",")))
	}
	if len(v.QuestionRefusals) > 0 {
		fmt.Println("refused questions (recent):")
		for _, r := range v.QuestionRefusals {
			fmt.Printf("  %s  %s ×%d  ask via %s\n", r.At, r.Role, r.Count, dash(r.AskVia))
		}
	}
	if len(v.Server) > 0 && string(v.Server) != "null" {
		fmt.Println("server:")
		fmt.Printf("  %s\n", strings.TrimSpace(string(v.Server)))
		fmt.Printf("  %s\n", v.AdminAuth)
	}
	for _, s := range v.Secrets {
		state := "not set"
		if s.Set {
			state = "set"
		}
		fmt.Printf("  %s: %s\n", s.Name, state)
	}
	return ExitOK, nil
}

// hostBossCall reads a boss view of the terminal tab (#397). refused is true
// when the server answered 403 or 404 (not a boss role, hosting off, or a
// server older than the view), so the caller can fall back.
func hostBossCall(c Config, view string) (b []byte, refused bool, err error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(c.Endpoint, "/")+"/api/boss/"+view, nil)
	if err != nil {
		return nil, false, err
	}
	authorize(c, req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	b, _ = io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return nil, true, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	case resp.StatusCode >= 300:
		return nil, false, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return b, false, nil
}

// errNoBossView is hostBossCall's refusal when nothing falls back.
var errNoBossView = errors.New("boss roles only")

// hostNodes runs `host nodes` with the profile's token: the nodes, all of
// them for a global boss, those running a session in view for a group boss.
func hostNodes(jsonOut bool, stdout io.Writer) (int, error) {
	c, err := loadConfig()
	if err != nil {
		return 0, fmt.Errorf("host nodes: %w", err)
	}
	b, _, err := hostBossCall(c, "nodes")
	if err != nil {
		return 0, fmt.Errorf("host nodes (%w): %v", errNoBossView, err)
	}
	if jsonOut {
		_, err = stdout.Write(b)
		return 0, err
	}
	var list []wire.NodeView
	if err := json.Unmarshal(b, &list); err != nil {
		return 0, err
	}
	if len(list) == 0 {
		fmt.Fprintln(stdout, "no nodes")
		return 0, nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tMACHINE\tOS\tVERSION\tLAST SEEN\tSTATE")
	for _, n := range list {
		seen, state := "-", "active"
		if n.LastSeenAt != nil {
			seen = time.UnixMilli(*n.LastSeenAt).Local().Format("2006-01-02 15:04")
		}
		if n.RevokedAt != nil {
			state = "revoked"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", n.Name, dash(n.Machine), dash(n.OS), dash(n.Version), seen, state)
	}
	return 0, tw.Flush()
}

// printBossAutos prints GET /api/boss/autos: the scheduled runs of the
// sessions in view (`host auto list` with a boss role's token).
func printBossAutos(b []byte, jsonOut bool, stdout io.Writer) (int, error) {
	if jsonOut {
		_, err := stdout.Write(b)
		return 0, err
	}
	var list []wire.HostAuto
	if err := json.Unmarshal(b, &list); err != nil {
		return 0, err
	}
	if len(list) == 0 {
		fmt.Fprintln(stdout, "no scheduled runs")
		return 0, nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSESSION\tCRON\tTZ\tSTATE\tLABEL")
	for _, a := range list {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Session, a.Cron, a.TZ, a.State, dash(a.Label))
	}
	return 0, tw.Flush()
}
