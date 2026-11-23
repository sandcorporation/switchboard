package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/cron"
	"github.com/sandcorporation/switchboard/internal/wire"
)

var scheduleCmd = command{name: "schedule", args: "ACTION [ID]", nargs: 2, optArgs: true,
	synopsis: "(ACTION: add | list | show ID | rm ID | pause ID | resume ID)",
	summary:  "Have the server mail this role a prompt at times; fires while no session is attached are merged into one mail. A boss role lists, shows (the read is recorded), pauses and resumes the schedules of roles in view; another role's prompt is only in show. A paused schedule drops the times that pass and its expiry waits; the owner is told when someone else pauses or resumes it",
	options: []option{
		{"--cron", "EXPR", "add: five-field cron (minute hour day-of-month month day-of-week) in this machine's time zone"},
		{"--at", "TIME", "add: once at TIME (RFC 3339, YYYY-MM-DD HH:MM, or HH:MM today or tomorrow, local time)"},
		{"--in", "DURATION", "add: once after DURATION, e.g. 20m, 2h, 1d"},
		{"--prompt", "TEXT", "add: the prompt each fire mails"},
		{"--expires", "DURATION", "add: when it ends unless a session of the role is attached, e.g. 7d (default 7d)"},
		{"--label", "TEXT", "add: label (default 예약 #ID)"},
		{"--no-extend", "", "add: the expiry is fixed; it does not move forward while a session is attached"},
		{"--idem", "KEY", "add: retry-safe key; adding again with the same key returns the first schedule"},
		{"--state", "STATE", "list: only schedules in this state (active, paused, ended, expired; live: active or paused)"},
		{"--owner", "ROLE", "list: only this role's schedules (boss roles)"},
		{"--reason", "TEXT", "pause: why, shown with who paused it"},
		{"--fire", "", "resume: a once schedule whose time passed while paused sends its prompt now"},
		{"--drop", "", "resume: a once schedule whose time passed while paused drops it and ends"},
		optJSON, optProfile,
	}}

func init() { commands = append(commands, scheduleCmd) }

func cliSchedule(c Config, p parsedArgs) (int, error) {
	action := p.pos[0]
	var id int64
	if action == "show" || action == "rm" || action == "pause" || action == "resume" {
		if len(p.pos) < 2 {
			return 0, usagef("schedule %s needs a schedule ID", action)
		}
		var err error
		if id, err = strconv.ParseInt(strings.TrimPrefix(p.pos[1], "#"), 10, 64); err != nil || id <= 0 {
			return 0, usagef("schedule: invalid schedule ID %q", p.pos[1])
		}
	} else if len(p.pos) > 1 {
		return 0, usagef("schedule %s takes no ID", action)
	}
	switch action {
	case "add":
		return scheduleAdd(c, p, time.Now())
	case "list":
		return scheduleList(c, p)
	case "show":
		return scheduleShow(c, id, p.bool("--json"))
	case "rm":
		b, err := slotCall(c, "DELETE", "/api/schedules/"+strconv.FormatInt(id, 10), nil)
		if err != nil {
			return 0, err
		}
		if p.bool("--json") {
			fmt.Println(strings.TrimSpace(string(b)))
		} else {
			fmt.Printf("removed #%d\n", id)
		}
		return ExitOK, nil
	case "pause":
		return schedulePause(c, id, p.values["--reason"], p.bool("--json"))
	case "resume":
		return scheduleResume(c, id, p.bool("--fire"), p.bool("--drop"), p.bool("--json"))
	}
	return 0, usagef("schedule: unknown action %q (add, list, show, rm, pause, resume)", action)
}

func scheduleAdd(c Config, p parsedArgs, now time.Time) (int, error) {
	given := 0
	for _, f := range []string{"--cron", "--at", "--in"} {
		if p.values[f] != "" {
			given++
		}
	}
	if given != 1 {
		return 0, usagef("schedule add needs one of --cron, --at or --in")
	}
	if strings.TrimSpace(p.values["--prompt"]) == "" {
		return 0, usagef("schedule add needs --prompt")
	}
	machine, _ := os.Hostname()
	q := wire.ScheduleCreate{Prompt: p.values["--prompt"], Label: p.values["--label"], Machine: machine,
		TZ: cron.LocalZone(), IdempotencyKey: p.values["--idem"], NoExtend: p.bool("--no-extend")}
	if v := p.values["--expires"]; v != "" {
		d, err := parseDays(v)
		if err != nil || d <= 0 {
			return 0, usagef("schedule add: --expires takes a duration such as 12h or 7d")
		}
		q.ExpiresIn = int64(d / time.Second)
	}
	switch {
	case p.values["--cron"] != "":
		q.Kind, q.Cron = wire.ScheduleCron, p.values["--cron"]
		if _, err := cron.Parse(q.Cron); err != nil {
			return 0, usagef("schedule add: %v", err)
		}
	case p.values["--in"] != "":
		d, err := parseDays(p.values["--in"])
		if err != nil || d <= 0 {
			return 0, usagef("schedule add: --in takes a duration such as 20m, 2h or 1d")
		}
		at := now.Add(d).UTC()
		q.Kind, q.At = wire.ScheduleOnce, &at
	default:
		at, err := parseAt(p.values["--at"], now)
		if err != nil {
			return 0, usagef("schedule add: %v", err)
		}
		q.Kind, q.At = wire.ScheduleOnce, &at
	}
	b, err := slotCall(c, "POST", "/api/schedules", q)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	var sc wire.Schedule
	if err = json.Unmarshal(b, &sc); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Println(sc.ID)
	return ExitOK, nil
}

