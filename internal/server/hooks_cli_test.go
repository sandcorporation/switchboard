package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The webhook writes through the real CLI (#549): a global boss role (a)
// adds, changes, makes again and removes webhooks; the address goes to a
// new owner-only file only and the original destination is read from stdin
// or a file, neither ever in stdout, stderr or --json; another role (b)
// gets 403 with the reason, and an existing address file stops an add
// before the server makes anything. Every write is in the admin action
// record with the role and the action.
func TestHookCLIWrites(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) {
		s.bossRoles = map[string]bool{"a": true}
		s.admin.ProxyToken = "proxy-secret"
	})
	h.configure([]string{"--profile", "boss"}, "a")
	h.configure([]string{"--profile", "worker"}, "b")
	boss := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=boss"}
	worker := []string{"DB_PATH=", "SWITCHBOARD_PROFILE=worker"}
	const dest = "https://hooks.example/services/T0/destination-value"
	var printed []string
	run := func(stdin string, env []string, args ...string) cliResult {
		t.Helper()
		r := h.runIn(stdin, env, args...)
		printed = append(printed, r.stdout, r.stderr)
		return r
	}
	must := func(stdin string, args ...string) string {
		t.Helper()
		r := run(stdin, boss, args...)
		if r.code != 0 {
			t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, r.code, r.stdout, r.stderr)
		}
		return r.stdout
	}
	hookCount := func() int {
		var n int
		_ = h.s.db.QueryRow(`SELECT COUNT(*) FROM hooks`).Scan(&n)
		return n
	}
	address := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" {
			if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
				t.Fatalf("%s mode %v", path, st.Mode().Perm())
			}
		}
		u := strings.TrimSpace(string(b))
		token, ok := strings.CutPrefix(u, h.url+"/hook/")
		if !ok || token == "" {
			t.Fatalf("address %q", u)
		}
		return token
	}

	// add: mail only by default; a notice to the address is b's mail.
	first := filepath.Join(h.dir, "deploys.addr")
	out := must("", "hooks", "add", "--name", "deploys", "--to", "b", "--address-file", first, "--window", "5m")
	if out != "added the webhook deploys (to b, mail); address written to "+first {
		t.Fatalf("add: %q", out)
	}
	token := address(first)
	if w := postHook(h.s, token, `{"content":"deploy done"}`); w.Code != 200 {
		t.Fatalf("notice=%d %s", w.Code, w.Body)
	}
	if got := inboxAlerts(t, h.s, "secret-b"); len(got) != 1 || !strings.Contains(got[0].Body, "deploy done") {
		t.Fatalf("b's alerts %+v", got)
	}

	// An existing file: refused before the server makes anything.
	if r := run("", boss, "hooks", "add", "--name", "other", "--to", "b", "--address-file", first); r.code == 0 || !strings.Contains(r.stderr, "exists") || hookCount() != 1 {
		t.Fatalf("add over a file: %+v hooks=%d", r, hookCount())
	}

	// forward needs a destination; from stdin it is taken, trimmed, and the
	// --json answer is the settings without the address.
	fwdFile := filepath.Join(h.dir, "fwd.addr")
	if r := run("", boss, "hooks", "add", "--name", "fwd", "--to", "b", "--kind", "forward", "--address-file", fwdFile); r.code == 0 || exists(fwdFile) {
		t.Fatalf("forward without a destination: %+v", r)
	}
	out = must("  "+dest+"\n", "hooks", "add", "--name", "fwd", "--to", "b", "--kind", "forward", "--urgent", "--destination-stdin", "--address-file", fwdFile, "--json")
	var hk wire.Hook
	if err := json.Unmarshal([]byte(out), &hk); err != nil || hk.Name != "fwd" || hk.Mode != wire.HookModeForward || !hk.Urgent || hk.Dest != wire.HookDestSet || strings.Contains(out, "token") {
		t.Fatalf("add --json: %s", out)
	}
	fwdToken := address(fwdFile)
	if v, err := h.s.hookSecrets().Open("hook:" + itoa(hk.ID) + ":dest"); err != nil || v != dest {
		t.Fatalf("stored destination %q %v", v, err)
	}

	// edit keeps what is not given.
	destFile := filepath.Join(h.dir, "dest")
	if err := os.WriteFile(destFile, []byte(dest+"/2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	must("", "hooks", "edit", "fwd", "--rename", "forwarded", "--no-urgent", "--window", "1h", "--destination-file", destFile)
	if r := run("", boss, "hooks", "edit", "forwarded", "--clear-destination"); r.code == 0 || !strings.Contains(r.stderr, "forward needs the original destination") {
		t.Fatalf("clearing a forward's destination: %+v", r)
	}
	must("", "hooks", "edit", "forwarded", "--kind", "mail", "--clear-destination", "--to", "a")
	var list []wire.Hook
	if err := json.Unmarshal(adminRequest(h.s, "GET", "/api/admin/hooks", "", nil).Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	if f := list[1]; f.Name != "forwarded" || f.Role != "a" || f.Mode != wire.HookModeMail || f.Urgent || f.WindowMin != 60 || f.Dest != wire.HookDestNone {
		t.Fatalf("edited %+v", f)
	}

	// rotate: the old address is gone, the new one works.
	if r := run("", boss, "hooks", "rotate", "deploys", "--address-file", filepath.Join(h.dir, "never")); r.code != 2 || exists(filepath.Join(h.dir, "never")) {
		t.Fatalf("rotate without --yes: %+v", r)
	}
	second := filepath.Join(h.dir, "deploys2.addr")
	if out = must("", "hooks", "rotate", "deploys", "--yes", "--address-file", second); out != "made a new address for the webhook deploys; address written to "+second {
		t.Fatalf("rotate: %q", out)
	}
	newToken := address(second)
	if w := postHook(h.s, token, `{"content":"x"}`); w.Code != 404 {
		t.Fatalf("old address=%d", w.Code)
	}
	if w := postHook(h.s, newToken, `{"content":"again"}`); w.Code != 200 {
		t.Fatalf("new address=%d", w.Code)
	}

	// Another role: 403 with the reason, and no address file left.
	refusedFile := filepath.Join(h.dir, "refused.addr")
	for _, args := range [][]string{
		{"hooks", "add", "--name", "mine", "--to", "b", "--address-file", refusedFile},
		{"hooks", "edit", "deploys", "--to", "b"},
		{"hooks", "rm", "deploys", "--yes"},
		{"hooks", "rotate", "deploys", "--yes", "--address-file", refusedFile},
	} {
		if r := run("", worker, args...); r.code == 0 || !strings.Contains(r.stderr, "403") || !strings.Contains(r.stderr, "webhook writes are for the global boss role only") || exists(refusedFile) {
			t.Fatalf("%v as b: %+v", args, r)
		}
	}
	if hookCount() != 2 {
		t.Fatalf("hooks after b's tries: %d", hookCount())
	}

	// rm, and the list every role reads.
	if out = must("", "hooks", "rm", "forwarded", "--yes"); out != "removed the webhook forwarded" {
		t.Fatalf("rm: %q", out)
	}
	if w := postHook(h.s, fwdToken, `{"content":"x"}`); w.Code != 404 {
		t.Fatalf("removed hook's address=%d", w.Code)
	}
	if out = h.mustRun(worker, "hooks", "list"); !strings.Contains(out, "deploys") || strings.Contains(out, "forwarded") {
		t.Fatalf("list: %s", out)
	}

	// The dashboard's writes are as before, and recorded with the login.
	addHook(t, h.s, wire.HookSave{Name: "board", Role: "b", Mode: wire.HookModeMail})

	for _, p := range printed {
		for _, secret := range []string{token, newToken, fwdToken, "destination-value", "/hook/"} {
			if strings.Contains(p, secret) {
				t.Fatalf("printed %q", p)
			}
		}
	}
	rows, err := h.s.db.Query(`SELECT admin,method,action,target,detail FROM admin_actions WHERE action LIKE 'hook.%' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var admin, method, action, target, detail string
		if err = rows.Scan(&admin, &method, &action, &target, &detail); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{token, newToken, fwdToken, "destination-value"} {
			if strings.Contains(target+detail, secret) {
				t.Fatalf("recorded %s %s", target, detail)
			}
		}
		if admin == "a" && method != "boss" || admin != "a" && method == "boss" {
			t.Fatalf("actor %s %s", admin, method)
		}
		who := "a"
		if admin != "a" {
			who = "login"
		}
		got = append(got, who+" "+action+" "+target)
	}
	want := []string{"a hook.add hook:deploys", "a hook.add hook:fwd", "a hook.change hook:forwarded", "a hook.change hook:forwarded",
		"a hook.address hook:deploys", "a hook.remove hook:forwarded", "login hook.add hook:board"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("admin actions:\n%s", strings.Join(got, "\n"))
	}
}
