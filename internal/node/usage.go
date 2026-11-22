package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Remaining usage (#434): a session of an adapter that reports usage
// (agent.Reporter) runs `switchboard node statusline` as its agent's status
// command. That command leaves the account's usage in StatusDir, one file per
// record, then runs the operator's own status command with the same input
// and shows its output. The node reads the files into its state frames.

// StatusTimeout bounds the operator's own status command.
const StatusTimeout = 5 * time.Second

// statusInputMax bounds the status input read.
const statusInputMax = 1 << 20

// statusMarker is in every status command the node sets; a chained command
// carrying it would be this one again.
const statusMarker = " node statusline "

// statusFile is the record's usage report.
func (c Config) statusFile(id string) string {
	return filepath.Join(c.StatusDir, id+".json")
}

// statusCommand is the status command a record's session runs.
func (c Config) statusCommand(adapter, id string) string {
	return shellWord(c.Switchboard) + statusMarker + "--adapter " + shellWord(adapter) + " --out " + shellWord(c.statusFile(id))
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

// shellWord is s as one word for the shell the agent runs its status
// command in (sh, or Git Bash on Windows): forward slashes on Windows, and
// double quotes when needed.
func shellWord(s string) string {
	if runtime.GOOS == "windows" {
		s = filepath.ToSlash(s)
	}
	if plainWord.MatchString(s) {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s) + `"`
}

// usageReport is a record's report file.
type usageReport struct {
	Adapter   string `json:"adapter"`
	Account   string `json:"account,omitempty"`
	Known     bool   `json:"known"`
	Remaining int    `json:"remaining"`
	Reset     int64  `json:"reset,omitempty"` // Unix ms
	At        int64  `json:"at"`              // Unix ms
}

// StatusIO is what StatusLine runs with; nil fields are the process's own.
type StatusIO struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	Home     string
	Now      func() time.Time
	// Run runs command in the shell with stdin, writing its output to
	// stdout; nil is sh -c (bash, else PowerShell, on Windows).
	Run func(ctx context.Context, command string, stdin []byte, stdout io.Writer) error
}

// StatusLine is `switchboard node statusline --adapter NAME --out FILE`:
// it leaves the usage the input shows in FILE first, then runs the
// operator's own status command (StatusTimeout) with the same input,
// its output passed through. Its own failures are not shown: the status
// line shows only the operator's command's output.
func StatusLine(adapter, out string, sio StatusIO) {
	sio = sio.withDefaults()
	in, _ := io.ReadAll(io.LimitReader(sio.Stdin, statusInputMax))
	a, err := agent.Lookup(adapter)
	if err != nil {
		return
	}
	rep, ok := a.(agent.Reporter)
	if !ok {
		return
	}
	if out != "" {
		u := rep.ReadUsage(in, sio.Getenv)
		r := usageReport{Adapter: a.Name(), Account: u.Account, Known: u.Known, Remaining: u.Remaining, At: sio.Now().UnixMilli()}
		if !u.Reset.IsZero() {
			r.Reset = u.Reset.UnixMilli()
		}
		_ = writeReport(out, r)
	}
	cmd := strings.TrimSpace(rep.ChainedCommand(in, sio.Getenv, sio.ReadFile, sio.Home))
	if cmd == "" || strings.Contains(cmd+" ", statusMarker) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()
	_ = sio.Run(ctx, cmd, in, sio.Stdout)
}

func (s StatusIO) withDefaults() StatusIO {
	if s.Stdin == nil {
		s.Stdin = os.Stdin
	}
	if s.Stdout == nil {
		s.Stdout = os.Stdout
	}
	if s.Getenv == nil {
		s.Getenv = os.Getenv
	}
	if s.ReadFile == nil {
		s.ReadFile = os.ReadFile
	}
	if s.Home == "" {
		s.Home, _ = os.UserHomeDir()
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Run == nil {
		s.Run = runShell
	}
	return s
}

// runShell runs command as the agent's status line would.
func runShell(ctx context.Context, command string, stdin []byte, stdout io.Writer) error {
	name, args := "sh", []string{"-c", command}
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("bash"); err == nil {
			name = "bash"
		} else {
			name, args = "powershell", []string{"-NoProfile", "-Command", command}
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout = bytes.NewReader(stdin), stdout
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

// writeReport replaces path with r in one rename.
func writeReport(path string, r usageReport) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".usage-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(f.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(f.Name())
	}
	return werr
}

func readReport(path string) (usageReport, bool) {
	var r usageReport
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// Usage is the remaining usage of the node's agent accounts (#434), from its
// records' reports: per adapter and account, the latest report that knows
// it. A report whose window has filled since is not known. An account
// without one is 0 while one of its sessions shows a usage-limit message
// (with that message's reset), else not known (nil Remaining). Nil when the
// node sets no status command (StatusDir empty).
func (n *Node) Usage() []wire.AgentUsage {
	u, _ := n.UsageAccounts()
	return u
}

// UsageAccounts is Usage with the account each record's last report named
// (#435), by record id; a record without a report is missing (its
// adapter's default account).
func (n *Node) UsageAccounts() ([]wire.AgentUsage, map[string]string) {
	if n.cfg.StatusDir == "" || n.cfg.Switchboard == "" {
		return nil, nil
	}
	now := n.cfg.Now()
	type session struct {
		id, adapter string
		limited     bool
		reset       time.Time
	}
	n.mu.Lock()
	var ss []session
	for _, r := range n.records {
		a := n.cfg.adapterFor(r)
		if _, ok := a.(agent.Reporter); !ok {
			continue
		}
		s := session{id: r.ID, adapter: a.Name()}
		if l := n.limits[r.ID]; l != nil && !l.spent {
			s.limited, s.reset = true, l.reset
		}
		ss = append(ss, s)
	}
	n.mu.Unlock()

	type key struct{ adapter, account string }
	type acc struct {
		known      *usageReport
		at         int64
		limited    bool
		limitReset time.Time
	}
	accs := map[key]*acc{}
	accounts := map[string]string{}
	for _, s := range ss {
		k := key{adapter: s.adapter}
		r, ok := readReport(n.cfg.statusFile(s.id))
		if ok && r.Adapter == s.adapter {
			k.account = r.Account
			if r.Account != "" {
				accounts[s.id] = r.Account
			}
		} else {
			ok = false
		}
		x := accs[k]
		if x == nil {
			x = &acc{}
			accs[k] = x
		}
		if s.limited {
			x.limited = true
			if s.reset.After(x.limitReset) {
				x.limitReset = s.reset
			}
		}
		if !ok {
			continue
		}
		x.at = max(x.at, r.At)
		if r.Known && (r.Reset == 0 || r.Reset > now.UnixMilli()) && (x.known == nil || r.At > x.known.At) {
			x.known = &r
		}
	}
	out := make([]wire.AgentUsage, 0, len(accs))
	for k, x := range accs {
		u := wire.AgentUsage{Adapter: k.adapter, Account: k.account, At: x.at}
		switch {
		case x.known != nil:
			rem := x.known.Remaining
			u.Remaining, u.At = &rem, x.known.At
			if x.known.Reset > 0 {
				reset := x.known.Reset
				u.ResetsAt = &reset
			}
		case x.limited:
			zero := 0
			u.Remaining = &zero
			if !x.limitReset.IsZero() {
				reset := x.limitReset.UnixMilli()
				u.ResetsAt = &reset
			}
		}
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b wire.AgentUsage) int {
		return strings.Compare(a.Adapter+"\x00"+a.Account, b.Adapter+"\x00"+b.Account)
	})
	return out, accounts
}
