package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/hosting"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// allowLogin sets what user may do, as `admin allow` stores it.
func allowLogin(t *testing.T, s *Server, user, perms string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO admin_perms(name,perms,updated_at) VALUES(?,?,'t') ON CONFLICT(name) DO UPDATE SET perms=excluded.perms`, user, perms); err != nil {
		t.Fatal(err)
	}
}

// A login that loses type stops typing on the input socket it already has
// open; the work screen's other writes answer 403 by permission, and the
// login's /api/admin/me says what it may do (#476).
func TestLoginPermsReachOpenSocketsAndRoutes(t *testing.T) {
	t.Parallel()
	r := newTermRig(t, hosting.Admin{})
	in, emu := r.openMode(t, hosting.ModeInput)
	eventually(t, "the node streaming", func() bool { return r.fake.Streams(r.name()) == 1 })
	typeInto(t, in, "a")
	eventually(t, "a typed", func() bool { return string(r.fake.Typed(r.name())) == "a" })

	allowLogin(t, r.s, "alice", "view")
	typeInto(t, in, "b")
	clientSend(t, in, wire.TerminalInput{Type: "resize", Cols: 30, Rows: 9})
	texts := readUntil(t, in, emu, func(texts []wire.TerminalMessage) bool {
		n := 0
		for _, m := range texts {
			if m.Type == "refused" {
				n++
			}
		}
		return n == 2
	})
	if m, _ := hasText(texts, "refused"); !strings.Contains(m.Error, wire.PermType) {
		t.Errorf("refusal %+v", m)
	}

	me := r.call(t, r.base, "GET", "/api/admin/me", "")
	var who struct {
		User  string   `json:"user"`
		Perms []string `json:"perms"`
	}
	if err := json.Unmarshal([]byte(me.body), &who); err != nil || who.User != "alice" || strings.Join(who.Perms, ",") != "view" {
		t.Errorf("me = %d %s", me.code, me.body)
	}
	for _, c := range []struct{ method, path, body, perm string }{
		{"POST", "/api/admin/terminals/" + r.id + "/ticket", `{"mode":"input"}`, wire.PermType},
		{"PUT", "/api/admin/terminals/" + r.id + "/files/raw?path=a.txt", `{"text":"x"}`, wire.PermFiles},
		{"DELETE", "/api/admin/terminals/" + r.id + "/files/raw?path=a.txt", ``, wire.PermFiles},
		{"POST", "/api/admin/terminals/" + r.id + "/restart", `{}`, wire.PermSessions},
		{"POST", "/api/admin/terminals/" + r.id + "/new-conversation", `{}`, wire.PermSessions},
		{"PUT", "/api/admin/terminals/" + r.id, `{}`, wire.PermSessions},
		{"DELETE", "/api/admin/terminals/" + r.id, ``, wire.PermSessions},
		{"POST", "/api/admin/terminals", newRecord, wire.PermSessions},
		{"POST", "/api/admin/left/1/drop", `{"reason":"x"}`, wire.PermDiscard},
	} {
		got := r.call(t, r.base, c.method, c.path, c.body)
		if got.code != http.StatusForbidden || !strings.Contains(got.body, "("+c.perm+")") {
			t.Errorf("%s %s = %d %s, want 403 for %s", c.method, c.path, got.code, got.body, c.perm)
		}
	}
	if got := r.call(t, r.base, "POST", "/api/admin/terminals/"+r.id+"/ticket", `{"mode":"view"}`); got.code != http.StatusOK {
		t.Errorf("view ticket = %d %s", got.code, got.body)
	}
	if got := r.call(t, r.base, "GET", "/api/admin/terminals", ""); got.code != http.StatusOK {
		t.Errorf("terminal list = %d", got.code)
	}

	// Without view, not even a view ticket.
	allowLogin(t, r.s, "alice", "none")
	if got := r.call(t, r.base, "POST", "/api/admin/terminals/"+r.id+"/ticket", `{"mode":"view"}`); got.code != http.StatusForbidden {
		t.Errorf("view ticket without view = %d %s", got.code, got.body)
	}

	// Given back, the same socket types again.
	allowLogin(t, r.s, "alice", "view,type,discard")
	typeInto(t, in, "c")
	eventually(t, "c typed", func() bool { return string(r.fake.Typed(r.name())) == "ac" })
	if got := r.call(t, r.base, "POST", "/api/admin/left/1/drop", `{"reason":"x"}`); strings.Contains(got.body, "("+wire.PermDiscard+")") {
		t.Errorf("drop refused by permission with discard allowed: %s", got.body)
	}
}

func TestAdminAllowAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "mail.db")
	t.Setenv("DB_PATH", path)
	if _, err := adminCLI(t, "correct horse battery\n", "create", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := adminCLI(t, "correct horse battery\n", "create", "bob"); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Name  string   `json:"name"`
		Perms []string `json:"perms"`
	}
	list := func() map[string]string {
		t.Helper()
		out, err := adminCLI(t, "", "list", "", "--json")
		var rows []row
		if err != nil || json.Unmarshal([]byte(out), &rows) != nil {
			t.Fatalf("list: %v %s", err, out)
		}
		m := map[string]string{}
		for _, r := range rows {
			m[r.Name] = strings.Join(r.Perms, ",")
		}
		return m
	}
	// Every login starts with everything.
	if got := list(); got["alice"] != "view,type,files,sessions,discard" || got["bob"] != got["alice"] {
		t.Fatalf("before allow: %v", got)
	}
	out, err := adminCLI(t, "", "allow", "alice", "type,view,type", "--json")
	var res struct {
		Name, Action string
		Perms        []string
	}
	if err != nil || json.Unmarshal([]byte(out), &res) != nil || res.Action != "allow" || strings.Join(res.Perms, ",") != "view,type" {
		t.Fatalf("allow --json: %v %s", err, out)
	}
	if _, err := adminCLI(t, "", "allow", "bob", "none"); err != nil {
		t.Fatal(err)
	}
	if got := list(); got["alice"] != "view,type" || got["bob"] != "" {
		t.Fatalf("after allow: %v", got)
	}
	if out, err := adminCLI(t, "", "list", ""); err != nil || !strings.Contains(out, "allow view,type") || !strings.Contains(out, "allow none") {
		t.Fatalf("list text: %v %q", err, out)
	}
	if _, err := adminCLI(t, "", "allow", "alice", "all"); err != nil {
		t.Fatal(err)
	}
	if got := list(); got["alice"] != "view,type,files,sessions,discard" {
		t.Fatalf("after all: %v", got)
	}
	for _, bad := range [][]string{{"allow", "alice", "view,root"}, {"allow", "alice"}, {"allow", "../x", "view"}, {"enable", "alice", "view"}} {
		if _, err := adminCLI(t, "", bad...); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// A login allowed nothing keeps its sessions (only the permissions
	// change); the stored row is all there is.
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var perms string
	if err := db.QueryRow(`SELECT perms FROM admin_perms WHERE name='bob'`).Scan(&perms); err != nil || perms != "" {
		t.Fatalf("bob's row = %q %v", perms, err)
	}
}