// parseAt reads --at: RFC 3339, or a local date and time, or a local time
// of day (today, or tomorrow when it has passed).
func parseAt(v string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, v, now.Location()); err == nil {
			return t.UTC(), nil
		}
	}
	if t, err := time.Parse("15:04", v); err == nil {
		at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("--at %q: want RFC 3339, YYYY-MM-DD HH:MM or HH:MM", v)
}

func scheduleList(c Config, p parsedArgs) (int, error) {
	v := url.Values{}
	if s := p.values["--state"]; s != "" {
		v.Set("state", s)
	}
	if r := p.values["--owner"]; r != "" {
		v.Set("role", r)
	}
	path := "/api/schedules"
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	b, err := slotCall(c, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	var list []wire.Schedule
	if err = json.Unmarshal(b, &list); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	for _, sc := range list {
		fmt.Printf("#%d  %s  %s  %s  %s  %s\n", sc.ID, scheduleState(sc), scheduleWhen(sc), nextText(sc), sc.Role, sc.Label)
	}
	return ExitOK, nil
}

// scheduleWhen is the cron expression or "once".
func scheduleWhen(sc wire.Schedule) string {
	if sc.Kind == wire.ScheduleCron {
		return "cron " + strconv.Quote(sc.Cron)
	}
	return sc.Kind
}

func nextText(sc wire.Schedule) string {
	s := "-"
	if sc.NextFireAt != nil && sc.State != wire.SchedulePaused {
		s = "next " + sc.NextFireAt.Local().Format("2006-01-02 15:04")
	}
	if sc.Missed > 0 {
		s += fmt.Sprintf(" (missed %d)", sc.Missed)
	}
	return s
}

func scheduleShow(c Config, id int64, jsonOut bool) (int, error) {
	b, err := slotCall(c, "GET", "/api/schedules/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return 0, err
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	var sc wire.Schedule
	if err = json.Unmarshal(b, &sc); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Printf("schedule #%d\nstate    %s\nwhen     %s (%s)\nlabel    %s\nowner    %s@%s\nnext     %s\ncreated  %s\nexpires  %s\nextends  %t\n",
		sc.ID, sc.State, scheduleWhen(sc), sc.TZ, sc.Label, sc.Role, sc.Machine, nextText(sc),
		sc.CreatedAt.Format(time.RFC3339), sc.ExpiresAt.Format(time.RFC3339), sc.Extends)
	if sc.LastFireAt != nil {
		fmt.Printf("last     %s\n", sc.LastFireAt.Format(time.RFC3339))
	}
	if sc.PausedAt != nil && sc.PausedBy != nil {
		fmt.Printf("paused   %s by %s\n", sc.PausedAt.Format(time.RFC3339), sc.PausedBy.Label())
		if sc.PauseReason != "" {
			fmt.Printf("reason   %s\n", sc.PauseReason)
		}
		if sc.Passed {
			fmt.Println("passed   its time went by while paused (resume with --fire or --drop)")
		}
	}
	fmt.Printf("prompt   %s\n", sc.Prompt)
	return ExitOK, nil
}

// schedulePause runs `schedule pause ID [--reason TEXT]` (#404): the owner,
// or a boss role over it.
func schedulePause(c Config, id int64, reason string, jsonOut bool) (int, error) {
	b, err := slotCall(c, "POST", "/api/schedules/"+strconv.FormatInt(id, 10)+"/pause", wire.SchedulePause{Reason: reason})
	if err != nil {
		return 0, err
	}
	return printPaused(b, jsonOut)
}

// scheduleResume runs `schedule resume ID [--fire|--drop]`. A once schedule
// whose time passed while paused needs one of the two; without it the
// server refuses and the command exits 2.
func scheduleResume(c Config, id int64, fire, drop, jsonOut bool) (int, error) {
	var q wire.ScheduleResume
	switch {
	case fire && drop:
		return 0, usagef("schedule resume: give --fire or --drop, not both")
	case fire:
		q.Passed = wire.PassedFire
	case drop:
		q.Passed = wire.PassedDrop
	}
	b, err := slotCall(c, "POST", "/api/schedules/"+strconv.FormatInt(id, 10)+"/resume", q)
	if err != nil {
		if q.Passed == "" && strings.Contains(err.Error(), "409") && strings.Contains(err.Error(), "passed") {
			return 0, usagef("schedule resume: #%d is a once schedule whose time passed while it was paused; give --fire (send it now) or --drop (drop it and end)", id)
		}
		return 0, err
	}
	return printPaused(b, jsonOut)
}

func printPaused(b []byte, jsonOut bool) (int, error) {
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	var sc wire.Schedule
	if err := json.Unmarshal(b, &sc); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Printf("#%d  %s  %s  %s\n", sc.ID, scheduleState(sc), nextText(sc), sc.Label)
	return ExitOK, nil
}

// scheduleState is the state, with who paused a paused schedule and
// whether its once time passed.
func scheduleState(sc wire.Schedule) string {
	if sc.State != wire.SchedulePaused || sc.PausedBy == nil {
		return sc.State
	}
	s := "paused by " + sc.PausedBy.Label()
	if sc.Passed {
		s += " (time passed)"
	}
	return s
}
