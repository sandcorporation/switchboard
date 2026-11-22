package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// hostingNameRE matches SWITCHBOARD_HOSTING, any SWITCHBOARD_HOSTING_* name
// and their old-prefix forms.
var hostingNameRE = regexp.MustCompile(`(` + EnvPrefix + `|` + legacyEnvPrefix + `)` + HostingName + `\w*`)

// repoRoot is the directory holding go.mod above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// isDeployEnvFile: .env, .env.example, prod.env, deploy.env.sample, env.example ...
func isDeployEnvFile(name string) bool {
	return strings.HasPrefix(name, ".env") || strings.HasSuffix(name, ".env") ||
		strings.Contains(name, ".env.") || strings.HasPrefix(name, "env.")
}

// operationsFiles lists the files that configure the operations instance:
// the compose file(s), Jenkinsfile, Dockerfile, every workflow and every
// deploy env file or example in the repository.
func operationsFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{filepath.Join(root, "compose.yaml"), filepath.Join(root, "Jenkinsfile"), filepath.Join(root, "Dockerfile")}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("operations file missing, the guard would pass vacuously: %v", err)
		}
	}
	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*"))
	if err != nil || len(workflows) == 0 {
		t.Fatalf("no workflows found under .github/workflows: %v", err)
	}
	files = append(files, workflows...)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if isDeployEnvFile(name) || (strings.HasPrefix(name, "compose") && p != filepath.Join(root, "compose.yaml") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"))) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// addrRE matches an address written out in a value: an IPv4 address or
// range, an IPv6 address or range, localhost, a URL, or host:port.
var addrRE = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b|([0-9A-Fa-f]{1,4}:){2,7}[0-9A-Fa-f]{0,4}|[0-9A-Fa-f]{0,4}::[0-9A-Fa-f]{0,4}|(?i:localhost)|://|[A-Za-z0-9.\]-]+:\d{1,5}\b`)

// composeHostingRE is the one form a compose file may set a hosting
// variable in: its value comes from the deploy .env, empty by default.
var composeHostingRE = regexp.MustCompile(`^\s*(-\s*)?["']?` + hostingNameRE.String() + `["']?\s*[:=]\s*["']?\$\{` + hostingNameRE.String() + `:-\}["']?\s*$`)

// hostingLineProblem is why one line of an operations file breaks the
// hosting rule (#181, #366 §2), or "": hosting settings may be named, but
// never carry an address, and a compose file passes them through only as
// ${NAME:-}. The value is whatever follows the first hosting name.
func hostingLineProblem(line string, compose bool) string {
	loc := hostingNameRE.FindStringIndex(line)
	if loc == nil {
		return ""
	}
	if m := addrRE.FindString(line[loc[1]:]); m != "" {
		return "carries the address " + strconv.Quote(m) + "; addresses belong in the deploy .env"
	}
	if compose && !composeHostingRE.MatchString(line) {
		return "sets a hosting variable other than as ${NAME:-} (empty default, value from the deploy .env)"
	}
	return ""
}

// TestOperationsConfigHostingCarriesNoAddress: the operations files may name
// SWITCHBOARD_HOSTING* (the deploy .env turns hosting on), but never write
// an address into them, and compose passes them through empty by default.
func TestOperationsConfigHostingCarriesNoAddress(t *testing.T) {
	root := repoRoot(t)
	for _, f := range operationsFiles(t, root) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, f)
		compose := strings.HasPrefix(filepath.Base(f), "compose")
		for i, line := range strings.Split(string(b), "\n") {
			if why := hostingLineProblem(line, compose); why != "" {
				t.Errorf("%s:%d %s", rel, i+1, why)
			}
		}
	}
}

func TestHostingLineProblem(t *testing.T) {
	for _, c := range []struct {
		line    string
		compose bool
		bad     bool
	}{
		{"      SWITCHBOARD_HOSTING: ${SWITCHBOARD_HOSTING:-}", true, false},
		{"      - SWITCHBOARD_HOSTING_TERMINAL=${SWITCHBOARD_HOSTING_TERMINAL:-}", true, false},
		{`      SWITCHBOARD_HOSTING_REMOTE_LISTEN: "${SWITCHBOARD_HOSTING_REMOTE_LISTEN:-}"`, true, false},
		{"      SWITCHBOARD_HOSTING: on", true, true},
		{"      SWITCHBOARD_HOSTING: ${SWITCHBOARD_HOSTING:-on}", true, true},
		{"      SWITCHBOARD_HOSTING_REMOTE_LISTEN: ${SWITCHBOARD_HOSTING_REMOTE_LISTEN:-192.0.2.10:8091}", true, true},
		{"ENV SWITCHBOARD_HOSTING=on", false, false},
		{"    SWITCHBOARD_HOSTING_MAX: 4", false, false},
		{"sh 'echo ${env.SWITCHBOARD_HOSTING_AGENT}'", false, false},
		{"      SWITCHBOARD_ADMIN_AUTH: builtin", true, false},
		{"LISTEN_ADDR=192.0.2.10:8080", false, false},
		{"SWITCHBOARD_HOSTING_REMOTE_LISTEN=192.0.2.10:8091", false, true},
		{"SWITCHBOARD_HOSTING_REMOTE_ALLOW=192.0.2.0/24", false, true},
		{"SWITCHBOARD_HOSTING_TERMINAL_ALLOW=2001:db8::/32", false, true},
		{"SWITCHBOARD_HOSTING_TERMINAL_ALLOW: fd00:1:2:3::/64", false, true},
		{"SWITCHBOARD_HOSTING_REMOTE_LISTEN=[::1]:8091", false, true},
		{"SWITCHBOARD_HOSTING_REMOTE_LISTEN=localhost:8091", false, true},
		{"SWITCHBOARD_HOSTING_REMOTE_LISTEN=pc.example:8091", false, true},
		{"SWITCHBOARD_HOSTING_URL=http://pc.example", false, true},
		{"AGENT_MAIL_HOSTING_REMOTE_LISTEN=192.0.2.10:8091", false, true},
	} {
		if got := hostingLineProblem(c.line, c.compose) != ""; got != c.bad {
			t.Errorf("%q (compose %v): flagged %v, want %v", c.line, c.compose, got, c.bad)
		}
	}
}

