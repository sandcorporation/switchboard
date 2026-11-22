package claude

import (
	"encoding/json"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
)

// Claude Code runs the settings' status line command with a JSON status on
// stdin; for claude.ai subscriptions it carries rate_limits after the first
// response (https://code.claude.com/docs/en/statusline.md).

const (
	settingsFlag = "--settings"
	// configDirEnv chooses the configuration (and account) a session uses.
	configDirEnv = "CLAUDE_CONFIG_DIR"
)

var _ agent.Reporter = Adapter{}

type statusLine struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// ReportArgs sets the status line command through --settings. A later
// --settings replaces an earlier one, so it joins the one own already has
// (the permission skip's), and adds nothing when the record's args carry
// their own.
func (Adapter) ReportArgs(own, args []string, command string) []string {
	for _, a := range args {
		if name, _, _ := strings.Cut(a, "="); name == settingsFlag {
			return own
		}
	}
	b, err := json.Marshal(struct {
		StatusLine statusLine `json:"statusLine"`
	}{statusLine{Type: "command", Command: command}})
	if err != nil {
		return own
	}
	line := string(b)
	out := slices.Clone(own)
	for i := 0; i+1 < len(out); i++ {
		if out[i] != settingsFlag || !strings.HasPrefix(out[i+1], "{") || !strings.HasSuffix(out[i+1], "}") {
			continue
		}
		body := strings.TrimSpace(strings.TrimSuffix(out[i+1], "}"))
		if body == "{" {
			out[i+1] = line
		} else {
			out[i+1] = body + "," + line[1:]
		}
		return out
	}
	return append(out, settingsFlag, line)
}

type window struct {
	Used   *float64 `json:"used_percentage"`
	Resets *int64   `json:"resets_at"`
}

// ReadUsage reads rate_limits: remaining is 100 less the larger of the
// five-hour and seven-day use, reset that window's resets_at. A window
// past its reset is left out of the input, so an input without either is
// not known.
func (Adapter) ReadUsage(input []byte, getenv func(string) string) agent.Usage {
	u := agent.Usage{Account: account(getenv)}
	var in struct {
		RateLimits *struct {
			FiveHour *window `json:"five_hour"`
			SevenDay *window `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if json.Unmarshal(input, &in) != nil || in.RateLimits == nil {
		return u
	}
	used, reset := -1.0, int64(0)
	for _, w := range []*window{in.RateLimits.FiveHour, in.RateLimits.SevenDay} {
		if w == nil || w.Used == nil || *w.Used <= used {
			continue
		}
		used, reset = *w.Used, 0
		if w.Resets != nil {
			reset = *w.Resets
		}
	}
	if used < 0 {
		return u
	}
	u.Known, u.Remaining = true, min(100, max(0, int(math.Floor(100-used))))
	if reset > 0 {
		u.Reset = time.Unix(reset, 0)
	}
	return u
}

// account is the configuration directory's name; "" the default one.
func account(getenv func(string) string) string {
	d := strings.TrimSpace(getenv(configDirEnv))
	if d == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(d))
}

// ChainedCommand is the status line the session would have shown: the
// first settings file that sets statusLine, in Claude Code's order (the
// project's local, the project's, then the user's settings).
func (Adapter) ChainedCommand(input []byte, getenv func(string) string, readFile func(string) ([]byte, error), home string) string {
	var in struct {
		Cwd       string `json:"cwd"`
		Workspace struct {
			ProjectDir string `json:"project_dir"`
			CurrentDir string `json:"current_dir"`
		} `json:"workspace"`
	}
	_ = json.Unmarshal(input, &in)
	var files []string
	for _, p := range []string{in.Workspace.ProjectDir, in.Workspace.CurrentDir, in.Cwd} {
		if p != "" {
			files = append(files, filepath.Join(p, ".claude", "settings.local.json"), filepath.Join(p, ".claude", "settings.json"))
			break
		}
	}
	dir := strings.TrimSpace(getenv(configDirEnv))
	if dir == "" && home != "" {
		dir = filepath.Join(home, ".claude")
	}
	if dir != "" {
		files = append(files, filepath.Join(dir, "settings.json"))
	}
	for _, f := range files {
		b, err := readFile(f)
		if err != nil {
			continue
		}
		var s struct {
			StatusLine *statusLine `json:"statusLine"`
		}
		if json.Unmarshal(b, &s) != nil || s.StatusLine == nil {
			continue
		}
		if s.StatusLine.Type != "" && s.StatusLine.Type != "command" {
			return ""
		}
		return s.StatusLine.Command
	}
	return ""
}
