package node

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Session folder files (#447). The node is the boundary: whatever the host
// or the browser checked, a request here names a path relative to the
// record's directory, and the node answers only for what, with every
// symbolic link resolved, is that directory or under it.

// errRefused is a path that is not a plain relative path inside the folder.
var errRefused = errors.New("path refused")

// fileError is a failed file request: its wire code and the error.
type fileError struct {
	code string
	err  error
}

func (e *fileError) Error() string { return e.err.Error() }

func (e *fileError) Unwrap() error { return e.err }

func refuse(format string, a ...any) *fileError {
	return &fileError{wire.FileRefused, fmt.Errorf("%w: "+format, append([]any{errRefused}, a...)...)}
}

// windowsDevices are the names Windows maps to a device in any folder and
// with any extension (CON, CON.txt).
var windowsDevices = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true, "CLOCK$": true}

func init() {
	for _, p := range []string{"COM", "LPT"} {
		for _, d := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
			windowsDevices[p+d] = true
		}
	}
}

// fileRelPath checks p, a path relative to a session folder as the host
// sends it, and returns its segments (none for the folder itself). It
// refuses an absolute path, a ".." segment, NUL, a drive letter, a UNC
// path, ':' (an alternate data stream, on any node) and an empty segment;
// with windows it also refuses a device name (CON, nul.txt) and a segment
// ending in a dot or a space, which Windows strips. Both / and \ separate.
func fileRelPath(p string, windows bool) ([]string, error) {
	switch {
	case strings.ContainsRune(p, 0):
		return nil, refuse("NUL in path")
	case len(p) > 4096:
		return nil, refuse("path too long")
	case strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`):
		return nil, refuse("absolute path %q", p)
	case len(p) >= 2 && p[1] == ':' && (p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z'):
		return nil, refuse("drive letter in %q", p)
	case strings.ContainsRune(p, ':'):
		return nil, refuse("':' in %q", p)
	}
	if p == "" || p == "." {
		return nil, nil
	}
	var out []string
	for _, s := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		switch {
		case s == ".":
			continue
		case s == "..":
			return nil, refuse("'..' in %q", p)
		case windows && strings.TrimRight(s, ". ") != s:
			return nil, refuse("segment %q ends in a dot or a space", s)
		case windows && windowsDevice(s):
			return nil, refuse("device name %q", s)
		}
		out = append(out, s)
	}
	if strings.Contains(strings.ReplaceAll(p, `\`, "/"), "//") {
		return nil, refuse("empty segment in %q", p)
	}
	return out, nil
}

// windowsDevice reports whether segment s names a Windows device: its part
// before the first dot, trailing spaces dropped, in any case.
func windowsDevice(s string) bool {
	base, _, _ := strings.Cut(s, ".")
	return windowsDevices[strings.ToUpper(strings.TrimRight(base, " "))]
}

// folderTarget resolves rel under the session folder dir: the folder
// itself through its symbolic links (root), and the target with every link
// resolved, which must be root or under it (wire.DirWithin: case-blind on
// Windows).
func folderTarget(dir, rel string) (root, real string, err error) {
	parts, err := fileRelPath(rel, runtime.GOOS == "windows")
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", "", &fileError{wire.FileNotFound, fmt.Errorf("session folder: %w", err)}
	}
	if root, err = filepath.Abs(root); err != nil {
		return "", "", &fileError{wire.FileFailed, err}
	}
	real, err = filepath.EvalSymlinks(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", &fileError{wire.FileNotFound, fmt.Errorf("%s: not found", rel)}
		}
		return "", "", &fileError{wire.FileFailed, fmt.Errorf("%s: %w", rel, err)}
	}
	if real, err = filepath.Abs(real); err != nil {
		return "", "", &fileError{wire.FileFailed, err}
	}
	if !wire.DirWithin(real, root) {
		return "", "", refuse("%s resolves outside the session folder", rel)
	}
	return root, real, nil
}

// openWithin opens real (a folderTarget) and checks again, on what it
// opened: the path, resolved once more, is still inside root and is the
// same file as the one open, so a link swapped in between the check and the
// open is refused.
func openWithin(root, real string) (*os.File, fs.FileInfo, error) {
	pre, err := os.Stat(real)
	if err != nil {
		return nil, nil, &fileError{wire.FileNotFound, errors.New("not found")}
	}
	if !pre.Mode().IsRegular() && !pre.IsDir() {
		// A FIFO or a device would block or never end.
		return nil, nil, &fileError{wire.FileNotFile, errors.New("not a regular file or directory")}
	}
	f, err := os.OpenFile(real, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return nil, nil, &fileError{wire.FileFailed, err}
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, &fileError{wire.FileFailed, err}
	}
	again, err := filepath.EvalSymlinks(real)
	if err == nil {
		again, err = filepath.Abs(again)
	}
	var now fs.FileInfo
	if err == nil {
		now, err = os.Stat(again)
	}
	if err != nil || !wire.DirWithin(again, root) || !os.SameFile(fi, now) {
		f.Close()
		return nil, nil, refuse("the path changed while it was opened")
	}
	if !fi.Mode().IsRegular() && !fi.IsDir() {
		f.Close()
		return nil, nil, &fileError{wire.FileNotFile, errors.New("not a regular file or directory")}
	}
	return f, fi, nil
}

func entryOf(name string, fi fs.FileInfo) wire.FileEntry {
	kind := wire.FileKindOther
	switch m := fi.Mode(); {
	case m&fs.ModeSymlink != 0:
		kind = wire.FileKindLink
	case m.IsDir():
		kind = wire.FileKindDir
	case m.IsRegular():
		kind = wire.FileKindFile
	}
	size := fi.Size()
	if kind != wire.FileKindFile {
		size = 0
	}
	return wire.FileEntry{Name: name, Kind: kind, Size: size, MTime: fi.ModTime().UnixMilli()}
}

// FolderFile answers one file request on the session folder dir: the
// result and, for a read, the bytes read. It never writes (FolderWrite
// does).
func FolderFile(dir string, req wire.NodeFile) (wire.NodeFileResult, []byte) {
	res := wire.NodeFileResult{Ref: req.Ref}
	data, err := folderFile(dir, req, &res)
	if err != nil {
		var fe *fileError
		if errors.As(err, &fe) {
			res.Code = fe.code
		} else {
			res.Code = wire.FileFailed
		}
		res.Error = err.Error()
		res.Stat, res.Entries, res.Truncated, res.Length = nil, nil, false, 0
		return res, nil
	}
	return res, data
}

func folderFile(dir string, req wire.NodeFile, res *wire.NodeFileResult) ([]byte, error) {
	switch req.Op {
	case wire.FileList, wire.FileStat, wire.FileRead:
	default:
		return nil, &fileError{wire.FileFailed, fmt.Errorf("unknown file op %q", req.Op)}
	}
	root, real, err := folderTarget(dir, req.Path)
	if err != nil {
		return nil, err
	}
	f, fi, err := openWithin(root, real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st := entryOf(filepath.Base(real), fi)
	if real == root {
		st.Name = ""
	}
	res.Stat = &st
	switch req.Op {
	case wire.FileList:
		if !fi.IsDir() {
			return nil, &fileError{wire.FileNotDir, errors.New("not a directory")}
		}
		ents, err := f.ReadDir(wire.FileListMax + 1)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, &fileError{wire.FileFailed, err}
		}
		if len(ents) > wire.FileListMax {
			ents, res.Truncated = ents[:wire.FileListMax], true
		}
		res.Entries = make([]wire.FileEntry, 0, len(ents))
		for _, e := range ents {
			info, err := e.Info()
			if err != nil {
				continue // gone since the listing
			}
			res.Entries = append(res.Entries, entryOf(e.Name(), info))
		}
		sort.Slice(res.Entries, func(i, j int) bool {
			a, b := res.Entries[i], res.Entries[j]
			if (a.Kind == wire.FileKindDir) != (b.Kind == wire.FileKindDir) {
				return a.Kind == wire.FileKindDir
			}
			return a.Name < b.Name
		})
	case wire.FileRead:
		if !fi.Mode().IsRegular() {
			return nil, &fileError{wire.FileNotFile, errors.New("not a regular file")}
		}
		n := req.Length
		if n <= 0 || n > wire.FileChunkMax {
			n = wire.FileChunkMax
		}
		if req.Offset < 0 {
			return nil, &fileError{wire.FileFailed, errors.New("negative offset")}
		}
		if req.Offset >= fi.Size() {
			return []byte{}, nil
		}
		if rest := fi.Size() - req.Offset; n > rest {
			n = rest
		}
		buf := make([]byte, n)
		got, err := f.ReadAt(buf, req.Offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, &fileError{wire.FileFailed, err}
		}
		res.Length = int64(got)
		return buf[:got], nil
	}
	return nil, nil
}

// File answers a file frame for record id: the record's directory is the
// session folder. A record not on this node is not found.
func (n *Node) File(id string, req wire.NodeFile) (wire.NodeFileResult, []byte) {
	dir, found := n.recordDir(id)
	if !found {
		return wire.NodeFileResult{Ref: req.Ref, Code: wire.FileNotFound, Error: "record " + id + " is not on this node"}, nil
	}
	return FolderFile(dir, req)
}

// recordDir is the directory of record id on this node.
func (n *Node) recordDir(id string) (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, r := range n.records {
		if r.ID == id {
			return r.Directory, true
		}
	}
	return "", false
}
