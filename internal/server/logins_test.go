package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// The dashboard's 설정 lists every login and what it may do (#515, #476):
// built-in users and logins named only by their permission row, read only,
// without a password hash or TOTP secret. Reading it needs view.
func TestAdminLoginsListsPermsWithoutSecrets(t *testing.T) {
	t.Parallel()
	s, secret, now := builtinServer(t)
	if _, err := s.db.Exec(`INSERT INTO admin_perms(name,perms,updated_at) VALUES('carol@mail.example','view,type','t')`); err != nil {
		t.Fatal(err)
	}
	w := doAuth(s, authReq{method: "POST", path: "/auth/login", body: loginBody("alice", "correct horse battery", codeAt(t, secret, *now, 0))})
	if w.Code != 200 {
		t.Fatalf("login=%d %s", w.Code, w.Body)
	}
	c := sessionFrom(t, w)
	w = doAuth(s, authReq{method: "GET", path: "/api/admin/logins", cookie: c})
	if w.Code != 200 {
		t.Fatalf("logins=%d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	var got []struct {
		Name      string   `json:"name"`
		Kind      string   `json:"kind"`
		Disabled  bool     `json:"disabled"`
		LastLogin string   `json:"last_login"`
		Perms     []string `json:"perms"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alice" || got[0].Kind != "builtin" || len(got[0].Perms) != 5 || got[0].LastLogin == "" ||
		got[1].Name != "carol@mail.example" || got[1].Kind != "external" || strings.Join(got[1].Perms, ",") != "view,type" {
		t.Fatalf("logins %s", body)
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM admin_users WHERE name='alice'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{hash, secret, "password", "totp", "secret"} {
		if strings.Contains(body, leak) {
			t.Fatalf("logins carries %q: %s", leak, body)
		}
	}
	// Read only: no write on the path.
	if w := doAuth(s, authReq{method: "PUT", path: "/api/admin/logins", cookie: c, headers: map[string]string{"X-Switchboard-Dashboard": "1"}, body: `{}`}); w.Code != 405 {
		t.Fatalf("PUT logins=%d", w.Code)
	}
	allowLogin(t, s, "alice", "")
	if w := doAuth(s, authReq{method: "GET", path: "/api/admin/logins", cookie: c}); w.Code != 403 || !strings.Contains(w.Body.String(), "(view)") {
		t.Fatalf("logins without view=%d %s", w.Code, w.Body)
	}
}
