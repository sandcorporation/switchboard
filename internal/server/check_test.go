package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/config"
)

// Values the check must never print.
const (
	checkSecret = "never-print-this-secret"
	checkToken  = "team/boss=never-print-this-token"
)

// refusedSettings are settings the server refuses to start with, each with
// the words its refusal names.
var refusedSettings = []struct {
	name string
	env  map[string]string
	want []string
}{
	{"oidc without issuer", map[string]string{
		"SWITCHBOARD_ADMIN_AUTH":         "oidc",
		"SWITCHBOARD_OIDC_CLIENT_ID":     "switchboard",
		"SWITCHBOARD_OIDC_CLIENT_SECRET": checkSecret,
		"SWITCHBOARD_OIDC_ADMINS":        "boss@mail.example",
		"SWITCHBOARD_PUBLIC_URL":         "https://mail.example",
		"SWITCHBOARD_TOKENS":             checkToken,
	}, []string{"SWITCHBOARD_ADMIN_AUTH=oidc needs SWITCHBOARD_OIDC_ISSUER"}},
	{"unknown admin auth", map[string]string{"SWITCHBOARD_ADMIN_AUTH": "proxy"}, []string{"SWITCHBOARD_ADMIN_AUTH must be builtin or oidc"}},
	{"listen address without port", map[string]string{"LISTEN_ADDR": "8080"}, []string{"LISTEN_ADDR must be host:port"}},
	{"hosting remote listener not ip:port", map[string]string{
		"SWITCHBOARD_HOSTING":               "on",
		"SWITCHBOARD_HOSTING_REMOTE_LISTEN": "nowhere",
	}, []string{"SWITCHBOARD_HOSTING=on refused", "SWITCHBOARD_HOSTING_REMOTE_LISTEN"}},
	{"every refusal at once", map[string]string{
		"SWITCHBOARD_SSE_KEEPALIVE": "soon",
		"SWITCHBOARD_POLL_MAX":      "-1",
		"SWITCHBOARD_ADMIN_AUTH":    "oidc",
	}, []string{"SWITCHBOARD_SSE_KEEPALIVE must be", "SWITCHBOARD_POLL_MAX must be", "SWITCHBOARD_ADMIN_AUTH=oidc needs"}},
}

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestCheckRefusesWhatTheServerRefuses(t *testing.T) {
	for _, c := range refusedSettings {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := Check("test", mapEnv(c.env), &out, nil); code != 1 {
				t.Fatalf("exit %d, want 1\n%s", code, out.String())
			}
			for _, w := range c.want {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output lacks %q:\n%s", w, out.String())
				}
			}
			assertNoValues(t, out.String())
		})
	}
}

func TestCheckPassesGoodSettings(t *testing.T) {
	for _, env := range []map[string]string{
		{},
		{"SWITCHBOARD_TOKENS": checkToken, "SWITCHBOARD_ADMIN_AUTH": "builtin", "LISTEN_ADDR": "127.0.0.1:8080"},
		{"SWITCHBOARD_ADMIN_AUTH": "oidc", "SWITCHBOARD_OIDC_ISSUER": "https://id.mail.example", "SWITCHBOARD_OIDC_CLIENT_ID": "switchboard",
			"SWITCHBOARD_OIDC_CLIENT_SECRET": checkSecret, "SWITCHBOARD_OIDC_ADMINS": "boss@mail.example"},
	} {
		var out bytes.Buffer
		// oidc needs a PUBLIC_URL, which the check then asks for /health.
		if env["SWITCHBOARD_ADMIN_AUTH"] == "oidc" {
			srv := healthServer(t, http.StatusOK, `{"ok":true,"version":"old"}`)
			env["SWITCHBOARD_PUBLIC_URL"] = srv.URL
		}
		if code := Check("test", mapEnv(env), &out, nil); code != 0 {
			t.Fatalf("env %v: exit %d, want 0\n%s", envNames(env), code, out.String())
		}
		if !strings.Contains(out.String(), "settings: ok") {
			t.Errorf("output lacks the verdict:\n%s", out.String())
		}
		assertNoValues(t, out.String())
	}
}

