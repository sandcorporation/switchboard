package node

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// writeReq is a write of data at p, expecting fp (nil: whatever is there).
func writeReq(p string, data []byte, fp *wire.FileFingerprint) wire.NodeFile {
	return wire.NodeFile{Ref: "w1", Op: wire.FileWrite, Path: p, Length: int64(len(data)), Expect: fp}
}

// writeAt writes data at p of dir, expecting fp.
func writeAt(dir, p string, data []byte, fp *wire.FileFingerprint) wire.NodeFileResult {
	return FolderWrite(dir, writeReq(p, data, fp), data)
}

// fingerprintOf is the fingerprint of the file at name now.
func fingerprintOf(t *testing.T, name string) *wire.FileFingerprint {
	t.Helper()
	fi, err := os.Stat(name)
	must(t, err)
	f, err := os.Open(name)
	must(t, err)
	defer f.Close()
	h, err := fileHash(f)
	must(t, err)
	return &wire.FileFingerprint{Size: fi.Size(), MTime: fi.ModTime().UnixMilli(), SHA256: h}
}

func noTemp(t *testing.T, dir string) {
	t.Helper()
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), ".switchboard-") {
			t.Errorf("temporary file left: %s", p)
		}
		return nil
	})
}

// TestFolderWriteReplacesTheFile: a write with the file's fingerprint
// replaces it, keeps its mode and answers the new stat and hash; a stale
// fingerprint is a conflict that writes nothing; no fingerprint writes.
func TestFolderWriteReplacesTheFile(t *testing.T) {
	dir, _ := folder(t)
	a := filepath.Join(dir, "a.txt")
	must(t, os.Chmod(a, 0o640))
	fp := fingerprintOf(t, a)
	res := writeAt(dir, "a.txt", []byte("new text\n"), fp)
	if res.Code != "" || res.Stat == nil || res.Stat.Size != 9 || res.Stat.Kind != wire.FileKindFile || res.Stat.SHA256 != fingerprintOf(t, a).SHA256 || res.Ref != "w1" {
		t.Fatalf("write = %+v", res)
	}
	if b, _ := os.ReadFile(a); string(b) != "new text\n" {
		t.Fatalf("file = %q", b)
	}
	if fi, _ := os.Stat(a); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	if res.Stat.MTime != fingerprintOf(t, a).MTime {
		t.Errorf("answered mtime %d, file %d", res.Stat.MTime, fingerprintOf(t, a).MTime)
	}
	// The fingerprint read before is stale.
	if res = writeAt(dir, "a.txt", []byte("lost"), fp); res.Code != wire.FileConflict || res.Stat != nil {
		t.Fatalf("stale write = %+v", res)
	}
	// Same size and time, other content: the hash tells.
	cur := fingerprintOf(t, a)
	cur.SHA256 = strings.Repeat("0", 64)
	if res = writeAt(dir, "a.txt", []byte("lost"), cur); res.Code != wire.FileConflict {
		t.Fatalf("other hash = %+v", res)
	}
	if b, _ := os.ReadFile(a); string(b) != "new text\n" {
		t.Fatalf("a conflict wrote: %q", b)
	}
	if res = writeAt(dir, "sub/b.md", []byte(""), nil); res.Code != "" || res.Stat.Size != 0 {
		t.Fatalf("write anyway = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "sub", "b.md")); len(b) != 0 {
		t.Fatalf("b.md = %q", b)
	}
	noTemp(t, filepath.Dir(dir))
}

// TestFolderWriteRefuses: what a read refuses, a link (inside or out) as
// the file named, a folder, a missing file, more than FileTextMax and a
// length that is not the bytes sent; none of them writes.
func TestFolderWriteRefuses(t *testing.T) {
	dir, outside := folder(t)
	symlink(t, outside, filepath.Join(dir, "out.txt"))
	symlink(t, filepath.Join(dir, "a.txt"), filepath.Join(dir, "in.txt"))
	symlink(t, filepath.Dir(outside), filepath.Join(dir, "up"))
	for _, c := range []struct {
		path string
		data []byte
		code string
	}{
		{"out.txt", []byte("x"), wire.FileRefused},
		{"in.txt", []byte("x"), wire.FileRefused},
		{"up/secret.txt", []byte("x"), wire.FileRefused},
		{"../secret.txt", []byte("x"), wire.FileRefused},
		{outside, []byte("x"), wire.FileRefused},
		{"a.txt:s", []byte("x"), wire.FileRefused},
		{"sub", []byte("x"), wire.FileNotFile},
		{"", []byte("x"), wire.FileNotFile},
		{"nope.txt", []byte("x"), wire.FileNotFound},
		{"a.txt", make([]byte, wire.FileTextMax+1), wire.FileTooBig},
	} {
		if res := writeAt(dir, c.path, c.data, nil); res.Code != c.code || res.Stat != nil {
			t.Errorf("write %q = %+v, want %s", c.path, res, c.code)
		}
	}
	short := writeReq("a.txt", []byte("x"), nil)
	short.Length = 2
	if res := FolderWrite(dir, short, []byte("x")); res.Code != wire.FileFailed {
		t.Errorf("short write = %+v", res)
	}
	if b, _ := os.ReadFile(outside); string(b) != "secret" {
		t.Fatalf("outside file = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "hello" {
		t.Fatalf("a.txt = %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "in.txt")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced")
	}
	if _, err := os.Stat(filepath.Join(dir, "nope.txt")); err == nil {
		t.Fatal("a write made a new file")
	}
	noTemp(t, filepath.Dir(dir))
}

// TestFolderWriteThroughALinkedFolder: a folder link that stays inside the
// session folder is followed to the file, which is written in place.
func TestFolderWriteThroughALinkedFolder(t *testing.T) {
	dir, _ := folder(t)
	symlink(t, filepath.Join(dir, "sub"), filepath.Join(dir, "alias"))
	if res := writeAt(dir, "alias/b.md", []byte("via"), nil); res.Code != "" {
		t.Fatalf("write = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "sub", "b.md")); string(b) != "via" {
		t.Fatalf("b.md = %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "alias")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the folder link was replaced")
	}
}

// TestWriteBuffers: bytes gather by ref, up to FileTextMax and one more
// byte, for at most fileWorkers refs at once; take empties the ref.
func TestWriteBuffers(t *testing.T) {
	w := writeBuffers{}
	w.add("a", []byte("he"))
	w.add("a", []byte("llo"))
	if got := string(w.take("a")); got != "hello" {
		t.Fatalf("take = %q", got)
	}
	if got := w.take("a"); got == nil || len(got) != 0 {
		t.Fatalf("take again = %v", got)
	}
	w.add("b", make([]byte, wire.FileUploadMax))
	w.add("b", make([]byte, 10))
	if n := len(w.take("b")); n != wire.FileUploadMax+1 {
		t.Fatalf("capped at %d", n)
	}
	for i := 0; i < fileWorkers+2; i++ {
		w.add(strings.Repeat("r", i+1), []byte("x"))
	}
	if len(w) != fileWorkers {
		t.Fatalf("%d refs held, want %d", len(w), fileWorkers)
	}
}
