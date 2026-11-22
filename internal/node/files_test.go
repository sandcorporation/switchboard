package node

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func TestFileRelPathRefuses(t *testing.T) {
	both := []string{
		"/etc/passwd", `\etc`, `\\server\share\x`, "//server/share",
		"..", "../x", "a/../../x", `a\..\x`, "a/..",
		"a\x00b", "C:x", `c:\Windows`, "D:/x",
		"file.txt:stream", "a:b/c", "a//b", `a\\b`,
		strings.Repeat("a", 5000),
	}
	for _, p := range both {
		for _, win := range []bool{false, true} {
			if _, err := fileRelPath(p, win); !errors.Is(err, errRefused) {
				t.Errorf("fileRelPath(%.40q, windows=%v) = %v, want refused", p, win, err)
			}
		}
	}
	windowsOnly := []string{"CON", "con", "nul.txt", "a/AUX", `x\com1.log`, "LPT9", "CONIN$", "prn .txt", "COM²", "dir./x", "name ", "x/y."}
	for _, p := range windowsOnly {
		if _, err := fileRelPath(p, true); !errors.Is(err, errRefused) {
			t.Errorf("fileRelPath(%q, windows) = %v, want refused", p, err)
		}
		if _, err := fileRelPath(p, false); err != nil {
			t.Errorf("fileRelPath(%q, linux) = %v, want accepted", p, err)
		}
	}
	ok := map[string]string{"": "", ".": "", "a": "a", "a/b.txt": "a/b.txt", `a\b`: "a/b", "./a/./b": "a/b", "dir/": "dir", "..a/b..c": "..a/b..c", "console.log": "console.log", "COM10": "COM10"}
	for p, want := range ok {
		for _, win := range []bool{false, true} {
			got, err := fileRelPath(p, win)
			if err != nil || strings.Join(got, "/") != want {
				t.Errorf("fileRelPath(%q, windows=%v) = %q, %v; want %q", p, win, got, err, want)
			}
		}
	}
}

// folder makes a session folder with a file, a sub folder and a file
// outside it, all under t.TempDir.
func folder(t *testing.T) (dir, outside string) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "work")
	outside = filepath.Join(base, "secret.txt")
	must(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "sub", "b.md"), []byte("# b"), 0o644))
	must(t, os.WriteFile(outside, []byte("secret"), 0o600))
	return dir, outside
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
}

func TestFolderFileListStatRead(t *testing.T) {
	dir, _ := folder(t)
	res, data := FolderFile(dir, wire.NodeFile{Ref: "r1", Op: wire.FileList, Path: ""})
	if res.Code != "" || res.Ref != "r1" || data != nil {
		t.Fatalf("list = %+v", res)
	}
	if len(res.Entries) != 2 || res.Entries[0].Name != "sub" || res.Entries[0].Kind != wire.FileKindDir || res.Entries[1].Name != "a.txt" || res.Entries[1].Size != 5 {
		t.Fatalf("entries = %+v, want sub (dir) then a.txt (5)", res.Entries)
	}
	if res.Stat == nil || res.Stat.Kind != wire.FileKindDir {
		t.Fatalf("stat of the folder = %+v", res.Stat)
	}
	res, _ = FolderFile(dir, wire.NodeFile{Op: wire.FileStat, Path: "sub/b.md"})
	if res.Code != "" || res.Stat.Name != "b.md" || res.Stat.Kind != wire.FileKindFile || res.Stat.Size != 3 {
		t.Fatalf("stat = %+v", res)
	}
	res, data = FolderFile(dir, wire.NodeFile{Op: wire.FileRead, Path: "a.txt", Offset: 1, Length: 3})
	if res.Code != "" || string(data) != "ell" || res.Length != 3 || res.Stat.Size != 5 {
		t.Fatalf("read = %+v %q", res, data)
	}
	res, data = FolderFile(dir, wire.NodeFile{Op: wire.FileRead, Path: "a.txt", Offset: 9})
	if res.Code != "" || len(data) != 0 {
		t.Fatalf("read past the end = %+v %q", res, data)
	}
	for _, c := range []struct {
		req  wire.NodeFile
		code string
	}{
		{wire.NodeFile{Op: wire.FileRead, Path: "sub"}, wire.FileNotFile},
		{wire.NodeFile{Op: wire.FileList, Path: "a.txt"}, wire.FileNotDir},
		{wire.NodeFile{Op: wire.FileStat, Path: "nope"}, wire.FileNotFound},
		{wire.NodeFile{Op: wire.FileRead, Path: "../secret.txt"}, wire.FileRefused},
		{wire.NodeFile{Op: "write", Path: "a.txt"}, wire.FileFailed},
	} {
		res, data := FolderFile(dir, c.req)
		if res.Code != c.code || data != nil || res.Stat != nil {
			t.Errorf("%+v = %+v, want code %s and nothing else", c.req, res, c.code)
		}
	}
}

