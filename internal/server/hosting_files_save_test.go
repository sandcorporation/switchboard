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
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/hosting"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// fpHeader is the fingerprint header.
var fpHeader = config.HeaderPrefix + config.FileFingerprintHeaderName

// filePut saves text at path p as the rig's admin, through the dashboard's
// write guard, with fingerprint fp ("" sends none).
func (r *termRig) filePut(t *testing.T, p, fp, text string) fileReply {
	t.Helper()
	b, _ := json.Marshal(wire.FileSave{Text: text})
	return r.filePutRaw(t, p, fp, string(b), true)
}

func (r *termRig) filePutRaw(t *testing.T, p, fp, body string, guard bool) fileReply {
	t.Helper()
	req, err := http.NewRequest("PUT", r.base+"/api/admin/terminals/"+r.id+"/files/raw?path="+url.QueryEscape(p), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range r.hdr {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	if guard {
		req.Header.Set("X-Switchboard-Dashboard", "1")
	}
	if fp != "" {
		req.Header.Set(fpHeader, fp)
	}
	if r.cookie != nil {
		req.AddCookie(&http.Cookie{Name: r.cookie.Name, Value: r.cookie.Value})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return fileReply{res.StatusCode, string(out), res.Header}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestHostingFilesSave: a whole read carries the fingerprint; a save with
// it writes, one with an older one is 409 and writes nothing, "*" saves
// anyway; the 2 MiB limit, the guard and the refusals, through the server
// and a real node.
func TestHostingFilesSave(t *testing.T) {
	t.Parallel()
	r := newTermRig(t, hosting.Admin{})
	dir := r.useFolder(t)
	a := filepath.Join(dir, "a.txt")

	got := r.fileGet(t, r.base, "files/raw", "a.txt")
	fp, ok := wire.ParseFileFingerprint(got.hdr.Get(fpHeader))
	if got.code != http.StatusOK || !ok || fp.Size != 5 || fp.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("read = %d %q, fingerprint %q", got.code, got.body, got.hdr.Get(fpHeader))
	}
	if got = r.fileGet(t, r.base, "files/raw", "a.txt", "Range", "bytes=0-1"); got.hdr.Get(fpHeader) != "" {
		t.Errorf("a range has a fingerprint: %q", got.hdr.Get(fpHeader))
	}

	got = r.filePut(t, "a.txt", fp.String(), "changed\n")
	var saved wire.FileSaved
	if got.code != http.StatusOK || json.Unmarshal([]byte(got.body), &saved) != nil || readFile(t, a) != "changed\n" || saved.Stat.Size != 8 || saved.Path != "a.txt" {
		t.Fatalf("save = %d %s; file %q", got.code, got.body, readFile(t, a))
	}
	// The fingerprint a save returns is the one the next read gives.
	if again := r.fileGet(t, r.base, "files/raw", "a.txt"); again.hdr.Get(fpHeader) != saved.Fingerprint {
		t.Errorf("fingerprint after save %q, read %q", saved.Fingerprint, again.hdr.Get(fpHeader))
	}
	// The fingerprint read before the save is stale now: 409, nothing written.
	if got = r.filePut(t, "a.txt", fp.String(), "lost"); got.code != http.StatusConflict || !strings.Contains(got.body, `"code":"conflict"`) || readFile(t, a) != "changed\n" {
		t.Fatalf("stale save = %d %s; file %q", got.code, got.body, readFile(t, a))
	}
	// Changed by someone else: same size and time would not hide it either.
	if err := os.WriteFile(a, []byte("other!!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got = r.filePut(t, "a.txt", saved.Fingerprint, "mine"); got.code != http.StatusConflict || readFile(t, a) != "other!!\n" {
		t.Fatalf("save over another change = %d %s", got.code, got.body)
	}
	// 그래도 저장.
	if got = r.filePut(t, "a.txt", "*", "mine"); got.code != http.StatusOK || readFile(t, a) != "mine" {
		t.Fatalf("save anyway = %d %s", got.code, got.body)
	}
	// More than a chunk goes in pieces.
	big := strings.Repeat("abcdefghi\n", wire.FileChunkMax/10+1000)
	if got = r.filePut(t, "sub/b.md", "*", big); got.code != http.StatusOK || readFile(t, filepath.Join(dir, "sub", "b.md")) != big {
		t.Fatalf("big save = %d %.200s", got.code, got.body)
	}

	for _, c := range []struct {
		name, path, fp, body string
		code                 int
		why                  string
	}{
		{"no fingerprint", "a.txt", "", `{"text":"x"}`, http.StatusBadRequest, "bad-request"},
		{"bad fingerprint", "a.txt", "1:2:zz", `{"text":"x"}`, http.StatusBadRequest, "bad-request"},
		{"bad body", "a.txt", "*", `text`, http.StatusBadRequest, "bad-request"},
		{"too big", "a.txt", "*", `{"text":"` + strings.Repeat("a", wire.FileTextMax+1) + `"}`, http.StatusRequestEntityTooLarge, wire.FileTooBig},
		{"link outside", "out.txt", "*", `{"text":"x"}`, http.StatusForbidden, wire.FileRefused},
		{"outside", "../secret.txt", "*", `{"text":"x"}`, http.StatusBadRequest, "bad-request"},
		{"absolute", "/etc/hosts", "*", `{"text":"x"}`, http.StatusBadRequest, "bad-request"},
		{"new file", "new.txt", "*", `{"text":"x"}`, http.StatusNotFound, wire.FileNotFound},
		{"folder", "sub", "*", `{"text":"x"}`, http.StatusBadRequest, wire.FileNotFile},
		{"the session folder", "", "*", `{"text":"x"}`, http.StatusBadRequest, wire.FileNotFile},
	} {
		got = r.filePutRaw(t, c.path, c.fp, c.body, true)
		if got.code != c.code || !strings.Contains(got.body, `"code":"`+c.why+`"`) {
			t.Errorf("%s: %d %s, want %d %s", c.name, got.code, got.body, c.code, c.why)
		}
	}
	if readFile(t, filepath.Join(filepath.Dir(dir), "secret.txt")) != "outside" || readFile(t, a) != "mine" {
		t.Fatal("a refused save wrote")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err == nil {
		t.Fatal("a save made a new file")
	}
	// Without the dashboard header the write guard refuses.
	if got = r.filePutRaw(t, "a.txt", "*", `{"text":"x"}`, false); got.code != http.StatusForbidden || readFile(t, a) != "mine" {
		t.Fatalf("unguarded save = %d", got.code)
	}
	anon := *r
	anon.cookie = nil
	if got = anon.filePut(t, "a.txt", "*", "x"); got.code != http.StatusUnauthorized || readFile(t, a) != "mine" {
		t.Fatalf("anonymous save = %d", got.code)
	}
	// Each save is one admin action on the session: its path and size, not
	// its content; refused saves record nothing.
	acts, err := r.s.hostingService().AdminActions(r.id, 0)
	if err != nil {
		t.Fatal(err)
	}
	saves := []string{}
	for _, a := range acts {
		if a.Action == wire.ActionFileSave {
			saves = append(saves, a.Detail)
		}
		if strings.Contains(a.Detail, "mine") || strings.Contains(a.Detail, "abcdefghi") {
			t.Errorf("content in the record: %+v", a)
		}
	}
	want := []string{"sub/b.md · " + strconv.Itoa(len(big)) + " B", "a.txt · 4 B", "a.txt · 8 B"}
	if strings.Join(saves, "|") != strings.Join(want, "|") {
		t.Errorf("save records = %q, want %q", saves, want)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".switchboard-") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

// TestHostingFilesSaveFollowsTheTerminalSetting: where the terminal is off
// a save is 404, as the reads are.
func TestHostingFilesSaveFollowsTheTerminalSetting(t *testing.T) {
	t.Parallel()
	r := newTermRigWith(t, hosting.Admin{}, func(s *Server) {
		var err error
		s.terminal, err = terminalSettings(fakeEnv(map[string]string{"SWITCHBOARD_HOSTING_TERMINAL": config.TerminalOff}))
		if err != nil {
			t.Fatal(err)
		}
	})
	if got := r.filePut(t, "a.txt", "*", "x"); got.code != http.StatusNotFound || strings.Contains(got.body, `"code"`) {
		t.Fatalf("save with the terminal off = %d %s", got.code, got.body)
	}
}

// TestHostingFilesSaveOnAReadOnlyNode: a node that reads files but did not
// advertise writes gets no write frame; the save is 501 read-only.
func TestHostingFilesSaveOnAReadOnlyNode(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	token := nodeToken(t, s, "pc")
	rec, err := s.hostingService().Create(wire.HostedSessionCreate{Node: "pc", Base: "a", Profile: "team-a", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c := dialFakeNode(t, base, token, config.HeaderPrefix+config.NodeFilesHeaderName, "1")
	c.assign()
	r := &termRig{s: s, base: base, cookie: cookie, id: rec.ID}
	if got := r.filePut(t, "a.txt", "*", "x"); got.code != http.StatusNotImplemented || !strings.Contains(got.body, `"code":"read-only"`) {
		t.Fatalf("save on a read-only node = %d %s", got.code, got.body)
	}
}

// TestHostingFilesSaveKeepsContentOutOfTheLog saves a file and refuses a
// save; the content reaches neither the log.
func TestHostingFilesSaveKeepsContentOutOfTheLog(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	log.SetOutput(writerFunc(func(b []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(b) }))
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	r := newTermRig(t, hosting.Admin{})
	r.useFolder(t)
	if got := r.filePut(t, "a.txt", "*", "save-marker-9"); got.code != http.StatusOK {
		t.Fatalf("save = %d %s", got.code, got.body)
	}
	r.filePut(t, "out.txt", "*", "refused-marker-9")
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(buf.String(), "marker-9") {
		t.Fatalf("file content in the log: %s", buf.String())
	}
}
