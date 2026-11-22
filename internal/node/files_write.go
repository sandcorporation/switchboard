package node

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Session folder writes (#448). A save replaces a regular file that is
// already in the folder: the new bytes go to a temporary file beside it,
// which is then renamed over it. The path is checked as for a read
// (folderTarget, openWithin), its last segment must not be a symbolic link
// (a link is never written through, nor replaced), and the file must still
// have the fingerprint the dashboard read, unless the admin chose to save
// anyway. Neither the bytes nor the path are logged.

// fileHash is the hex SHA-256 of r.
func fileHash(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FolderWrite answers a write on the session folder dir: data replaces the
// regular file at req.Path. The result's Stat is the file written, with its
// SHA256.
func FolderWrite(dir string, req wire.NodeFile, data []byte) wire.NodeFileResult {
	res := wire.NodeFileResult{Ref: req.Ref}
	st, err := folderWrite(dir, req, data)
	if err != nil {
		var fe *fileError
		if errors.As(err, &fe) {
			res.Code = fe.code
		} else {
			res.Code = wire.FileFailed
		}
		res.Error = err.Error()
		return res
	}
	res.Stat = &st
	return res
}

func folderWrite(dir string, req wire.NodeFile, data []byte) (wire.FileEntry, error) {
	switch {
	case req.Op != wire.FileWrite:
		return wire.FileEntry{}, &fileError{wire.FileFailed, fmt.Errorf("not a write: %q", req.Op)}
	case len(data) > wire.FileTextMax:
		return wire.FileEntry{}, &fileError{wire.FileTooBig, fmt.Errorf("more than %d bytes", wire.FileTextMax)}
	case int64(len(data)) != req.Length:
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("the bytes sent are not the length asked")}
	}
	parts, err := fileRelPath(req.Path, runtime.GOOS == "windows")
	if err != nil {
		return wire.FileEntry{}, err
	}
	if len(parts) == 0 {
		return wire.FileEntry{}, &fileError{wire.FileNotFile, errors.New("the session folder is not a file")}
	}
	root, real, err := folderTarget(dir, req.Path)
	if err != nil {
		return wire.FileEntry{}, err
	}
	// The path as named, its folders resolved: its last segment must be
	// the file itself, not a link to it.
	parent, err := filepath.EvalSymlinks(filepath.Join(append([]string{root}, parts[:len(parts)-1]...)...))
	if err == nil {
		parent, err = filepath.Abs(parent)
	}
	if err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("folder of the file")}
	}
	li, err := os.Lstat(filepath.Join(parent, parts[len(parts)-1]))
	if err != nil || li.Mode()&fs.ModeSymlink != 0 {
		return wire.FileEntry{}, refuse("%s is a symbolic link", req.Path)
	}
	f, fi, err := openWithin(root, real)
	if err != nil {
		return wire.FileEntry{}, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return wire.FileEntry{}, &fileError{wire.FileNotFile, errors.New("not a regular file")}
	}
	if e := req.Expect; e != nil {
		same := e.Size == fi.Size() && e.MTime == fi.ModTime().UnixMilli()
		if same {
			h, err := fileHash(f)
			if err != nil {
				f.Close()
				return wire.FileEntry{}, &fileError{wire.FileFailed, err}
			}
			same = h == e.SHA256
		}
		if !same {
			f.Close()
			return wire.FileEntry{}, &fileError{wire.FileConflict, errors.New("the file changed since it was read")}
		}
	}
	f.Close()
	folder := filepath.Dir(real)
	tmp, err := os.CreateTemp(folder, "."+filepath.Base(real)+".switchboard-*")
	if err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, fmt.Errorf("temporary file: %w", errors.Unwrap(err))}
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("write failed")}
	}
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil && runtime.GOOS != "windows" {
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("mode failed")}
	}
	if err := tmp.Sync(); err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("sync failed")}
	}
	if err := tmp.Close(); err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("close failed")}
	}
	// Check again just before the rename: the name is still the file that
	// was opened (no link swapped in) and its folder is still in the root.
	li, err = os.Lstat(real)
	again, err2 := filepath.EvalSymlinks(folder)
	if err2 == nil {
		again, err2 = filepath.Abs(again)
	}
	if err != nil || err2 != nil || li.Mode()&fs.ModeSymlink != 0 || !os.SameFile(fi, li) || !wire.DirWithin(again, root) {
		return wire.FileEntry{}, refuse("the path changed while it was written")
	}
	if err := os.Rename(tmp.Name(), real); err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, errors.New("rename failed")}
	}
	keep = true
	now, err := os.Lstat(real)
	if err != nil {
		return wire.FileEntry{}, &fileError{wire.FileFailed, err}
	}
	st := entryOf(filepath.Base(real), now)
	sum := sha256.Sum256(data)
	st.SHA256 = hex.EncodeToString(sum[:])
	return st, nil
}

// FileWrite answers a write frame for record id with data, the bytes the
// host sent before it. A record not on this node is not found.
func (n *Node) FileWrite(id string, req wire.NodeFile, data []byte) wire.NodeFileResult {
	dir, ok := n.recordDir(id)
	if !ok {
		return wire.NodeFileResult{Ref: req.Ref, Code: wire.FileNotFound, Error: "record " + id + " is not on this node"}
	}
	return FolderWrite(dir, req, data)
}
