package node

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func change(dir string, req wire.NodeFile, data []byte) wire.NodeFileResult {
	req.Ref = "c1"
	return FolderChange(dir, req, data)
}

func upload(dir, p string, data []byte, replace bool) wire.NodeFileResult {
	return change(dir, wire.NodeFile{Op: wire.FileUpload, Path: p, Length: int64(len(data)), Replace: replace}, data)
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// TestFolderDelete (#473): a file, a link (not its target) and an empty
// folder go; a folder with entries stays (not-empty).
func TestFolderDelete(t *testing.T) {
	dir, outside := folder(t)
	must(t, os.Mkdir(filepath.Join(dir, "empty"), 0o755))
	del := func(p string) wire.NodeFileResult {
		return change(dir, wire.NodeFile{Op: wire.FileDelete, Path: p}, nil)
	}
	if res := del("sub/b.md"); res.Code != "" || res.Ref != "c1" || res.Stat == nil {
		t.Fatalf("delete file = %+v", res)
	}
	if exists(filepath.Join(dir, "sub", "b.md")) {
		t.Fatal("b.md is still there")
	}
	if res := del("empty"); res.Code != "" || exists(filepath.Join(dir, "empty")) {
		t.Fatalf("delete empty folder = %+v", res)
	}
	must(t, os.WriteFile(filepath.Join(dir, "sub", "c"), nil, 0o644))
	if res := del("sub"); res.Code != wire.FileNotEmpty || !exists(filepath.Join(dir, "sub", "c")) {
		t.Fatalf("delete full folder = %+v", res)
	}
	symlink(t, outside, filepath.Join(dir, "out.txt"))
	if res := del("out.txt"); res.Code != "" || exists(filepath.Join(dir, "out.txt")) || !exists(outside) {
		t.Fatalf("delete link = %+v; the target must stay", res)
	}
}

// TestFolderRename (#474): in the same folder only, never onto a name that
// is there.
func TestFolderRename(t *testing.T) {
	dir, _ := folder(t)
	ren := func(p, to string) wire.NodeFileResult {
		return change(dir, wire.NodeFile{Op: wire.FileRename, Path: p, To: to}, nil)
	}
	res := ren("sub/b.md", "c.md")
	if res.Code != "" || res.Stat == nil || res.Stat.Name != "c.md" || res.Stat.Size != 3 {
		t.Fatalf("rename = %+v", res)
	}
	if exists(filepath.Join(dir, "sub", "b.md")) || !exists(filepath.Join(dir, "sub", "c.md")) {
		t.Fatal("not renamed")
	}
	if res := ren("sub", "src"); res.Code != "" || !exists(filepath.Join(dir, "src", "c.md")) {
		t.Fatalf("rename folder = %+v", res)
	}
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644))
	if res := ren("a.txt", "b.txt"); res.Code != wire.FileExists {
		t.Fatalf("rename onto b.txt = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "b" {
		t.Fatalf("b.txt = %q", b)
	}
	if res := ren("nope", "x"); res.Code != wire.FileNotFound {
		t.Fatalf("rename missing = %+v", res)
	}
}

// TestFolderUpload (#475): a new file; an existing name only with replace,
// and then only a regular file, keeping its mode.
func TestFolderUpload(t *testing.T) {
	dir, _ := folder(t)
	res := upload(dir, "sub/new.bin", []byte{0, 1, 2}, false)
	if res.Code != "" || res.Stat == nil || res.Stat.Name != "new.bin" || res.Stat.Size != 3 {
		t.Fatalf("upload = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "sub", "new.bin")); string(b) != "\x00\x01\x02" {
		t.Fatalf("new.bin = %q", b)
	}
	if res := upload(dir, "a.txt", []byte("bye"), false); res.Code != wire.FileExists {
		t.Fatalf("upload over a.txt = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "hello" {
		t.Fatalf("a.txt = %q", b)
	}
	must(t, os.Chmod(filepath.Join(dir, "a.txt"), 0o600))
	if res := upload(dir, "a.txt", []byte("bye"), true); res.Code != "" || res.Stat.Size != 3 {
		t.Fatalf("replace a.txt = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "bye" {
		t.Fatalf("a.txt = %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "a.txt")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if res := upload(dir, "sub", []byte("x"), true); res.Code != wire.FileNotFile {
		t.Fatalf("replace a folder = %+v", res)
	}
	if res := upload(dir, "big", make([]byte, wire.FileUploadMax+1), false); res.Code != wire.FileTooBig || exists(filepath.Join(dir, "big")) {
		t.Fatalf("too big = %+v", res)
	}
	short := wire.NodeFile{Op: wire.FileUpload, Path: "s", Length: 2}
	if res := change(dir, short, []byte("x")); res.Code != wire.FileFailed {
		t.Fatalf("short = %+v", res)
	}
	noTemp(t, dir)
}

// TestFolderChangeBoundary: no change reaches outside the session folder or
// the folder itself: "..", absolute paths, ':' and a folder that is a link
// out are refused, and a link inside is never followed.
func TestFolderChangeBoundary(t *testing.T) {
	dir, outside := folder(t)
	symlink(t, filepath.Dir(outside), filepath.Join(dir, "up"))
	symlink(t, outside, filepath.Join(dir, "out.txt"))
	for _, req := range []wire.NodeFile{
		{Op: wire.FileDelete, Path: ""},
		{Op: wire.FileDelete, Path: "../secret.txt"},
		{Op: wire.FileDelete, Path: outside},
		{Op: wire.FileDelete, Path: "up/secret.txt"},
		{Op: wire.FileDelete, Path: "a.txt:s"},
		{Op: wire.FileRename, Path: "", To: "x"},
		{Op: wire.FileRename, Path: "a.txt", To: "../a.txt"},
		{Op: wire.FileRename, Path: "a.txt", To: "sub/a.txt"},
		{Op: wire.FileRename, Path: "a.txt", To: outside},
		{Op: wire.FileRename, Path: "up/secret.txt", To: "x"},
		{Op: wire.FileUpload, Path: "up/x", Length: 1},
		{Op: wire.FileUpload, Path: "../x", Length: 1},
		{Op: wire.FileUpload, Path: "out.txt", Length: 1, Replace: true},
	} {
		if res := change(dir, req, []byte("x")); res.Code != wire.FileRefused {
			t.Errorf("%s %q→%q = %+v, want refused", req.Op, req.Path, req.To, res)
		}
	}
	if b, _ := os.ReadFile(outside); string(b) != "secret" {
		t.Fatalf("outside = %q", b)
	}
	if exists(filepath.Join(filepath.Dir(outside), "x")) || !exists(filepath.Join(dir, "a.txt")) {
		t.Fatal("a refused change happened")
	}
}
