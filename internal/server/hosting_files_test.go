package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/hosting"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// fileReply is a files response with its headers.
type fileReply struct {
	code int
	body string
	hdr  http.Header
}

// fileGet asks base for the rig's record's files (sub "files" or
// "files/raw") at path p as the rig's admin, with extra headers in pairs.
func (r *termRig) fileGet(t *testing.T, base, sub, p string, extra ...string) fileReply {
	t.Helper()
	req, err := http.NewRequest("GET", base+"/api/admin/terminals/"+r.id+"/"+sub+"?path="+url.QueryEscape(p), nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range r.hdr {
		req.Header.Set(k, v)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	if r.cookie != nil {
		req.AddCookie(&http.Cookie{Name: r.cookie.Name, Value: r.cookie.Value})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return fileReply{res.StatusCode, string(b), res.Header}
}

// useFolder points the rig's record at a session folder in t.TempDir (a.txt,
// sub/b.md, a file outside it and a link to that file) and waits until its
// node has it.
func (r *termRig) useFolder(t *testing.T) (dir string) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "work")
	for name, body := range map[string]string{"a.txt": "hello", "sub/b.md": "# b"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "secret.txt"), filepath.Join(dir, "out.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.db.Exec(`UPDATE hosted_sessions SET directory=? WHERE id=?`, dir, r.id); err != nil {
		t.Fatal(err)
	}
	if err := r.s.hostingHub().Sync(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the node has the folder", func() bool { return r.fileGet(t, r.base, "files", "a.txt").code == http.StatusOK })
	return dir
}

// TestHostingFilesListStatAndRead: the listing, a stat, a read whole and
// by Range, the content types and headers, and the refusals, through the
// server and a real node.
func TestHostingFilesListStatAndRead(t *testing.T) {
	t.Parallel()
	r := newTermRig(t, hosting.Admin{})
	dir := r.useFolder(t)
	big := bytes.Repeat([]byte("0123456789"), wire.FileChunkMax/10+7) // more than a chunk
	files := map[string][]byte{"big.bin": big, "page.html": []byte("<script>x</script>"), "pic.svg": []byte("<svg/>"), "img.png": {0x89, 'P'}, "clip.mp4": {0}, "note.MD": []byte("m"), "blob.xyz": {1}}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := r.fileGet(t, r.base, "files", "")
	var ls wire.FileListing
	if got.code != http.StatusOK || json.Unmarshal([]byte(got.body), &ls) != nil || ls.Stat.Kind != wire.FileKindDir {
		t.Fatalf("listing = %d %s", got.code, got.body)
	}
	names := []string{}
	for _, e := range ls.Entries {
		names = append(names, e.Name+":"+e.Kind)
	}
	if want := "sub:dir a.txt:file big.bin:file blob.xyz:file clip.mp4:file img.png:file note.MD:file out.txt:link page.html:file pic.svg:file"; strings.Join(names, " ") != want {
		t.Fatalf("entries %v, want %s", names, want)
	}
	got = r.fileGet(t, r.base, "files", "sub/b.md")
	if got.code != http.StatusOK || !strings.Contains(got.body, `"kind":"file"`) || !strings.Contains(got.body, `"size":3`) || strings.Contains(got.body, `"entries"`) {
		t.Fatalf("stat = %d %s", got.code, got.body)
	}

	got = r.fileGet(t, r.base, "files/raw", "a.txt")
	if got.code != http.StatusOK || got.body != "hello" {
		t.Fatalf("raw = %d %q", got.code, got.body)
	}
	if csp := got.hdr.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || got.hdr.Get("X-Content-Type-Options") != "nosniff" || got.hdr.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("raw headers %v", got.hdr)
	}
	for _, c := range []struct{ rng, body, cr string }{
		{"bytes=1-3", "ell", "bytes 1-3/5"},
		{"bytes=-2", "lo", "bytes 3-4/5"},
		{"bytes=2-", "llo", "bytes 2-4/5"},
		{"bytes=3-99", "lo", "bytes 3-4/5"},
	} {
		got = r.fileGet(t, r.base, "files/raw", "a.txt", "Range", c.rng)
		if got.code != http.StatusPartialContent || got.body != c.body || got.hdr.Get("Content-Range") != c.cr || !strings.Contains(got.hdr.Get("Content-Security-Policy"), "sandbox") {
			t.Errorf("Range %s = %d %q %q, want 206 %q %q", c.rng, got.code, got.body, got.hdr.Get("Content-Range"), c.body, c.cr)
		}
	}
	if got = r.fileGet(t, r.base, "files/raw", "a.txt", "Range", "bytes=9-"); got.code != http.StatusRequestedRangeNotSatisfiable || got.hdr.Get("Content-Range") != "bytes */5" {
		t.Errorf("Range past the end = %d %v", got.code, got.hdr)
	}
	// More than a chunk: read from the node a chunk at a time.
	if got = r.fileGet(t, r.base, "files/raw", "big.bin"); got.code != http.StatusOK || got.body != string(big) {
		t.Fatalf("big = %d, %d bytes of %d", got.code, len(got.body), len(big))
	}
	from := int64(wire.FileChunkMax - 5)
	if got = r.fileGet(t, r.base, "files/raw", "big.bin", "Range", "bytes=1048571-1048580"); got.code != http.StatusPartialContent || got.body != string(big[from:from+10]) {
		t.Fatalf("a range across chunks = %d %q", got.code, got.body)
	}

	for name, want := range map[string]string{
		"page.html": "text/plain; charset=utf-8", "note.MD": "text/plain; charset=utf-8", "pic.svg": "image/svg+xml",
		"img.png": "image/png", "clip.mp4": "video/mp4", "blob.xyz": "application/octet-stream",
	} {
		if got = r.fileGet(t, r.base, "files/raw", name); got.code != http.StatusOK || got.hdr.Get("Content-Type") != want || got.body != string(files[name]) {
			t.Errorf("%s: %d %q, want %q", name, got.code, got.hdr.Get("Content-Type"), want)
		}
	}

	for _, c := range []struct {
		sub, path string
		code      int
		why       string
	}{
		{"files/raw", "out.txt", http.StatusForbidden, wire.FileRefused}, // a link outside: the node refuses
		{"files", "out.txt", http.StatusForbidden, wire.FileRefused},
		{"files/raw", "../secret.txt", http.StatusBadRequest, "bad-request"},
		{"files", "/etc", http.StatusBadRequest, "bad-request"},
		{"files", "C:/x", http.StatusBadRequest, "bad-request"},
		{"files/raw", "a.txt:x", http.StatusBadRequest, "bad-request"},
		{"files/raw", "nope.txt", http.StatusNotFound, wire.FileNotFound},
		{"files/raw", "sub", http.StatusBadRequest, wire.FileNotFile},
	} {
		got = r.fileGet(t, r.base, c.sub, c.path)
		if got.code != c.code || !strings.Contains(got.body, `"code":"`+c.why+`"`) || strings.Contains(got.body, "outside\n") {
			t.Errorf("%s %s = %d %s, want %d %s", c.sub, c.path, got.code, got.body, c.code, c.why)
		}
	}
	anon := *r
	anon.cookie = nil
	if got = anon.fileGet(t, r.base, "files", ""); got.code != http.StatusUnauthorized {
		t.Errorf("anonymous listing = %d", got.code)
	}
	r2 := *r
	r2.id = "hs_" + strings.Repeat("0", 32)
	if got = r2.fileGet(t, r.base, "files", ""); got.code != http.StatusNotFound {
		t.Errorf("unknown record = %d", got.code)
	}
}

// TestHostingFilesFollowTheTerminalSetting: the file routes answer where
// the terminal does and are 404 where it is (the setting, the peer) and on
// the remote listener; the allow list refuses with 403.
func TestHostingFilesFollowTheTerminalSetting(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		mode             string
		loopback, remote int
	}{
		{"", 200, 404},
		{config.TerminalOn, 200, 200},
		{config.TerminalOff, 404, 404},
	} {
		t.Run("mode="+c.mode, func(t *testing.T) {
			t.Parallel()
			r := newTermRigWith(t, hosting.Admin{}, func(s *Server) {
				var err error
				s.terminal, err = terminalSettings(fakeEnv(map[string]string{"SWITCHBOARD_HOSTING_TERMINAL": c.mode}))
				if err != nil {
					t.Fatal(err)
				}
			})
			if c.loopback == 200 {
				r.useFolder(t)
			}
			for peer, want := range map[string]int{loopbackPeerAddr: c.loopback, remotePeerAddr: c.remote} {
				base := peerServer(t, r.s, peer)
				for _, sub := range []string{"files", "files/raw"} {
					got := r.fileGet(t, base, sub, "a.txt")
					if got.code != want || want == 404 && strings.Contains(got.body, `"code"`) {
						t.Errorf("%s %s = %d %s, want %d", peer, sub, got.code, got.body, want)
					}
				}
				// The node's own listener never serves them (no address
				// limit, so the route is what refuses).
				remote := remoteServerAllow(t, r.s, peer, nil)
				if got := r.fileGet(t, remote, "files/raw", "a.txt"); got.code != http.StatusNotFound || got.body == "hello" {
					t.Errorf("remote listener from %s = %d", peer, got.code)
				}
			}
		})
	}
	t.Run("allow", func(t *testing.T) {
		t.Parallel()
		r := newTermRigWith(t, hosting.Admin{}, func(s *Server) {
			var err error
			s.terminal, err = terminalSettings(fakeEnv(map[string]string{"SWITCHBOARD_HOSTING_TERMINAL": config.TerminalOn, "SWITCHBOARD_HOSTING_TERMINAL_ALLOW": "127.0.0.1/32"}))
			if err != nil {
				t.Fatal(err)
			}
		})
		r.useFolder(t)
		if got := r.fileGet(t, peerServer(t, r.s, remotePeerAddr), "files/raw", "a.txt"); got.code != http.StatusForbidden || got.body == "hello" {
			t.Errorf("outside the allow list = %d %q", got.code, got.body)
		}
	})
}

