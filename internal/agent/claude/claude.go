// Package claude is the adapter for Claude Code (#434). It holds every
// argument, environment name and screen message that belongs to that CLI;
// nothing outside internal/agent/claude names them.
package claude

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
)

// Name is the adapter's registry name and the CLI's file name.
const Name = "claude"

// ResumeFailRun: a --resume run that exits non-zero within it found no
// conversation to resume.
const ResumeFailRun = 15 * time.Second

func init() {
	agent.MustRegister(Adapter{})
	agent.SetDefault(Name)
}

// Adapter is Claude Code.
type Adapter struct{}

func (Adapter) Name() string       { return Name }
func (Adapter) Executable() string { return Name }

// LaunchArgs: --session-id on a conversation's first launch, --resume after
// it. Never -p, never --continue.
func (Adapter) LaunchArgs(conversation string, resume bool) []string {
	flag := "--session-id"
	if resume {
		flag = "--resume"
	}
	return []string{flag, conversation}
}

// Claude Code 2.1.328 (claude --help, 2026-11-19). The first run with
// --dangerously-skip-permissions shows a one-time acceptance screen
// ("Bypass Permissions mode", default "No, exit"); the settings key
// skipDangerousModePermissionPrompt passed with --settings skips it without
// touching the user's settings files. With several --settings the last
// wins, so ours goes before the record's args: a --settings of the record's
// own replaces it.
var (
	skipArgs  = []string{"--dangerously-skip-permissions", "--settings", `{"skipDangerousModePermissionPrompt":true}`}
	modeFlags = []string{"--dangerously-skip-permissions", "--permission-mode", "--allow-dangerously-skip-permissions"}
)

func (Adapter) PermissionSkipArgs(args []string) []string {
	for _, a := range args {
		name, _, _ := strings.Cut(a, "=")
		if slices.Contains(modeFlags, name) {
			return nil
		}
	}
	return slices.Clone(skipArgs)
}

// refusedArgs are flags the node sets itself (session id, resume) or that
// make the agent non-interactive.
var refusedArgs = map[string]string{
	"--session-id": "the node sets the session id",
	"--resume":     "the node resumes the conversation itself",
	"-r":           "the node resumes the conversation itself",
	"--continue":   "the node resumes the conversation itself",
	"-c":           "the node resumes the conversation itself",
	"--print":      "a hosted session is interactive",
	"-p":           "a hosted session is interactive",
}

func (Adapter) RefusedArgs() map[string]string { return maps.Clone(refusedArgs) }

// roleArgs are the flags a role's hosted session may carry (#377), each
// with the shape its value must have. The others widen folders, permissions
// or settings beyond the grant.
var roleArgs = map[string]*regexp.Regexp{
	"--model":  regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]{0,63}$`),
	"--agent":  regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`),
	"--name":   regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._:-]{0,63}$`),
	"--effort": regexp.MustCompile(`^[a-z]{1,16}$`),
}

func (Adapter) RoleArgs() map[string]*regexp.Regexp { return maps.Clone(roleArgs) }

// sessionMarkers are the variables a running Claude Code sets for its own
// session and children. A node started from inside such a session would hand
// them to hosted sessions, which then think they are sub-sessions (no
// transcript saved, so --resume has nothing to resume). Settings the user
// chooses on purpose (plugin dirs, effort, auth) are not here.
var sessionMarkers = []string{
	"CLAUDECODE",
	"CLAUDE_PID",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_CHILD_NOTES_DIR",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_AGENT",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN",
	"AI_AGENT",
}

func (Adapter) SessionMarkers() []string { return slices.Clone(sessionMarkers) }

// ResumeFailed: a --resume run that exits non-zero within ResumeFailRun.
func (Adapter) ResumeFailed(code int, ran time.Duration) bool {
	return code != 0 && ran < ResumeFailRun
}

// limitPatterns are the usage-limit messages, matched against a session's
// visible screen. This is the one list to change when the CLI words its
// message differently.
var limitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)usage limit reached`),
	regexp.MustCompile(`(?i)\b(?:5-hour|weekly|session|opus|sonnet) limit reached`),
	regexp.MustCompile(`(?i)you(?:'|’)ve (?:hit|reached) your (?:usage |weekly |session )?limit`),
	regexp.MustCompile(`(?i)limit will reset at`),
}

func (Adapter) DetectLimit(rows []string, at time.Time) (agent.LimitHit, bool) {
	return agent.MatchLimit(limitPatterns, rows, at)
}

// Words: the name (which also covers the CLAUDE_* markers) and the
// permission flags and settings key. --session-id and --resume are not here:
// switchboard's own CLI has flags of those names.
func (Adapter) Words() []string {
	return []string{Name, "--dangerously-skip-permissions", "skipDangerousModePermissionPrompt", "--permission-mode", "AI_AGENT"}
}