func TestHostingNameGuardMatches(t *testing.T) {
	for _, s := range []string{"SWITCHBOARD_HOSTING: on", "- SWITCHBOARD_HOSTING_MAX=4", "AGENT_MAIL_HOSTING=on", "env.SWITCHBOARD_HOSTING_AGENT"} {
		if !hostingNameRE.MatchString(s) {
			t.Errorf("guard misses %q", s)
		}
	}
	for _, s := range []string{"SWITCHBOARD_HOST", "SWITCHBOARD_ADMIN_AUTH: builtin"} {
		if hostingNameRE.MatchString(s) {
			t.Errorf("guard flags %q", s)
		}
	}
	for name, want := range map[string]bool{".env": true, ".env.example": true, "prod.env": true, "deploy.env.sample": true, "env.example": true, "envelope.go": false, "compose.yaml": false} {
		if isDeployEnvFile(name) != want {
			t.Errorf("isDeployEnvFile(%q) = %v", name, !want)
		}
	}
}

// upRE matches a command line that runs `switchboard up`.
// composeServices splits a compose file's services: section into each
// service's lines, by service name (two-space keys).
func composeServices(compose string) map[string]string {
	out := map[string]string{}
	in, name := false, ""
	for _, line := range strings.Split(compose, "\n") {
		switch {
		case line == "services:":
			in = true
			continue
		case in && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			in = false
		}
		if !in {
			continue
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(strings.TrimSpace(line), ":") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			name = strings.TrimSuffix(strings.TrimSpace(line), ":")
		}
		if name != "" {
			out[name] += line + "\n"
		}
	}
	return out
}

func TestComposeServicesSplitsBlocks(t *testing.T) {
	got := composeServices("x: &a\n  K: v\nservices:\n  a:\n    command: x\n  # note\n  b:\n    image: y\nvolumes:\n  d:\n")
	if len(got) != 2 || !strings.Contains(got["a"], "command: x") || strings.Contains(got["b"], "command") || strings.Contains(got["b"], "d:") {
		t.Fatalf("got %q", got)
	}
}

var upRE = regexp.MustCompile(`\bswitchboard(\.exe)?["']?\s*,?\s*["']?up\b`)

// TestOperationsConfigNeverRunsUp keeps the operations instance on the
// plain server (Run): only `up` opens the first-run admin setup page (#221),
// so the image starts `switchboard` with no subcommand and no compose file
// overrides that.
func TestOperationsConfigNeverRunsUp(t *testing.T) {
	root := repoRoot(t)
	commandRE := regexp.MustCompile(`(?m)^\s*(command|entrypoint):`)
	for _, f := range operationsFiles(t, root) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, f)
		if m := upRE.FindString(string(b)); m != "" {
			t.Errorf("%s runs %q: the operations instance never runs up", rel, m)
		}
		if !strings.HasPrefix(filepath.Base(f), "compose") {
			continue
		}
		for name, block := range composeServices(string(b)) {
			if !commandRE.MatchString(block) {
				continue
			}
			// The pre-deploy settings check (#428) runs only serve --check.
			if name == "switchboard-check" && strings.Contains(block, "switchboard serve --check") && !strings.Contains(strings.ReplaceAll(block, "switchboard serve --check", ""), "switchboard serve") {
				continue
			}
			t.Errorf("%s: service %s overrides the image command: the operations instance runs the image's entrypoint", rel, name)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CMD") {
			t.Errorf("Dockerfile has %q: the image runs `switchboard` with no subcommand", line)
		}
		if strings.HasPrefix(line, "ENTRYPOINT") && line != `ENTRYPOINT ["switchboard"]` {
			t.Errorf("Dockerfile has %q, want ENTRYPOINT [\"switchboard\"]", line)
		}
	}
	for s, want := range map[string]bool{`switchboard up --data /x`: true, `["switchboard", "up"]`: true, `switchboard.exe up`: true, "docker compose -p switchboard-x up -d": false, "switchboard update": false} {
		if upRE.MatchString(s) != want {
			t.Errorf("upRE(%q) = %v", s, !want)
		}
	}
}

// TestComposePublishesNodeListenerOnLoopbackByDefault: compose.yaml publishes
// the node listener port 8091, and with REMOTE_BIND_ADDRESS unset only on
// loopback (#422). The variable must not start with SWITCHBOARD_HOSTING, so
// the address guard does not see it.
func TestComposePublishesNodeListenerOnLoopbackByDefault(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const want = `- "${REMOTE_BIND_ADDRESS:-127.0.0.1}:8091:8091"`
	if !strings.Contains(string(b), want) {
		t.Errorf("compose.yaml lacks the node listener port %s", want)
	}
}