// TestCheckProbesPublicURL: a PUBLIC_URL that answers no switchboard /health
// is a warning, not a refusal; one that does is said.
func TestCheckProbesPublicURL(t *testing.T) {
	good := healthServer(t, http.StatusOK, `{"ok":true,"version":"old"}`)
	missing := healthServer(t, http.StatusNotFound, `not here`)
	other := healthServer(t, http.StatusOK, `<html>`)
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(tlsSrv.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	for _, c := range []struct {
		name, url string
		client    *http.Client
		want      string
	}{
		{"answers", good.URL + "/", nil, "SWITCHBOARD_PUBLIC_URL answers /health"},
		{"trusted certificate", tlsSrv.URL, tlsSrv.Client(), "SWITCHBOARD_PUBLIC_URL answers /health"},
		{"untrusted certificate", tlsSrv.URL, nil, "warning: SWITCHBOARD_PUBLIC_URL: /health did not answer: the certificate check failed"},
		{"not found", missing.URL, nil, "warning: SWITCHBOARD_PUBLIC_URL: /health answered 404"},
		{"not a switchboard", other.URL, nil, "warning: SWITCHBOARD_PUBLIC_URL: /health answered, but not as a switchboard server"},
		{"nothing listens", closedURL, nil, "warning: SWITCHBOARD_PUBLIC_URL: /health did not answer: cannot connect"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			code := Check("test", mapEnv(map[string]string{"SWITCHBOARD_PUBLIC_URL": c.url}), &out, c.client)
			if code != 0 {
				t.Fatalf("exit %d, want 0 (a warning only)\n%s", code, out.String())
			}
			if !strings.Contains(out.String(), c.want) {
				t.Errorf("output lacks %q:\n%s", c.want, out.String())
			}
			host := strings.TrimPrefix(strings.TrimPrefix(c.url, "https://"), "http://")
			if strings.Contains(out.String(), strings.TrimSuffix(host, "/")) {
				t.Errorf("output names the address:\n%s", out.String())
			}
		})
	}
}

// TestServerStartRefusesTheSameSettings runs the server (Run) in a child
// process with each refused setting and checks it exits 1 with the refusal
// the check printed: the start and the check take the same decisions.
func TestServerStartRefusesTheSameSettings(t *testing.T) {
	if os.Getenv("SB_CHECK_RUN") != "" {
		Run("test")
		return
	}
	t.Parallel()
	for _, c := range refusedSettings {
		var checked bytes.Buffer
		Check("test", mapEnv(c.env), &checked, nil)
		cmd := exec.Command(os.Args[0], "-test.run=^TestServerStartRefusesTheSameSettings$")
		dbPath := filepath.Join(t.TempDir(), "mail.db")
		cmd.Env = append(config.WithoutOwnEnv(os.Environ()), "SB_CHECK_RUN=1", "DB_PATH="+dbPath)
		for k, v := range c.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Errorf("%s: exit %v, want code 1\n%s", c.name, err, out)
			continue
		}
		for _, line := range strings.Split(checked.String(), "\n") {
			refusal, ok := strings.CutPrefix(line, "settings: refused: ")
			if ok && !strings.Contains(string(out), refusal) {
				t.Errorf("%s: the server's log lacks the check's refusal %q:\n%s", c.name, refusal, out)
			}
		}
		if _, err := os.Stat(dbPath); err == nil {
			t.Errorf("%s: the server opened its database before refusing its settings", c.name)
		}
	}
}

func healthServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func assertNoValues(t *testing.T, out string) {
	t.Helper()
	for _, v := range []string{checkSecret, "never-print-this-token", "mail.example"} {
		if strings.Contains(out, v) {
			t.Errorf("output prints the value %q:\n%s", v, out)
		}
	}
}

func envNames(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
