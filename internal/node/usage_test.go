package node_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/node/fakeagent"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// A node that knows its binary and a status directory sets the default
// adapter's status command; every other argument stays as before (#434).
func TestStatusCommandJoinsLaunchArgs(t *testing.T) {
	r := rec("a")
	r.Profile = "" // no identity to pin: the status line alone
	r.SkipPermissions = true
	before := node.Config{Agent: "/opt/agent/claude"}.Argv(r, false)
	cfg := node.Config{Agent: "/opt/agent/claude", Switchboard: "/opt/sb/switchboard", StatusDir: "/var/sb/status"}
	got := cfg.Argv(r, false)
	cmd := "/opt/sb/switchboard node statusline --adapter " + agent.Default().Name() + " --out /var/sb/status/a.json"
	want := slices.Clone(before)
	want[5] = `{"skipDangerousModePermissionPrompt":true,"statusLine":{"type":"command","command":"` + cmd + `"}}`
	if !slices.Equal(got, want) {
		t.Fatalf("argv\n%q\nwant\n%q", got, want)
	}
	r.SkipPermissions = false
	plain := node.Config{Agent: "/opt/agent/claude"}.Argv(r, true)
	if got, want := cfg.Argv(r, true), slices.Concat(plain[:3], []string{"--settings", `{"statusLine":{"type":"command","command":"` + cmd + `"}}`}, plain[3:]); !slices.Equal(got, want) {
		t.Fatalf("argv %q", got)
	}
	cfg.Agent = "/x/other" // a CLI the adapter does not know gets nothing added
	if got := cfg.Argv(r, false); slices.Contains(got, "--settings") {
		t.Fatalf("other CLI %q", got)
	}
	cfg = node.Config{Agent: "/opt/agent/claude", Switchboard: "/opt/sb/switchboard", StatusDir: "/var/sb/status", DefaultAdapter: agent.Default().Name(), Adapters: map[string]string{agent.Default().Name(): "/opt/agent/claude", "stub": "/opt/stub/stubcli"}}
	r.Adapter = "stub" // an adapter that reports no usage
	if got := cfg.Argv(r, false); slices.Contains(got, "--settings") {
		t.Fatalf("stub %q", got)
	}
}

const usageInput = `{"workspace":{"project_dir":"/nowhere"},"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":%d},"seven_day":{"used_percentage":41.2,"resets_at":%d}}}`