func TestFolderFileChunkAndListLimits(t *testing.T) {
	dir := t.TempDir()
	big := bytes.Repeat([]byte("x"), wire.FileChunkMax+10)
	must(t, os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o644))
	res, data := FolderFile(dir, wire.NodeFile{Op: wire.FileRead, Path: "big.bin", Length: 5 << 20})
	if res.Code != "" || len(data) != wire.FileChunkMax || res.Length != wire.FileChunkMax || res.Stat.Size != int64(len(big)) {
		t.Fatalf("read of %d bytes asking 5 MiB = %d bytes, %+v", len(big), len(data), res.Stat)
	}
	res, data = FolderFile(dir, wire.NodeFile{Op: wire.FileRead, Path: "big.bin", Offset: wire.FileChunkMax})
	if len(data) != 10 {
		t.Fatalf("the rest = %d bytes, want 10", len(data))
	}
	many := filepath.Join(dir, "many")
	must(t, os.Mkdir(many, 0o755))
	for i := 0; i <= wire.FileListMax; i++ {
		must(t, os.WriteFile(filepath.Join(many, "f"+strconv.Itoa(i)), nil, 0o644))
	}
	res, _ = FolderFile(dir, wire.NodeFile{Op: wire.FileList, Path: "many"})
	if len(res.Entries) != wire.FileListMax || !res.Truncated {
		t.Fatalf("listing of %d = %d entries, truncated %v", wire.FileListMax+1, len(res.Entries), res.Truncated)
	}
}

func TestFolderFileLinks(t *testing.T) {
	dir, outside := folder(t)
	symlink(t, outside, filepath.Join(dir, "out.txt"))
	symlink(t, filepath.Dir(outside), filepath.Join(dir, "up"))
	symlink(t, filepath.Join(dir, "sub", "b.md"), filepath.Join(dir, "in.md"))
	for _, p := range []string{"out.txt", "up/secret.txt", "up"} {
		for _, op := range []string{wire.FileStat, wire.FileRead, wire.FileList} {
			res, data := FolderFile(dir, wire.NodeFile{Op: op, Path: p})
			if res.Code != wire.FileRefused || data != nil {
				t.Errorf("%s %s = %+v %q, want refused", op, p, res, data)
			}
		}
	}
	res, data := FolderFile(dir, wire.NodeFile{Op: wire.FileRead, Path: "in.md"})
	if res.Code != "" || string(data) != "# b" {
		t.Fatalf("a link inside = %+v %q", res, data)
	}
	res, _ = FolderFile(dir, wire.NodeFile{Op: wire.FileList})
	kinds := map[string]string{}
	for _, e := range res.Entries {
		kinds[e.Name] = e.Kind
	}
	if kinds["out.txt"] != wire.FileKindLink || kinds["up"] != wire.FileKindLink {
		t.Fatalf("links are listed as links, not followed: %v", kinds)
	}
	// The session folder itself through a link: its resolved path is the root.
	alias := filepath.Join(filepath.Dir(dir), "alias")
	symlink(t, dir, alias)
	res, data = FolderFile(alias, wire.NodeFile{Op: wire.FileRead, Path: "a.txt"})
	if res.Code != "" || string(data) != "hello" {
		t.Fatalf("through a linked folder = %+v %q", res, data)
	}
	res, _ = FolderFile(alias, wire.NodeFile{Op: wire.FileRead, Path: "out.txt"})
	if res.Code != wire.FileRefused {
		t.Fatalf("outside through a linked folder = %+v", res)
	}
}

// TestOpenWithinRefusesSwap swaps the checked file for a link to a file
// outside between the check and the open: what was opened is checked
// again and refused.
func TestOpenWithinRefusesSwap(t *testing.T) {
	dir, outside := folder(t)
	root, real, err := folderTarget(dir, "a.txt")
	must(t, err)
	must(t, os.Remove(real))
	symlink(t, outside, real)
	if f, _, err := openWithin(root, real); !errors.Is(err, errRefused) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("open after a swap = %v, want refused", err)
	}
	// A folder swapped for a link to one outside, under a checked path.
	root, real, err = folderTarget(dir, "sub")
	must(t, err)
	must(t, os.RemoveAll(real))
	symlink(t, filepath.Dir(outside), real)
	if f, _, err := openWithin(root, real); !errors.Is(err, errRefused) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("open of a swapped folder = %v, want refused", err)
	}
}

func TestFolderFileCase(t *testing.T) {
	dir, _ := folder(t)
	res, _ := FolderFile(dir, wire.NodeFile{Op: wire.FileStat, Path: "SUB/B.MD"})
	switch runtime.GOOS {
	case "windows":
		// NTFS is case-blind, and so is wire.DirWithin for C:\ paths.
		if res.Code != "" {
			t.Fatalf("SUB/B.MD on Windows = %+v, want found", res)
		}
	case "linux":
		if res.Code != wire.FileNotFound {
			t.Fatalf("SUB/B.MD on Linux = %+v, want not found", res)
		}
	}
	if !wire.DirWithin(`c:\Work\App\x.txt`, `C:\work\app`) || wire.DirWithin("/work/App/x", "/work/app") {
		t.Fatal("DirWithin: case-blind for Windows paths only")
	}
}

func TestNodeFileUnknownRecord(t *testing.T) {
	n := &Node{}
	res, data := n.File("hs_none", wire.NodeFile{Ref: "r", Op: wire.FileList})
	if res.Code != wire.FileNotFound || res.Ref != "r" || data != nil {
		t.Fatalf("unknown record = %+v", res)
	}
}
