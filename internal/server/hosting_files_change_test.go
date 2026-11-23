package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/hosting"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// fileChange is a files pane change as the dashboard sends it: DELETE with
// no body, POST with a wire.FileChange.
func (r *termRig) fileChange(t *testing.T, method, p string, c *wire.FileChange) httpReply {
	t.Helper()
	body := ""
	if c != nil {
		b, _ := json.Marshal(c)
		body = string(b)
	}
	return r.call(t, r.base, method, "/api/admin/terminals/"+r.id+"/files?path="+url.QueryEscape(p), body)
}

// TestHostingFilesChange: delete (#473), rename (#474) and upload (#475)
// reach the node through the host, answer what is there after, keep inside
// the session folder, and need the files permission (#476).
func TestHostingFilesChange(t *testing.T) {
	t.Parallel()
	r := newTermRig(t, hosting.Admin{})
	dir := r.useFolder(t)

	got := r.fileChange(t, "POST", "sub", &wire.FileChange{Op: wire.FileUpload, Name: "up.bin", Data: []byte{0xff, 0, 'x'}})
	var ch wire.FileChanged
	if got.code != http.StatusOK || json.Unmarshal([]byte(got.body), &ch) != nil || ch.Path != "sub/up.bin" || ch.Stat == nil || ch.Stat.Size != 3 {
		t.Fatalf("upload = %d %s", got.code, got.body)
	}
	if readFile(t, filepath.Join(dir, "sub", "up.bin")) != "\xff\x00x" {
		t.Fatal("upload bytes")
	}
	if got = r.fileChange(t, "POST", "", &wire.FileChange{Op: wire.FileUpload, Name: "a.txt", Data: []byte("new")}); got.code != http.StatusConflict || !strings.Contains(got.body, `"exists"`) {
		t.Fatalf("upload onto a.txt = %d %s", got.code, got.body)
	}
	if got = r.fileChange(t, "POST", "", &wire.FileChange{Op: wire.FileUpload, Name: "a.txt", Data: []byte("new"), Replace: true}); got.code != http.StatusOK || readFile(t, filepath.Join(dir, "a.txt")) != "new" {
		t.Fatalf("replace a.txt = %d %s", got.code, got.body)
	}
	if got = r.fileChange(t, "POST", "", &wire.FileChange{Op: wire.FileUpload, Name: "big", Data: make([]byte, wire.FileUploadMax+1)}); got.code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too big = %d %s", got.code, got.body)
	}

	if got = r.fileChange(t, "POST", "sub/up.bin", &wire.FileChange{Op: wire.FileRename, To: "down.bin"}); got.code != http.StatusOK || json.Unmarshal([]byte(got.body), &ch) != nil || ch.Path != "sub/down.bin" {
		t.Fatalf("rename = %d %s", got.code, got.body)
	}
	if got = r.fileChange(t, "POST", "a.txt", &wire.FileChange{Op: wire.FileRename, To: "../a.txt"}); got.code != http.StatusBadRequest {
		t.Fatalf("rename out = %d %s", got.code, got.body)
	}
	if got = r.fileChange(t, "POST", "a.txt", &wire.FileChange{Op: wire.FileRename, To: "sub"}); got.code != http.StatusConflict {
		t.Fatalf("rename onto sub = %d %s", got.code, got.body)
	}

	if got = r.fileChange(t, "DELETE", "sub", nil); got.code != http.StatusConflict || !strings.Contains(got.body, `"not-empty"`) {
		t.Fatalf("delete full folder = %d %s", got.code, got.body)
	}
	if got = r.fileChange(t, "DELETE", "out.txt", nil); got.code != http.StatusOK || readFile(t, filepath.Join(filepath.Dir(dir), "secret.txt")) != "outside" {
		t.Fatalf("delete link = %d %s", got.code, got.body)
	}
	for _, p := range []string{"", "../secret.txt"} {
		if got = r.fileChange(t, "DELETE", p, nil); got.code != http.StatusForbidden && got.code != http.StatusBadRequest {
			t.Fatalf("delete %q = %d %s", p, got.code, got.body)
		}
	}
	if got = r.fileChange(t, "DELETE", "sub/down.bin", nil); got.code != http.StatusOK {
		t.Fatalf("delete = %d %s", got.code, got.body)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "down.bin")); err == nil {
		t.Fatal("down.bin still there")
	}

	// Recorded by path, never content.
	acts, err := r.s.hostingService().AdminActions(r.id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, a := range acts {
		switch a.Action {
		case wire.ActionFileUpload, wire.ActionFileRename, wire.ActionFileDelete:
			seen = append(seen, a.Action+" "+a.Detail)
		}
	}
	for _, want := range []string{"file.upload sub/up.bin · 3 B", "file.rename sub/up.bin → sub/down.bin", "file.delete out.txt", "file.delete sub/down.bin"} {
		if !strings.Contains(strings.Join(seen, "|"), want) {
			t.Errorf("records %q lack %q", seen, want)
		}
	}

	// Without files: 403 before the node, nothing changes.
	allowLogin(t, r.s, "alice", "view")
	if got = r.fileChange(t, "DELETE", "a.txt", nil); got.code != http.StatusForbidden || readFile(t, filepath.Join(dir, "a.txt")) != "new" {
		t.Fatalf("delete without files = %d %s", got.code, got.body)
	}
	if got = r.fileChange(t, "POST", "", &wire.FileChange{Op: wire.FileUpload, Name: "x", Data: []byte("x")}); got.code != http.StatusForbidden {
		t.Fatalf("upload without files = %d %s", got.code, got.body)
	}
	if got := r.fileGet(t, r.base, "files", ""); got.code != http.StatusOK {
		t.Fatalf("listing with view = %d", got.code)
	}
}