// statusHome is a home whose agent settings set statusLine to command.
func statusHome(t *testing.T, command string) string {
	home := t.TempDir()
	p := filepath.Join(home, "."+agent.Default().Name(), "settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"statusLine":{"type":"command","command":"`+command+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// The status command leaves the report before it runs the operator's own
// status line, gives it the same input under a time limit, and shows only
// its output.
func TestStatusLineReportsThenChains(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "st", "a.json")
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	input := fmt.Sprintf(usageInput, at.Add(time.Hour).Unix(), at.Add(72*time.Hour).Unix())
	var stdout bytes.Buffer
	var ran string
	sio := node.StatusIO{
		Stdin: strings.NewReader(input), Stdout: &stdout, Getenv: func(string) string { return "" },
		Home: statusHome(t, "my-line"), Now: func() time.Time { return at },
		Run: func(ctx context.Context, command string, stdin []byte, w io.Writer) error {
			ran = command
			if _, err := os.Stat(out); err != nil {
				t.Errorf("report not written before the chained command: %v", err)
			}
			if d, ok := ctx.Deadline(); !ok || time.Until(d) > node.StatusTimeout {
				t.Errorf("no time limit: %v %v", d, ok)
			}
			if string(stdin) != input {
				t.Errorf("stdin %q", stdin)
			}
			fmt.Fprint(w, "mine\n")
			return context.DeadlineExceeded
		},
	}
	node.StatusLine(agent.Default().Name(), out, sio)
	if ran != "my-line" || stdout.String() != "mine\n" {
		t.Fatalf("ran %q, printed %q", ran, stdout.String())
	}

	// Our side failing (unwritable report) still shows the operator's line.
	stdout.Reset()
	sio.Stdin = strings.NewReader(input)
	sio.Run = func(_ context.Context, _ string, _ []byte, w io.Writer) error { fmt.Fprint(w, "mine\n"); return nil }
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	node.StatusLine(agent.Default().Name(), filepath.Join(blocker, "a.json"), sio)
	if stdout.String() != "mine\n" {
		t.Fatalf("printed %q", stdout.String())
	}

	// No chained command, or one that is this command again: nothing shown.
	for _, home := range []string{t.TempDir(), statusHome(t, "/opt/sb/switchboard node statusline --adapter x --out y")} {
		stdout.Reset()
		sio.Stdin, sio.Home = strings.NewReader(input), home
		sio.Run = func(context.Context, string, []byte, io.Writer) error { t.Error("ran"); return nil }
		node.StatusLine(agent.Default().Name(), out, sio)
		if stdout.Len() != 0 {
			t.Fatalf("printed %q", stdout.String())
		}
	}
	// An adapter that reports nothing writes nothing.
	other := filepath.Join(dir, "other.json")
	node.StatusLine("stub", other, node.StatusIO{Stdin: strings.NewReader(input), Stdout: &stdout})
	if _, err := os.Stat(other); err == nil {
		t.Fatal("stub wrote a report")
	}
}

// The shell runs the chained command with the input on stdin.
func TestStatusLineRunsTheShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	var stdout bytes.Buffer
	node.StatusLine(agent.Default().Name(), filepath.Join(t.TempDir(), "a.json"), node.StatusIO{
		Stdin: strings.NewReader(`{"x":1}`), Stdout: &stdout, Getenv: func(string) string { return "" },
		Home: statusHome(t, "printf 'line:'; cat"),
	})
	if stdout.String() != `line:{"x":1}` {
		t.Fatalf("printed %q", stdout.String())
	}
}

func usageOf(t *testing.T, u []wire.AgentUsage) string {
	t.Helper()
	var parts []string
	for _, x := range u {
		s := x.Adapter + "/" + x.Account + "="
		if x.Remaining == nil {
			s += "?"
		} else {
			s += fmt.Sprint(*x.Remaining)
		}
		if x.ResetsAt != nil {
			s += "@" + time.UnixMilli(*x.ResetsAt).UTC().Format("15:04")
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// The node reports per account the latest known remaining usage; a window
// that filled again is unknown, and a limit message on screen is 0.
func TestNodeUsagePerAccount(t *testing.T) {
	r := newRig(t)
	dir := t.TempDir()
	r.cfg.Switchboard, r.cfg.StatusDir = "/opt/sb/switchboard", dir
	n := r.node(rec("a"), rec("b"))
	def := agent.Default().Name()
	if got := usageOf(t, n.Usage()); got != def+"/=?" {
		t.Fatalf("no reports: %s", got)
	}
	report := func(id string, env map[string]string, used float64, reset time.Time) {
		input := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":%v,"resets_at":%d}}}`, used, reset.Unix())
		node.StatusLine(def, filepath.Join(dir, id+".json"), node.StatusIO{
			Stdin: strings.NewReader(input), Stdout: io.Discard, Getenv: func(k string) string { return env[k] },
			Home: t.TempDir(), Now: func() time.Time { return r.now },
		})
	}
	reset := r.now.Add(time.Hour).Truncate(time.Minute)
	report("a", nil, 30, reset)
	report("b", map[string]string{"CLAUDE_CONFIG_DIR": "/cfg/acct-b"}, 90, reset)
	want := fmt.Sprintf("%[1]s/=70@%[2]s %[1]s/acct-b=10@%[2]s", def, reset.UTC().Format("15:04"))
	if got := usageOf(t, n.Usage()); got != want {
		t.Fatalf("usage %s, want %s", got, want)
	}
	r.now = reset.Add(time.Minute)
	if got := usageOf(t, n.Usage()); got != def+"/=? "+def+"/acct-b=?" {
		t.Fatalf("after the reset: %s", got)
	}
	r.reconcile(n)
	if err := r.fake.Output("sb-a", []byte(fakeagent.LimitMessage("3pm")+"\r\n")); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(node.LimitScan)
	r.reconcile(n)
	if got := usageOf(t, n.Usage()); !strings.HasPrefix(got, def+"/=0@") {
		t.Fatalf("limited: %s", got)
	}
	r.cfg.StatusDir = ""
	if got := r.node(rec("a")).Usage(); got != nil {
		t.Fatalf("without a status directory: %v", got)
	}
}