// TestHostingFilesOldNode: a node that did not advertise files gets no
// file frames; the API says it is not supported (501).
func TestHostingFilesOldNode(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	rec, err := s.hostingService().Create(wire.HostedSessionCreate{Node: "pc", Base: "a", Profile: "team-a", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c := dialFakeNode(t, base, token) // no files header: an older node
	c.assign()
	got := adminCall(t, base, "GET", "/api/admin/terminals/"+rec.ID+"/files?path=", cookie, false, "")
	if got.code != http.StatusNotImplemented || !strings.Contains(got.body, `"code":"unsupported"`) {
		t.Fatalf("files of an older node = %d %s", got.code, got.body)
	}
	if err := c.ws.CloseNow(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the node gone", func() bool {
		got = adminCall(t, base, "GET", "/api/admin/terminals/"+rec.ID+"/files?path=", cookie, false, "")
		return got.code == http.StatusServiceUnavailable
	})
	if !strings.Contains(got.body, `"code":"offline"`) {
		t.Fatalf("files of an offline node = %s", got.body)
	}
}

// TestHostingFilesKeepContentOutOfTheLog reads a file and refuses one
// outside; neither the content nor the outside file's reaches the log.
func TestHostingFilesKeepContentOutOfTheLog(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	log.SetOutput(writerFunc(func(b []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(b) }))
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	r := newTermRig(t, hosting.Admin{})
	dir := r.useFolder(t)
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("content-marker-7"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := r.fileGet(t, r.base, "files/raw", "marker.txt"); got.body != "content-marker-7" {
		t.Fatalf("raw = %d %q", got.code, got.body)
	}
	r.fileGet(t, r.base, "files/raw", "out.txt")
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(buf.String(), "content-marker-7") || strings.Contains(buf.String(), "outside") {
		t.Fatalf("file content in the log: %s", buf.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
