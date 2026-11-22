package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The boss read commands (#397) through the real CLI: a boss role (a) reads
// mail, roles, settings and jobs; another role (b) is refused; the settings
// name a secret setting without its value; showing mail leaves its state.
func TestBossReadCommands(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) {
		s.bossRoles = map[string]bool{"a": true}
		s.admin.ProxyToken = "proxy-secret-value"
	})
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	w := callAs(h.s, "POST", "/api/messages", "secret-b", "b", map[string]string{"recipient": "b", "subject": "note to self", "body": "line one"})
	var m wire.Message
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || w.Code != 201 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}

	out := h.mustRun(nil, "mail", "list", "--open", "--since", "1h", "--profile", "boss")
	if !strings.Contains(out, "#"+strconv.FormatInt(m.ID, 10)+" pending · b → b") || !strings.Contains(out, "note to self") {
		t.Fatalf("mail list: %s", out)
	}
	var list []wire.Message
	if err := json.Unmarshal([]byte(h.mustRun(nil, "mail", "list", "--json", "--profile", "boss")), &list); err != nil || len(list) != 1 {
		t.Fatalf("mail list --json: %v %+v", err, list)
	}
	if out := h.mustRun(nil, "mail", "show", strconv.FormatInt(m.ID, 10), "--profile", "boss"); !strings.Contains(out, "  line one") {
		t.Fatalf("mail show: %s", out)
	}
	if out := h.mustRun(nil, "inbox", "--profile", "worker"); !strings.Contains(out, "b → b · pending") {
		t.Fatalf("state after mail show: %s", out)
	}
	if res := h.run(nil, "mail", "list", "--since", "soon", "--profile", "boss"); res.code == 0 {
		t.Fatalf("bad --since accepted: %+v", res)
	}

	if out := h.mustRun(nil, "roles", "--profile", "boss"); !strings.Contains(out, "ROLE") || !strings.Contains(out, "b ") {
		t.Fatalf("roles: %s", out)
	}
	out = h.mustRun(nil, "settings", "--profile", "boss")
	if !strings.Contains(out, "SWITCHBOARD_ADMIN_PROXY_TOKEN: set") || !strings.Contains(out, "SWITCHBOARD_DISCORD_WEBHOOK: not set") || strings.Contains(out, "proxy-secret-value") {
		t.Fatalf("settings: %s", out)
	}
	if out := h.mustRun(nil, "settings", "--json", "--profile", "boss"); strings.Contains(out, "proxy-secret-value") || !strings.Contains(out, `"question_settings"`) {
		t.Fatalf("settings --json: %s", out)
	}
	if out := h.mustRun(nil, "jobs", "--profile", "boss"); !strings.Contains(out, "no jobs") {
		t.Fatalf("jobs: %s", out)
	}

	for _, args := range [][]string{{"mail", "list"}, {"mail", "show", "1"}, {"jobs"}, {"roles"}, {"settings"}} {
		res := h.run(nil, append(args, "--profile", "worker")...)
		if res.code == 0 || !strings.Contains(res.stderr, "403") {
			t.Fatalf("%v as a non-boss: %+v", args, res)
		}
	}
	if res := h.run([]string{"SWITCHBOARD_PROFILE=boss"}, "host", "nodes"); res.code == 0 || !strings.Contains(res.stderr, "hosting is off") {
		t.Fatalf("host nodes, hosting off: %+v", res)
	}
	if out := h.mustRun(nil, "help", "mail"); !strings.Contains(out, "--since DURATION") {
		t.Fatalf("help mail: %s", out)
	}
}

// host list, host nodes and host auto list with a boss role's token and no
// DB_PATH show every record in view (#397); a role that is no boss lists the
// records under its grant, as before.
func TestBossHostCommands(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) {
		s.bossRoles = map[string]bool{"a": true}
		s.hosting = true
	})
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	if _, err := h.s.db.Exec(`INSERT INTO nodes(name,token_hash,created_at) VALUES('pc','h',1)`); err != nil {
		t.Fatal(err)
	}
	rec, err := h.s.hostingService().Create(wire.HostedSessionCreate{Node: "pc", Base: "b", Profile: "p", Directory: "/work/app", Label: "worker-one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.hostingService().CreateAuto(rec.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "go", Label: "morning"}); err != nil {
		t.Fatal(err)
	}
	boss := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}
	if out := h.mustRun(boss, "host", "list"); !strings.Contains(out, rec.ID) || !strings.Contains(out, "worker-one") {
		t.Fatalf("boss host list: %s", out)
	}
	if out := h.mustRun(boss, "host", "nodes"); !strings.Contains(out, "pc") || !strings.Contains(out, "NAME") {
		t.Fatalf("boss host nodes: %s", out)
	}
	if out := h.mustRun(boss, "host", "auto", "list"); !strings.Contains(out, "morning") || !strings.Contains(out, rec.ID) {
		t.Fatalf("boss host auto list: %s", out)
	}
	if out := h.mustRun(boss, "host", "auto", "list", "--json"); !strings.Contains(out, `"cron":"0 9 * * *"`) {
		t.Fatalf("boss host auto list --json: %s", out)
	}
	worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}
	if out := h.mustRun(worker, "host", "list"); !strings.Contains(out, "no hosted sessions") {
		t.Fatalf("worker host list: %s", out)
	}
	if r := h.run(worker, "host", "nodes"); r.code == 0 || !strings.Contains(r.stderr, "403") {
		t.Fatalf("worker host nodes: %+v", r)
	}
}
