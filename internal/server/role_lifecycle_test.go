package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/identity"
)

const rotatedToken = "rotated-worker-credential-0123456789"

func roleViewOf(t *testing.T, s *Server, role string) identity.RoleView {
	t.Helper()
	w := adminRequest(s, "GET", "/api/admin/roles", "", nil)
	var list []identity.RoleView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatalf("roles: %d %s", w.Code, w.Body)
	}
	for _, v := range list {
		if v.Role == role {
			return v
		}
	}
	t.Fatalf("roles list has no %s: %s", role, w.Body)
	return identity.RoleView{}
}

// An operator replaces a role's token: the old one stops at once under every
// name of the role, the new one works, and nothing that answers carries it.
func TestRotateReplacesRoleToken(t *testing.T) {
	s, _ := slotServer(t)
	s.admin.ProxyToken = "proxy-secret"
	t.Setenv("SWITCHBOARD_ENROLLMENT_TOKEN", "enrollment-secret-0123456789abcdef")
	body := func(cred string) string {
		b, _ := json.Marshal(map[string]string{"credential": cred})
		return string(b)
	}

	if w := adminRequest(s, "POST", "/api/admin/roles/team/worker/rotate", body(rotatedToken), nil); w.Code != 403 {
		t.Fatalf("rotate without write guard: %d", w.Code)
	}
	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"GET", "GET", "/api/admin/roles/team/worker/rotate", "", 405},
		{"no body", "POST", "/api/admin/roles/team/worker/rotate", "", 400},
		{"short", "POST", "/api/admin/roles/team/worker/rotate", body("short"), 400},
		{"enrollment", "POST", "/api/admin/roles/team/worker/rotate", body("enrollment-secret-0123456789abcdef"), 400},
		{"unknown", "POST", "/api/admin/roles/team/nobody/rotate", body(rotatedToken), 404},
	} {
		if w := adminWrite(s, c.method, c.path, c.body); w.Code != c.want {
			t.Fatalf("%s: %d %s, want %d", c.name, w.Code, w.Body, c.want)
		}
	}
	if w := callAs(s, "GET", "/api/messages", workerToken, "team/worker-2", nil); w.Code != 200 {
		t.Fatalf("old token before rotate: %d", w.Code)
	}

	w := adminWrite(s, "POST", "/api/admin/roles/team/worker/rotate", body(rotatedToken))
	var v identity.RoleView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Role != "team/worker" || v.RotatedAt == nil || v.RevokedAt != nil {
		t.Fatalf("rotate: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), rotatedToken) {
		t.Fatalf("rotate answer carries the token: %s", w.Body)
	}
	for _, name := range []string{"team/worker", "team/worker-2"} {
		if w := callAs(s, "GET", "/api/messages", workerToken, name, nil); w.Code != 401 {
			t.Fatalf("old token as %s after rotate: %d", name, w.Code)
		}
		if w := callAs(s, "GET", "/api/messages", rotatedToken, name, nil); w.Code != 200 {
			t.Fatalf("new token as %s after rotate: %d %s", name, w.Code, w.Body)
		}
	}
	if w := adminWrite(s, "POST", "/api/admin/roles/team/worker/rotate", body(rotatedToken)); w.Code != 400 {
		t.Fatalf("rotate to the current token: %d", w.Code)
	}
	// A client holding the new token resumes; the old one is refused.
	if w := callAs(s, "POST", "/api/register", "", "", map[string]string{"role": "team/worker", "credential": rotatedToken}); w.Code != 200 || !strings.Contains(w.Body.String(), "resumed") {
		t.Fatalf("resume with the new token: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/register", "", "", map[string]string{"role": "team/worker", "credential": workerToken}); w.Code != 409 {
		t.Fatalf("resume with the old token: %d", w.Code)
	}
	if got := roleViewOf(t, s, "team/worker"); got.RotatedAt == nil || *got.RotatedAt != *v.RotatedAt {
		t.Fatalf("roles list rotated_at: %+v", got)
	}
}

// Retiring revokes a role for good: no restore, no rotate, no registration
// under its name, while it stays in the role list and its mail stays readable.
func TestRetireKeepsMailAndEndsRole(t *testing.T) {
	s, _ := slotServer(t)
	s.admin.ProxyToken = "proxy-secret"
	t.Setenv("SWITCHBOARD_ENROLLMENT_TOKEN", "enrollment-secret-0123456789abcdef")
	slotDo(t, s, "register", ident("m", "/one"))
	if w := call(s, "POST", "/api/messages", "secret-a", map[string]string{"recipient": "team/worker", "subject": "kept after retire", "body": "b"}, "retire-1"); w.Code != 201 && w.Code != 200 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}

	if w := adminWrite(s, "POST", "/api/admin/roles/team/nobody/retire", ""); w.Code != 404 {
		t.Fatalf("retire unknown role: %d", w.Code)
	}
	if w := adminWrite(s, "POST", "/api/admin/roles/team/worker/retire", ""); w.Code != 409 || !strings.Contains(w.Body.String(), `"last_used_at"`) {
		t.Fatalf("retire of a role in use without force: %d %s", w.Code, w.Body)
	}
	w := adminWrite(s, "POST", "/api/admin/roles/team/worker/retire?force=1", "")
	var v identity.RoleView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.RetiredAt == nil || v.RevokedAt == nil {
		t.Fatalf("retire: %d %s", w.Code, w.Body)
	}
	if live, err := s.presence().ActiveSlots("team/worker"); err != nil || len(live) != 0 {
		t.Fatalf("live slots after retire: %v %v", live, err)
	}
	if w := callAs(s, "GET", "/api/messages", workerToken, "team/worker", nil); w.Code != 401 || !strings.HasPrefix(w.Body.String(), "role revoked at ") {
		t.Fatalf("token after retire: %d %s", w.Code, w.Body)
	}
	first := *v.RetiredAt
	if w = adminWrite(s, "POST", "/api/admin/roles/team/worker/retire", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || *v.RetiredAt != first {
		t.Fatalf("second retire: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "POST", "/api/admin/roles/team/worker/restore", ""); w.Code != 409 {
		t.Fatalf("restore a retired role: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "POST", "/api/admin/roles/team/worker/rotate", `{"credential":"`+rotatedToken+`"}`); w.Code != 409 {
		t.Fatalf("rotate a retired role: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/register", "enrollment-secret-0123456789abcdef", "", map[string]string{"role": "team/worker", "credential": rotatedToken}); w.Code != 409 {
		t.Fatalf("register the retired name anew: %d %s", w.Code, w.Body)
	}
	if got := roleViewOf(t, s, "team/worker"); got.RetiredAt == nil || *got.RetiredAt != first {
		t.Fatalf("roles list retired_at: %+v", got)
	}
	if w := adminRequest(s, "GET", "/api/admin/messages?role=team/worker", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "kept after retire") {
		t.Fatalf("dashboard mail of the retired role: %d %s", w.Code, w.Body)
	}
}

// A boss role rotates and retires with its token inside its view; the boss
// write record and the log never hold the credential.
func TestBossRotateAndRetire(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)
	s, _ := bossMailServer(t)
	cred := map[string]string{"credential": rotatedToken}
	for _, c := range []struct {
		name, token, role, path string
		body                    any
		want                    int
	}{
		{"non-boss rotate", "secret-a", "a", "roles/a/rotate", cred, 403},
		{"group boss rotate outside", partnerBossToken, partnerBossRole, "roles/a/rotate", cred, 403},
		{"group boss retire outside", partnerBossToken, partnerBossRole, "roles/a/retire?force=1", nil, 403},
		{"global boss rotate", bossToken, internalBossRole, "roles/a/rotate", cred, 200},
		{"global boss retire", bossToken, internalBossRole, "roles/a/retire?force=1", nil, 200},
	} {
		if got := callAs(s, "POST", "/api/boss/"+c.path, c.token, c.role, c.body); got.Code != c.want {
			t.Fatalf("%s: %d %s, want %d", c.name, got.Code, got.Body, c.want)
		}
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM boss_writes WHERE path LIKE 'roles/a/%'`).Scan(&n)
	if n == 0 {
		t.Fatal("boss writes not recorded")
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM boss_writes WHERE path LIKE ? OR target LIKE ?`, "%"+rotatedToken+"%", "%"+rotatedToken+"%").Scan(&n)
	if n != 0 || strings.Contains(logged.String(), rotatedToken) {
		t.Fatal("the credential reached the boss write record or the log")
	}
}

// roles rotate and roles retire through the real CLI: rotate writes a new
// random token owner-only to --token-file before sending it and never prints
// it; retire shows as retired in the role list.
func TestRolesRotateAndRetireCommands(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	boss := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}
	worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}
	file := filepath.Join(t.TempDir(), "b.token")

	if r := h.run(boss, "roles", "rotate", "b", "--yes"); r.code != 2 || !strings.Contains(r.stderr, "--token-file") {
		t.Fatalf("rotate without --token-file: %+v", r)
	}
	if r := h.run(boss, "roles", "rotate", "b", "--token-file", file); r.code != 2 || !strings.HasPrefix(r.stdout, "would rotate") {
		t.Fatalf("rotate without --yes: %+v", r)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file written without --yes: %v", err)
	}
	out := h.mustRun(boss, "roles", "rotate", "b", "--token-file", file, "--yes")
	raw, err := os.ReadFile(file)
	token := strings.TrimSpace(string(raw))
	if err != nil || len(token) < 24 || !strings.Contains(out, "rotated b") || strings.Contains(out, token) {
		t.Fatalf("rotate: %q token %d bytes, %v", out, len(token), err)
	}
	if r := h.run(worker, "inbox"); r.code == 0 {
		t.Fatalf("old token still works: %+v", r)
	}
	if w := callAs(h.s, "GET", "/api/messages", token, "b", nil); w.Code != 200 {
		t.Fatalf("new token: %d", w.Code)
	}

	if r := h.run(boss, "roles", "retire", "b"); r.code != 2 || !strings.HasPrefix(r.stdout, "would retire b") {
		t.Fatalf("retire without --yes: %+v", r)
	}
	if out := h.mustRun(boss, "roles", "retire", "b", "--yes"); !strings.Contains(out, "retired b") {
		t.Fatalf("retire: %s", out)
	}
	if out := h.mustRun(boss, "roles"); !regexp.MustCompile(`(?m)^b\s+-\s+retired$`).MatchString(out) {
		t.Fatalf("roles list after retire:\n%s", out)
	}
}
