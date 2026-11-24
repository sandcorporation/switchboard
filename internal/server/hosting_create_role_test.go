package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// host add --create-role and --another-boss through a role's token (#562):
// a global boss registers any role, a group boss only under its groups, a
// role with a grant none; a boss base needs another_boss; a node known not
// to take role credentials refuses a new role, one not known records why.
// The boss records and scheduled runs that were there keep working.
func TestHostAddCreateRole(t *testing.T) {
	t.Parallel()
	s, _ := bossMailServer(t)
	s.hosting = true
	if _, err := s.db.Exec(`INSERT INTO nodes(name,token_hash,created_at,credentials) VALUES('n1','h',1,1),('old','h',1,0),('new','h',1,NULL)`); err != nil {
		t.Fatal(err)
	}
	add := func(token, role, path string, c wire.HostedSessionCreate) (int, string) {
		t.Helper()
		if c.Node == "" {
			c.Node = "n1"
		}
		c.Profile, c.Desired = "p", "stopped" // past the node's cap of running sessions
		w := callAs(s, "POST", path, token, role, c)
		return w.Code, w.Body.String()
	}
	boss := func(c wire.HostedSessionCreate) (int, string) {
		return add(bossToken, internalBossRole, "/api/boss/terminals", c)
	}
	group := func(c wire.HostedSessionCreate) (int, string) {
		return add(partnerBossToken, partnerBossRole, "/api/boss/terminals", c)
	}
	exists := func(role string) bool { return identity.RoleExists(s.db, role) }

	// A boss role made before #562, with a scheduled run: still changed
	// and scheduled through the boss route.
	svc := s.hostingService()
	old, err := svc.Create(wire.HostedSessionCreate{Node: "n1", Base: internalBossRole, Profile: "p", Directory: "/w/boss", Desired: "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAuto(old.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "go"}); err != nil {
		t.Fatal(err)
	}

	// Without the flag a missing role is refused and stays missing.
	if code, body := boss(wire.HostedSessionCreate{Base: "team/typo", Directory: "/w/t"}); code != 400 || !strings.Contains(body, "--create-role") || exists("team/typo") {
		t.Fatalf("missing role without --create-role: %d %s", code, body)
	}
	// A global boss registers it and the record in one add; the reply
	// carries no token.
	code, body := boss(wire.HostedSessionCreate{Base: "team/worker2", Directory: "/w/a", CreateRole: true})
	if code != 201 || !exists("team/worker2") || strings.Contains(strings.ToLower(body), "token") || strings.Contains(body, "credential") {
		t.Fatalf("global boss --create-role: %d %s", code, body)
	}
	// Again with the flag: the role is there, one more session is added.
	if code, body := boss(wire.HostedSessionCreate{Base: "team/worker2", Directory: "/w/b", CreateRole: true}); code != 201 {
		t.Fatalf("--create-role of a registered role: %d %s", code, body)
	}

	// A group boss: under its group, not outside it.
	if code, body := group(wire.HostedSessionCreate{Base: "partner-a/new", Directory: "/w/pa", CreateRole: true}); code != 201 || !exists("partner-a/new") {
		t.Fatalf("group boss inside: %d %s", code, body)
	}
	if code, body := group(wire.HostedSessionCreate{Base: "team/outside", Directory: "/w/po", CreateRole: true}); code != 403 || exists("team/outside") {
		t.Fatalf("group boss outside: %d %s", code, body)
	}

	// A role that is no boss: not with a grant either.
	if _, err := svc.SetGrant(wire.HostGrant{Role: partnerDevRole, Bases: []string{partnerDevRole}, Nodes: []string{"n1"}, DirRoots: []string{"/w"}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/host/terminals", "/api/boss/terminals"} {
		for _, base := range []string{"partner-a/made", partnerDevRole} {
			if code, body := add(partnerDevToken, partnerDevRole, path, wire.HostedSessionCreate{Base: base, Directory: "/w/m", CreateRole: true}); code != 403 || exists("partner-a/made") {
				t.Fatalf("non-boss %s %s: %d %s", path, base, code, body)
			}
		}
	}
	if code, body := add(partnerDevToken, partnerDevRole, "/api/host/terminals", wire.HostedSessionCreate{Base: partnerDevRole, Directory: "/w/m", CreateRole: true}); !strings.Contains(body, "a host grant does not allow it") {
		t.Fatalf("grant refusal: %d %s", code, body)
	}

	// A boss base needs another_boss; the refusal names both ways.
	for _, base := range []string{internalBossRole, partnerBossRole} {
		code, body := boss(wire.HostedSessionCreate{Base: base, Directory: "/w/again"})
		if code != 400 || !strings.Contains(body, "--another-boss") || !strings.Contains(body, "--create-role") {
			t.Fatalf("boss base %s: %d %s", base, code, body)
		}
	}
	if code, body := boss(wire.HostedSessionCreate{Base: internalBossRole, Directory: "/w/again", AnotherBoss: true}); code != 201 {
		t.Fatalf("--another-boss: %d %s", code, body)
	}
	// The dashboard's admin form is not guarded.
	if w := adminWrite(s, "POST", "/api/admin/terminals", `{"node":"n1","base":"`+partnerBossRole+`","profile":"p","directory":"/w/form","desired":"stopped"}`); w.Code != 201 {
		t.Fatalf("admin form boss base: %d %s", w.Code, w.Body)
	}

	// The boss record before #562 still changes and schedules.
	if w := callAs(s, "PUT", "/api/boss/terminals/"+old.ID, bossToken, internalBossRole, map[string]string{"label": "kept"}); w.Code != 200 {
		t.Fatalf("set the old boss record: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/boss/terminals/"+old.ID+"/autos", bossToken, internalBossRole, wire.HostAutoCreate{Cron: "0 10 * * *", Prompt: "go"}); w.Code != 201 {
		t.Fatalf("schedule the old boss record: %d %s", w.Code, w.Body)
	}
	if autos, err := svc.Autos(old.ID); err != nil || len(autos) != 2 {
		t.Fatalf("old boss record's runs: %v %v", autos, err)
	}

	// A node known not to take role credentials: refused before anything.
	if code, body := boss(wire.HostedSessionCreate{Node: "old", Base: "team/oldnode", Directory: "/w/o", CreateRole: true}); code != 400 || !strings.Contains(body, "does not take role credentials") || exists("team/oldnode") {
		t.Fatalf("no-credential node: %d %s", code, body)
	}
	// … but a role that is registered already is added there as before.
	if code, body := boss(wire.HostedSessionCreate{Node: "old", Base: "team/worker2", Directory: "/w/o", CreateRole: true}); code != 201 {
		t.Fatalf("registered role on a no-credential node: %d %s", code, body)
	}
	// A node not known yet: added, and the record says why it may not run.
	code, body = boss(wire.HostedSessionCreate{Node: "new", Base: "team/newnode", Directory: "/w/n", CreateRole: true})
	if code != 201 || !exists("team/newnode") {
		t.Fatalf("unknown node: %d %s", code, body)
	}
	var rec wire.HostedSession
	_ = json.Unmarshal([]byte(body), &rec)
	events, err := svc.Events(rec.ID)
	if err != nil || len(events) != 1 || events[0].Kind != wire.HostedEventCredentialUnknown || !strings.Contains(events[0].Reason, "switchboard node join") {
		t.Fatalf("unknown node's record events: %+v %v", events, err)
	}
}

// The admin's DB_PATH follows the same rules (#562): a missing role needs
// --create-role and the refusal shows the command with it; a boss base
// needs --another-boss and the refusal shows both commands.
func TestHostCLICreateRoleAndAnotherBoss(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO roles(role,token,created_at) VALUES('team/boss','h','now');
INSERT INTO groups(prefix,boss_role,created_at) VALUES('team','team/boss','now');
INSERT INTO nodes(name,token_hash,created_at,credentials) VALUES('pc','h',1,1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	add := []string{"add", "--node", "pc", "--base", "team/worker", "--profile", "team-worker", "--dir", "/work/app", "--label", "two words"}
	code, _, stderr := hostCLI(t, dbPath, add...)
	want := "to register it with the session: switchboard host add --node pc --base team/worker --profile team-worker --dir /work/app --label 'two words' --create-role"
	if code != 1 || !strings.Contains(stderr, want) {
		t.Fatalf("missing role = %d %s", code, stderr)
	}
	code, id, stderr := hostCLI(t, dbPath, append(add, "--create-role")...)
	if code != 0 || !strings.HasPrefix(id, "hs_") {
		t.Fatalf("--create-role = %d %q %s", code, id, stderr)
	}

	bossAdd := []string{"add", "--node", "pc", "--base", "team/boss", "--profile", "team-boss", "--dir", "/work/boss"}
	code, _, stderr = hostCLI(t, dbPath, bossAdd...)
	if code != 1 || !strings.Contains(stderr, "another role: switchboard host add --node pc --base <new role> --profile team-boss --dir /work/boss --create-role") ||
		!strings.Contains(stderr, "one more boss session: switchboard host add --node pc --base team/boss --profile team-boss --dir /work/boss --another-boss") {
		t.Fatalf("boss base = %d %s", code, stderr)
	}
	if code, id, stderr = hostCLI(t, dbPath, append(bossAdd, "--another-boss")...); code != 0 || !strings.HasPrefix(id, "hs_") {
		t.Fatalf("--another-boss = %d %q %s", code, id, stderr)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !identity.RoleExists(db, "team/worker") {
		t.Fatal("team/worker not registered")
	}
}

// host --help starts with the one-line example of a new role's session,
// and both hosting documents carry it (#562).
func TestHostHelpShowsNewRoleExample(t *testing.T) {
	t.Parallel()
	const example = "host add --base team/worker --create-role --node NODE --profile NAME --dir PATH"
	code, out, stderr := binaryCLI(t, "", "host", "--help")
	if code != 0 {
		t.Fatalf("host --help = %d %s", code, stderr)
	}
	lines := strings.SplitN(out, "\n", 4)
	if len(lines) < 3 || !strings.Contains(lines[2], example) {
		t.Fatalf("host --help head:\n%s", out)
	}
	for _, doc := range []string{"hosting-server.md", "hosting-pc.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		if err != nil || !strings.Contains(string(b), example) {
			t.Errorf("%s lacks the new role example (%v)", doc, err)
		}
	}
}
