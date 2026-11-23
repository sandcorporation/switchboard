package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A login started on another address (an internal one) moves to the public
// URL first, so the flow cookie lands where the provider returns (#519).
func TestOIDCLoginMovesToPublicURL(t *testing.T) {
	t.Parallel()
	p := newFakeIdP(t)
	s, now := oidcServer(t, p, "admin@example.com")

	w := doAuth(s, authReq{method: "GET", path: "/auth/login?next=%2F%23sessions", host: "internal.example:8080"})
	if w.Code != http.StatusFound {
		t.Fatalf("login on another host=%d %s", w.Code, w.Body)
	}
	if c := cookieNamed(w, oidcFlowCookie); c != nil {
		t.Fatalf("flow cookie set on the other host: %+v", c)
	}
	to, err := url.Parse(w.Header().Get("Location"))
	if err != nil || to.Scheme != "https" || to.Host != "mail.example" || to.Path != "/auth/login" {
		t.Fatalf("location=%q", w.Header().Get("Location"))
	}
	if to.Query().Get("next") != "/#sessions" || to.Query().Get("hop") == "" {
		t.Fatalf("query=%q", to.RawQuery)
	}

	// On the public URL the flow starts and the done page returns to next.
	w = doAuth(s, authReq{method: "GET", path: to.RequestURI(), host: "mail.example"})
	flow := cookieNamed(w, oidcFlowCookie)
	if w.Code != http.StatusFound || flow == nil {
		t.Fatalf("login on the public host=%d flow=%v", w.Code, flow)
	}
	code, state := p.issue(t, w.Header().Get("Location"), *now, nil)
	done := callback(s, code, state, flow)
	if done.Code != 200 || !strings.Contains(done.Body.String(), `url=/#sessions`) {
		t.Fatalf("callback=%d %s", done.Code, done.Body)
	}
}

// A proxy that rewrites Host would make every request look internal; the hop
// marker stops a second redirect.
func TestOIDCLoginHopDoesNotLoop(t *testing.T) {
	t.Parallel()
	p := newFakeIdP(t)
	s, _ := oidcServer(t, p, "admin@example.com")
	w := doAuth(s, authReq{method: "GET", path: "/auth/login?hop=1", host: "internal.example"})
	if w.Code != http.StatusFound || cookieNamed(w, oidcFlowCookie) == nil {
		t.Fatalf("hop login=%d", w.Code)
	}
	if loc := w.Header().Get("Location"); strings.HasPrefix(loc, "https://mail.example/") {
		t.Fatalf("redirected again: %q", loc)
	}
}

// Only a path on this origin is kept as the return path.
func TestOIDCLoginNextStaysLocal(t *testing.T) {
	t.Parallel()
	p := newFakeIdP(t)
	s, now := oidcServer(t, p, "admin@example.com")
	for _, next := range []string{"//evil.example/", "https://evil.example/", "evil", "/\\evil.example"} {
		w := doAuth(s, authReq{method: "GET", path: "/auth/login?" + url.Values{"next": {next}}.Encode(), host: "mail.example"})
		code, state := p.issue(t, w.Header().Get("Location"), *now, nil)
		done := callback(s, code, state, cookieNamed(w, oidcFlowCookie))
		if done.Code != 200 || !strings.Contains(done.Body.String(), `content="0;url=/"`) {
			t.Errorf("next=%q: callback=%d %s", next, done.Code, done.Body)
		}
	}
}
