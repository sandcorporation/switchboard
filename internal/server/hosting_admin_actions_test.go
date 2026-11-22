package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/hosting"
	"github.com/sandcorporation/switchboard/internal/term/termtest"
	"github.com/sandcorporation/switchboard/internal/wire"
)

func adminActions(t *testing.T, base string, cookie *http.Cookie, query string) []wire.AdminAction {
	t.Helper()
	got := adminCall(t, base, "GET", "/api/admin/admin-actions"+query, cookie, false, "")
	var out []wire.AdminAction
	if err := json.Unmarshal([]byte(got.body), &out); err != nil || got.code != http.StatusOK {
		t.Fatalf("admin-actions = %d %s", got.code, got.body)
	}
	return out
}

// The dashboard's invite (#446) is the CLI's code with the CLI's lifetime,
// shown once: the database keeps its hash, the action record only the name.
func TestAdminNodeInviteAPI(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	if got := adminCall(t, base, "POST", "/api/admin/node-invites", cookie, false, `{"name":"pc3"}`); got.code != http.StatusForbidden {
		t.Fatalf("invite without the write guard = %d", got.code)
	}
	for body, want := range map[string]int{
		`{"name":"-bad"}`:           http.StatusBadRequest,
		`{"name":"pc3","extra":1}`:  http.StatusBadRequest,
		`not json`:                  http.StatusBadRequest,
		`{"name":"pc3","code":"x"}`: http.StatusBadRequest,
		`{"name":"` + "pc" + `"}`:   http.StatusCreated,
	} {
		if got := adminCall(t, base, "POST", "/api/admin/node-invites", cookie, true, body); got.code != want {
			t.Errorf("invite %s = %d %s, want %d", body, got.code, got.body, want)
		}
	}
	if got := adminCall(t, base, "GET", "/api/admin/node-invites", cookie, false, ""); got.code != http.StatusMethodNotAllowed {
		t.Fatalf("GET invites = %d", got.code)
	}

	req, _ := http.NewRequest("POST", base+"/api/admin/node-invites", strings.NewReader(`{"name":"pc3"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Switchboard-Dashboard", "1")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var inv wire.NodeInvited
	err = json.NewDecoder(res.Body).Decode(&inv)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusCreated || inv.Name != "pc3" || !strings.HasPrefix(inv.Code, "sbj_") {
		t.Fatalf("invite = %d %+v %v", res.StatusCode, inv, err)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", res.Header.Get("Cache-Control"))
	}
	var created, expires int64
	if err := s.db.QueryRow(`SELECT created_at,expires_at FROM node_invites WHERE name='pc3'`).Scan(&created, &expires); err != nil || expires != inv.ExpiresAt || time.Duration(expires-created)*time.Millisecond != hosting.NodeInviteTTL {
		t.Errorf("invite row %d..%d (%v), answer %d, want %v", created, expires, err, inv.ExpiresAt, hosting.NodeInviteTTL)
	}

	// Nothing outside the response holds the code: not the invite table
	// (its hash only), not the action record.
	var stored int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM node_invites WHERE code_hash LIKE ?`, "%"+inv.Code+"%").Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("node_invites holds the code: %d %v", stored, err)
	}
	acts := adminActions(t, base, cookie, "?target=pc3")
	if len(acts) != 1 || acts[0].Action != wire.ActionNodeInvite || acts[0].Admin != "alice" || acts[0].Method == "" || acts[0].Addr == "" {
		t.Fatalf("actions = %+v", acts)
	}
	raw, _ := json.Marshal(acts)
	if strings.Contains(string(raw), inv.Code) || strings.Contains(string(raw), inv.Code[4:]) {
		t.Fatal("the action record carries the code")
	}

	// The code joins like the CLI's; the name is then taken.
	name, err := joinHost(context.Background(), base, filepath.Join(t.TempDir(), "node.token"), inv.Code)
	if err != nil || name != "pc3" {
		t.Fatalf("join = %q %v", name, err)
	}
	if got := adminCall(t, base, "POST", "/api/admin/node-invites", cookie, true, `{"name":"pc3"}`); got.code != http.StatusConflict {
		t.Fatalf("invite a joined name = %d %s", got.code, got.body)
	}
}

// Every hosting write the spec names (#446) leaves one admin_actions row
// with a name for target; reads and refused writes leave none.
func TestAdminActionsRecordHostingWrites(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	nodeToken(t, s, "pc2")
	if got := adminCall(t, base, "GET", "/api/admin/nodes", cookie, false, ""); !strings.Contains(got.body, `"connected":false`) {
		t.Fatalf("nodes before attach = %s", got.body)
	}
	stop := attachNode(t, base, token, termtest.New())
	defer stop()
	eventually(t, "pc connected", func() bool {
		got := adminCall(t, base, "GET", "/api/admin/nodes", cookie, false, "")
		var ns []wire.NodeView
		_ = json.Unmarshal([]byte(got.body), &ns)
		return len(ns) == 2 && ns[0].Name == "pc" && ns[0].Connected && !ns[1].Connected
	})

	steps := []struct {
		method, path, body string
	}{
		{"POST", "/api/admin/host-grants", `{"role":"a","bases":["b"],"nodes":["pc"],"dir_roots":["/work"]}`},
		{"POST", "/api/admin/terminals", newRecord},
	}
	for _, st := range steps {
		if got := adminCall(t, base, st.method, st.path, cookie, true, st.body); got.code/100 != 2 {
			t.Fatalf("%s %s = %d %s", st.method, st.path, got.code, got.body)
		}
	}
	var id string
	for k := range terminals(t, base, cookie) {
		id = k
	}
	for _, st := range []struct{ method, path, body string }{
		{"PUT", "/api/admin/terminals/" + id, `{"label":"43"}`},
		{"DELETE", "/api/admin/terminals/" + id, ""},
		{"DELETE", "/api/admin/host-grants?role=a", ""},
		{"DELETE", "/api/admin/nodes/pc2", ""},
	} {
		if got := adminCall(t, base, st.method, st.path, cookie, true, st.body); got.code/100 != 2 {
			t.Fatalf("%s %s = %d %s", st.method, st.path, got.code, got.body)
		}
	}
	// Refused writes record nothing.
	adminCall(t, base, "DELETE", "/api/admin/nodes/nobody", cookie, true, "")
	adminCall(t, base, "DELETE", "/api/admin/host-grants?role=a", cookie, true, "")
	adminCall(t, base, "POST", "/api/admin/terminals", cookie, false, newRecord)

	acts := adminActions(t, base, cookie, "")
	var got []string
	for _, a := range acts {
		got = append(got, a.Action+" "+a.Target)
		if a.Admin != "alice" || a.Addr == "" {
			t.Errorf("row %+v", a)
		}
	}
	want := []string{
		wire.ActionNodeRevoke + " pc2",
		wire.ActionGrantRevoke + " a",
		wire.ActionSessionRemove + " " + id,
		wire.ActionSessionChange + " " + id,
		wire.ActionSessionAdd + " " + id,
		wire.ActionGrantSet + " a",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("actions\n got %v\nwant %v", got, want)
	}
	if one := adminActions(t, base, cookie, "?limit=1"); len(one) != 1 || one[0].Target != "pc2" {
		t.Fatalf("limit=1 = %+v", one)
	}
	if got := adminCall(t, base, "POST", "/api/admin/admin-actions", cookie, true, "{}"); got.code != http.StatusMethodNotAllowed {
		t.Fatalf("POST admin-actions = %d", got.code)
	}
}
