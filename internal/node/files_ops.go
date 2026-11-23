package node

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Session folder changes of the files pane: delete (#473), rename (#474)
// and upload (#475). Each names an entry by its path in the folder; the
// path is checked as for a read (fileRelPath: no absolute path, drive, "..",
// NUL, ':' or Windows device name), the entry's folder is resolved through
// its symbolic links and must be the session folder or under it, and the
// entry itself is never followed: a symbolic link is deleted or renamed as
// the link, and an upload never writes through one. Just before the change
// the folder is resolved again and must be the same. The session folder
// itself is never deleted or renamed. Neither paths nor bytes are logged.

// named is an entry of the session folder as a change names it.
type named struct {
	root   string // the session folder, its links resolved
	folder string // the entry's folder, its links resolved, root or under it
	name   string // the entry's last segment, not followed
}

func (n named) path() string { return filepath.Join(n.folder, n.name) }

// namedEntry checks rel and resolves its folder.
func namedEntry(dir, rel string) (named, error) {
	parts, err := fileRelPath(rel, runtime.GOOS == "windows")
	if err != nil {
		return named{}, err
	}
	if len(parts) == 0 {
		return named{}, refuse("the session folder itself")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err == nil {
		root, err = filepath.Abs(root)
	}
	if err != nil {
		return named{}, &fileError{wire.FileNotFound, errors.New("session folder not found")}
	}
	folder, err := resolvedWithin(root, filepath.Join(append([]string{root}, parts[:len(parts)-1]...)...))
	if err != nil {
		return named{}, err
	}
	return named{root: root, folder: folder, name: parts[len(parts)-1]}, nil
}

// resolvedWithin resolves p through its links; it must be a folder at root
// or under it.
func resolvedWithin(root, p string) (string, error) {
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		real, err = filepath.Abs(real)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", &fileError{wire.FileNotFound, errors.New("folder not found")}
		}
		return "", &fileError{wire.FileFailed, errors.New("folder of the entry")}
	}
	if !wire.DirWithin(real, root) {
		return "", refuse("the folder resolves outside the session folder")
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() {
		return "", &fileError{wire.FileNotDir, errors.New("not a folder")}
	}
	return real, nil
}

// still checks, just before a change, that n's folder resolves where it
// did: no link swapped in for it since it was checked.
func (n named) still() error {
	folder, err := filepath.EvalSymlinks(n.folder)
	if err == nil {
		folder, err = filepath.Abs(folder)
	}
	if err != nil || folder != n.folder || !wire.DirWithin(folder, n.root) {
		return refuse("the path changed while it was changed")
	}
	return nil
}

// FolderChange answers a delete, rename or upload on the session folder
// dir; data is an upload's bytes. The result's Stat is the entry after a
// rename or an upload, none after a delete.
func FolderChange(dir string, req wire.NodeFile, data []byte) wire.NodeFileResult {
	res := wire.NodeFileResult{Ref: req.Ref}
	var st *wire.FileEntry
	var err error
	switch req.Op {
	case wire.FileDelete:
		err = folderDelete(dir, req.Path)
	case wire.FileRename:
		st, err = folderRename(dir, req.Path, req.To)
	case wire.FileUpload:
		st, err = folderUpload(dir, req, data)
	default:
		err = &fileError{wire.FileFailed, fmt.Errorf("not a change: %q", req.Op)}
	}
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
	if st == nil {
		st = &wire.FileEntry{}
	}
	res.Stat = st
	return res
}

// folderDelete removes a regular file, a symbolic link (not its target) or
// an empty folder; a folder with entries is FileNotEmpty.
func folderDelete(dir, rel string) error {
	n, err := namedEntry(dir, rel)
	if err != nil {
		return err
	}
	li, err := os.Lstat(n.path())
	if err != nil {
		return &fileError{wire.FileNotFound, fmt.Errorf("%s: not found", rel)}
	}
	if li.IsDir() {
		f, err := os.Open(n.path())
		if err != nil {
			return &fileError{wire.FileFailed, errors.New("open folder")}
		}
		_, err = f.Readdirnames(1)
		f.Close()
		if !errors.Is(err, io.EOF) {
			return &fileError{wire.FileNotEmpty, fmt.Errorf("%s: the folder is not empty", rel)}
		}
	}
	if err := n.still(); err != nil {
		return err
	}
	if err := os.Remove(n.path()); err != nil {
		if li.IsDir() {
			return &fileError{wire.FileNotEmpty, fmt.Errorf("%s: the folder is not empty", rel)}
		}
		return &fileError{wire.FileFailed, errors.New("delete failed")}
	}
	return nil
}

// folderRename gives rel the name to, in the same folder. A name already
// there is FileExists, unless it is the same entry (a change of case on a
// folder that ignores case).
func folderRename(dir, rel, to string) (*wire.FileEntry, error) {
	n, err := namedEntry(dir, rel)
	if err != nil {
		return nil, err
	}
	seg, err := fileRelPath(to, runtime.GOOS == "windows")
	if err != nil {
		return nil, err
	}
	if len(seg) != 1 {
		return nil, refuse("the new name is one name, in the same folder")
	}
	li, err := os.Lstat(n.path())
	if err != nil {
		return nil, &fileError{wire.FileNotFound, fmt.Errorf("%s: not found", rel)}
	}
	dest := filepath.Join(n.folder, seg[0])
	if seg[0] == n.name {
		st := entryOf(seg[0], li)
		return &st, nil
	}
	if di, err := os.Lstat(dest); err == nil && !os.SameFile(li, di) {
		return nil, &fileError{wire.FileExists, fmt.Errorf("%s is already there", seg[0])}
	}
	if err := n.still(); err != nil {
		return nil, err
	}
	if err := os.Rename(n.path(), dest); err != nil {
		return nil, &fileError{wire.FileFailed, errors.New("rename failed")}
	}
	now, err := os.Lstat(dest)
	if err != nil {
		return nil, &fileError{wire.FileFailed, err}
	}
	st := entryOf(seg[0], now)
	return &st, nil
}

// folderUpload puts data in a new regular file at req.Path, written to a
// temporary file beside it and renamed. An entry already there is
// FileExists, unless req.Replace and it is a regular file, whose mode the
// new one keeps.
func folderUpload(dir string, req wire.NodeFile, data []byte) (*wire.FileEntry, error) {
	switch {
	case len(data) > wire.FileUploadMax:
		return nil, &fileError{wire.FileTooBig, fmt.Errorf("more than %d bytes", wire.FileUploadMax)}
	case int64(len(data)) != req.Length:
		return nil, &fileError{wire.FileFailed, errors.New("the bytes sent are not the length asked")}
	}
	n, err := namedEntry(dir, req.Path)
	if err != nil {
		return nil, err
	}
	mode := fs.FileMode(0o644)
	var old fs.FileInfo
	if li, err := os.Lstat(n.path()); err == nil {
		switch {
		case !req.Replace:
			return nil, &fileError{wire.FileExists, fmt.Errorf("%s is already there", n.name)}
		case li.Mode()&fs.ModeSymlink != 0:
			return nil, refuse("%s is a symbolic link", n.name)
		case !li.Mode().IsRegular():
			return nil, &fileError{wire.FileNotFile, fmt.Errorf("%s is not a regular file", n.name)}
		}
		old, mode = li, li.Mode().Perm()
	}
	tmp, err := os.CreateTemp(n.folder, "."+n.name+".switchboard-*")
	if err != nil {
		return nil, &fileError{wire.FileFailed, errors.New("temporary file")}
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return nil, &fileError{wire.FileFailed, errors.New("write failed")}
	}
	if err := tmp.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		return nil, &fileError{wire.FileFailed, errors.New("mode failed")}
	}
	if err := tmp.Sync(); err != nil {
		return nil, &fileError{wire.FileFailed, errors.New("sync failed")}
	}
	if err := tmp.Close(); err != nil {
		return nil, &fileError{wire.FileFailed, errors.New("close failed")}
	}
	// Again just before the rename: the folder is where it was, and the
	// name is what it was (nothing new without Replace, the same regular
	// file with it).
	if err := n.still(); err != nil {
		return nil, err
	}
	li, err := os.Lstat(n.path())
	switch {
	case old == nil && err == nil:
		return nil, &fileError{wire.FileExists, fmt.Errorf("%s is already there", n.name)}
	case old != nil && (err != nil || !os.SameFile(old, li)):
		return nil, refuse("%s changed while it was uploaded", n.name)
	}
	if err := os.Rename(tmp.Name(), n.path()); err != nil {
		return nil, &fileError{wire.FileFailed, errors.New("rename failed")}
	}
	keep = true
	now, err := os.Lstat(n.path())
	if err != nil {
		return nil, &fileError{wire.FileFailed, err}
	}
	st := entryOf(n.name, now)
	return &st, nil
}

// FileChange answers a delete, rename or upload frame for record id; data
// is an upload's bytes, sent before it. A record not on this node is not
// found.
func (n *Node) FileChange(id string, req wire.NodeFile, data []byte) wire.NodeFileResult {
	dir, ok := n.recordDir(id)
	if !ok {
		return wire.NodeFileResult{Ref: req.Ref, Code: wire.FileNotFound, Error: "record " + id + " is not on this node"}
	}
	return FolderChange(dir, req, data)
}
