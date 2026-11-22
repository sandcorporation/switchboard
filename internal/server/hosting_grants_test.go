package server

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// roleCall sends body to a role route with role's token.
func hostRoleCall(t *testing.T, base, method, path, token, role, body string) httpReply {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Switchboard-Role", role)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return httpReply{res.StatusCode, string(b)}
}

func TestRoleHostsSessionsWithinItsGrant(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	nodeToken(t, s, "pc")
	nodeToken(t, s, "pc2")
	asA := func(method, path, body string) httpReply {
		return hostRoleCall(t, base, method, "/api/host/"+path, "secret-a", "a", body)
	}
	rec := func(dir, extra string) string {
		return `{"node":"pc","base":"b","profile":"team-b","directory":"` + dir + `"` + extra + `}`
	}

	if got := asA("POST", "terminals", rec("/work/app", "")); got.code != 403 || !strings.Contains(got.body, "no host grant for a") {
		t.Fatalf("add without a grant = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "POST", "/api/admin/host-grants", cookie, false, `{"role":"a","bases":["b"],"nodes":["pc"],"dir_roots":["/work"]}`); got.code != 403 {
		t.Fatalf("grant without the write guard = %d", got.code)
	}
	for body, want := range map[string]string{
		`{"role":"nobody","bases":["b"],"nodes":["pc"],"dir_roots":["/work"]}`: "role nobody does not exist",
		`{"role":"a","bases":["b"],"nodes":["pc"],"dir_roots":["work"]}`:       "not an absolute path",
		`{"role":"a","bases":[],"nodes":["pc"],"dir_roots":["/work"]}`:         "at least one base",
	} {
		if got := adminCall(t, base, "POST", "/api/admin/host-grants", cookie, true, body); got.code != 400 || !strings.Contains(got.body, want) {
			t.Errorf("grant %s = %d %s, want 400 %q", body, got.code, got.body, want)
		}
	}
	if got := adminCall(t, base, "POST", "/api/admin/host-grants", cookie, true, `{"role":"a","bases":["b"],"nodes":["pc"],"dir_roots":["/work"],"groups":["slow"]}`); got.code != 400 || !strings.Contains(got.body, `no work group "slow"`) {
		t.Errorf("grant of a missing group = %d %s", got.code, got.body)
	}
	for _, g := range []string{`{"name":"slow","threshold":20}`, `{"name":"fast"}`} {
		if got := adminCall(t, base, "POST", "/api/admin/terminals/work-groups", cookie, true, g); got.code != 201 {
			t.Fatalf("group %s = %d %s", g, got.code, got.body)
		}
	}
	got := adminCall(t, base, "POST", "/api/admin/host-grants", cookie, true, `{"role":"a","bases":["b"],"nodes":["pc"],"dir_roots":["/work/"],"groups":["slow"]}`)
	var g wire.HostGrant
	if got.code != 200 || json.Unmarshal([]byte(got.body), &g) != nil || g.DirRoots[0] != "/work" {
		t.Fatalf("grant = %d %s", got.code, got.body)
	}
	if got = adminCall(t, base, "GET", "/api/admin/host-grants", cookie, false, ""); got.code != 200 || !strings.Contains(got.body, `"role":"a"`) {
		t.Fatalf("grant list = %d %s", got.code, got.body)
	}

	for _, c := range []struct{ body, want string }{
		{`{"node":"pc","base":"a","profile":"p","directory":"/work/app"}`, "base: a is not in the grant of a"},
		{`{"node":"pc2","base":"b","profile":"p","directory":"/work/app"}`, "node: pc2 is not in the grant of a"},
		{rec("/etc", ""), "directory: /etc is not under the grant roots of a"},
		{rec("/work/../etc", ""), "directory: /etc is not under"},
		{rec("/work/app/../../etc", ""), "directory: /etc is not under"},
		{rec("/workshop", ""), "directory: /workshop is not under"},
		{rec("/work/app", `,"parent":"b"`), "parent: b is not a"},
		{rec("/work/app", `,"group":"fast"`), "group: fast is not in the grant of a (slow)"},
		{rec("/work/app", `,"env":{"XDG_CONFIG_HOME":"/tmp"}`), "env: a role's session takes no env"},
		{rec("/work/app", `,"args":["--add-dir","/"]`), "--add-dir is not allowed for a role"},
		{rec("/work/app", `,"args":["--settings","x"]`), "--settings is not allowed"},
		{rec("/work/app", `,"args":["--mcp-config=x"]`), "--mcp-config=x is not allowed"},
		{rec("/work/app", `,"args":["--permission-mode","x"]`), "--permission-mode is not allowed"},
		{rec("/work/app", `,"args":["--append-system-prompt","x"]`), "--append-system-prompt is not allowed"},
		{rec("/work/app", `,"args":["--model","../x"]`), `--model value "../x" is not allowed`},
		{rec("/work/app", `,"args":["--agent","a b"]`), "--agent value"},
		{rec("/work/app", `,"args":["--model"]`), "--model needs a value"},
	} {
		if got := asA("POST", "terminals", c.body); got.code != 403 || !strings.Contains(got.body, c.want) {
			t.Errorf("add %s = %d %s, want 403 %q", c.body, got.code, got.body, c.want)
		}
	}
	if got := asA("POST", "terminals", rec("work", "")); got.code != 400 {
		t.Errorf("relative directory = %d %s", got.code, got.body)
	}

	got = asA("POST", "terminals", rec("/work/./app/x/..", `,"class":"review","group":"slow","args":["--model","claude-x","--agent=reviewer","--name","Pc 1","--effort","high"]`))
	var h wire.HostedSession
	if got.code != 201 || json.Unmarshal([]byte(got.body), &h) != nil {
		t.Fatalf("add within the grant = %d %s", got.code, got.body)
	}
	if h.Parent != "a" || h.Directory != "/work/app" || h.DirRoot != "/work" || h.Group != "slow" || len(h.Args) != 7 {
		t.Errorf("added %+v", h)
	}
	admins := createRecord(t, base, cookie, `{"node":"pc","base":"b","profile":"p","directory":"/work/other","parent":"a","args":["--add-dir","/"]}`)
	other := createRecord(t, base, cookie, `{"node":"pc","base":"b","profile":"p","directory":"/work/b","parent":"b"}`)

	var list []wire.HostedSession
	if got = asA("GET", "terminals", ""); got.code != 200 || json.Unmarshal([]byte(got.body), &list) != nil || len(list) != 2 || !strings.Contains(got.body, h.ID) || !strings.Contains(got.body, admins.ID) {
		t.Fatalf("list = %d %s", got.code, got.body)
	}
	if got = hostRoleCall(t, base, "GET", "/api/host/terminals", "secret-b", "b", ""); !strings.Contains(got.body, other.ID) || strings.Contains(got.body, h.ID) {
		t.Fatalf("b's list = %d %s", got.code, got.body)
	}

	for _, c := range []struct{ method, path string }{
		{"POST", "terminals/" + other.ID + "/stop"},
		{"POST", "terminals/" + other.ID + "/start"},
		{"DELETE", "terminals/" + other.ID},
	} {
		if got := asA(c.method, c.path, ""); got.code != 403 || !strings.Contains(got.body, "is not under a") {
			t.Errorf("%s %s of another's = %d %s", c.method, c.path, got.code, got.body)
		}
	}
	if got := hostRoleCall(t, base, "DELETE", "/api/host/terminals/"+h.ID, "secret-b", "b", ""); got.code != 403 {
		t.Errorf("b removes a's = %d %s", got.code, got.body)
	}
	for _, p := range []string{"auto", "autos", "auto/1", "terminals/" + h.ID + "/autos"} {
		if got := asA("POST", p, `{}`); got.code != 403 {
			t.Errorf("POST %s = %d %s", p, got.code, got.body)
		}
	}
	if got := asA("POST", "terminals/"+h.ID+"/restart", ""); got.code != 403 || !strings.Contains(got.body, "host restart is for the admin") {
		t.Errorf("restart = %d %s", got.code, got.body)
	}

	if got = asA("POST", "terminals/"+h.ID+"/stop", ""); got.code != 200 || !strings.Contains(got.body, `"desired":"stopped"`) {
		t.Fatalf("stop = %d %s", got.code, got.body)
	}
	if got = asA("POST", "terminals/"+h.ID+"/start", ""); got.code != 200 || !strings.Contains(got.body, `"desired":"running"`) {
		t.Fatalf("start = %d %s", got.code, got.body)
	}
	if got = asA("DELETE", "terminals/"+h.ID, ""); got.code != 200 {
		t.Fatalf("remove = %d %s", got.code, got.body)
	}
	if got = asA("DELETE", "terminals/"+h.ID, ""); got.code != 404 {
		t.Fatalf("remove again = %d %s", got.code, got.body)
	}

	rows, err := s.db.Query(`SELECT kind, actor FROM hosted_events WHERE session=? ORDER BY id`, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	var audit []string
	for rows.Next() {
		var kind, actor string
		if err := rows.Scan(&kind, &actor); err != nil {
			t.Fatal(err)
		}
		audit = append(audit, kind+" by "+actor)
	}
	rows.Close()
	if strings.Join(audit, ", ") != "add by a, remove by a" {
		t.Errorf("history %q", audit)
	}

	if got = adminCall(t, base, "DELETE", "/api/admin/host-grants?role=a", cookie, true, ""); got.code != 200 {
		t.Fatalf("revoke = %d %s", got.code, got.body)
	}
	if got = asA("POST", "terminals", rec("/work/app2", "")); got.code != 403 || !strings.Contains(got.body, "no host grant") {
		t.Errorf("add after revoke = %d %s", got.code, got.body)
	}
	if got = adminCall(t, base, "DELETE", "/api/admin/host-grants?role=a", cookie, true, ""); got.code != 404 {
		t.Errorf("revoke again = %d %s", got.code, got.body)
	}
}

func TestRoleHostRoutesAreUnknownWithHostingOff(t *testing.T) {
	t.Parallel()
	_, base, _ := signedInServer(t, false)
	if got := hostRoleCall(t, base, "GET", "/api/host/terminals", "secret-a", "a", ""); got.code != 404 {
		t.Fatalf("hosting off = %d %s", got.code, got.body)
	}
}

func TestHostGrantCLIAndRoleProfile(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO roles(role,token,created_at) VALUES('team/boss','h','now')`,
		`INSERT INTO roles(role,token,created_at) VALUES('team/worker','h2','now')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	code, out, stderr := hostCLI(t, dbPath, "grant", "team/boss", "--base", "team/worker", "--node", "pc,pc2", "--dir-root", `C:\Work`)
	if code != 0 || !strings.Contains(out, "team/boss") || !strings.Contains(out, "pc,pc2") || !strings.Contains(out, `C:\Work`) {
		t.Fatalf("host grant = %d %q %s", code, out, stderr)
	}
	if code, out, _ = hostCLI(t, dbPath, "grant", "list", "--json"); code != 0 || !strings.Contains(out, `"role":"team/boss"`) {
		t.Fatalf("host grant list = %d %q", code, out)
	}
	if code, _, stderr = hostCLI(t, dbPath, "grant", "team/boss", "--base", "team/worker"); code != 2 || !strings.Contains(stderr, "requires --base, --node and --dir-root") {
		t.Fatalf("host grant without nodes = %d %s", code, stderr)
	}
	if code, out, _ = hostCLI(t, dbPath, "revoke-grant", "team/boss"); code != 0 || out != "revoked the host grant of team/boss" {
		t.Fatalf("host revoke-grant = %d %q", code, out)
	}

	// Without DB_PATH, host runs as the profile's role through the server.
	h := newCLIHarnessWith(t, func(s *Server) { s.hosting = true })
	h.configure(nil, "a")
	if r := h.run([]string{"DB_PATH="}, "host", "list"); r.code != 0 || !strings.Contains(r.stdout, "no hosted sessions") {
		t.Fatalf("role host list = %+v", r)
	}
	if r := h.run([]string{"DB_PATH="}, "host", "add", "--node", "pc", "--base", "b", "--profile", "p", "--dir", "/work/app"); r.code == 0 || !strings.Contains(r.stderr, "403") || !strings.Contains(r.stderr, "no host grant for a") {
		t.Fatalf("role host add without a grant = %+v", r)
	}
	if r := h.run([]string{"DB_PATH="}, "host", "auto", "list"); r.code == 0 || !strings.Contains(r.stderr, "host auto) are for the admin") {
		t.Fatalf("role host auto = %+v", r)
	}
	if r := h.run([]string{"DB_PATH="}, "host", "grant", "list"); r.code == 0 || !strings.Contains(r.stderr, "admin's") {
		t.Fatalf("role host grant = %+v", r)
	}
}
