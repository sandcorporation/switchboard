package wire

import (
	"strconv"
	"strings"
)

// Session folder files (#447): the host asks a node for a listing, a stat
// or a piece of a file under a hosted record's directory with a file frame,
// and the node answers with one file-result frame of the same Ref; a read's
// bytes come first, in one binary frame of kind FileData whose id is the
// Ref. The node enforces the boundary (internal/node/files.go); a node
// sends file frames only to a host whose node advertised
// config.NodeFilesHeaderName, and both sides ignore frame types and binary
// kinds they do not know.
//
// A write (#448) goes the other way: the host sends the new bytes first, in
// FileData frames of at most FileChunkMax whose id is the Ref, then the
// file frame (Op FileWrite, Length the total), only to a node that also
// advertised config.NodeFileWriteHeaderName. The node answers with one
// file-result frame whose Stat is the file written, with its SHA256.
const (
	NodeFileType       = "file"        // host to node: File, ID is the record
	NodeFileResultType = "file-result" // node to host: FileResult
)

// FileData is the binary frame kind carrying a read's bytes, node to host,
// or a write's, host to node: EncodeTermFrame(FileData, ref, bytes).
const FileData byte = 'f'

// File operations.
const (
	FileList  = "list"  // the entries of a directory, at most FileListMax
	FileStat  = "stat"  // what the path is
	FileRead  = "read"  // Length bytes from Offset, at most FileChunkMax
	FileWrite = "write" // replace a regular file with Length bytes (#448)
	// Changes of the files pane, only to a node that advertised
	// config.NodeFileOpsHeaderName.
	FileDelete = "delete" // remove a file, a symbolic link or an empty folder (#473)
	FileRename = "rename" // give Path the name To, in the same folder (#474)
	FileUpload = "upload" // a new file of Length bytes at Path; with Replace over a regular file (#475)
)

// Limits of the file path (#443 §4).
const (
	FileListMax   = 1000    // entries in one listing
	FileChunkMax  = 1 << 20 // bytes in one read
	FileTextMax   = 2 << 20 // a text file the dashboard opens whole, and a write
	FileUploadMax = 4 << 20 // an uploaded file (#475)
)

// File error codes: FileResult.Code, which the server maps to a status.
const (
	FileRefused  = "refused"   // the path is not a relative path inside the session folder
	FileNotFound = "not-found" // nothing there, or no such record on the node
	FileNotFile  = "not-file"  // a read of something that is not a regular file
	FileNotDir   = "not-dir"   // a listing of something that is not a directory
	FileFailed   = "failed"    // any other error
	FileConflict = "conflict"  // a write whose expected fingerprint is not the file's (#448)
	FileTooBig   = "too-big"   // a write of more than FileTextMax (#448)
	FileExists   = "exists"    // a rename or an upload onto a name already there
	FileNotEmpty = "not-empty" // a delete of a folder that has entries
)

// File entry kinds.
const (
	FileKindFile  = "file"
	FileKindDir   = "dir"
	FileKindLink  = "link" // a symbolic link, not followed in a listing
	FileKindOther = "other"
)

// NodeFile asks for Op on Path, relative to the record's directory with /
// separators ("" is the directory itself). A read takes Length bytes (at
// most FileChunkMax; 0 means FileChunkMax) from Offset. A write replaces
// the regular file at Path with the Length bytes of the FileData frames
// before it; with Expect, only while the file still has that fingerprint
// (else FileConflict), without it whatever the file is now.
type NodeFile struct {
	Ref    string           `json:"ref"`
	Op     string           `json:"op"`
	Path   string           `json:"path"`
	Offset int64            `json:"offset,omitempty"`
	Length int64            `json:"length,omitempty"`
	Expect *FileFingerprint `json:"expect,omitempty"`
	// To is a rename's new name: one segment, in Path's folder.
	To string `json:"to,omitempty"`
	// Replace lets an upload write over a regular file of that name.
	Replace bool `json:"replace,omitempty"`
}

// FileEntry is one directory entry, or the stat of a path.
type FileEntry struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // Unix ms
	// SHA256 is the content's hash, hex: set on the Stat of a write only.
	SHA256 string `json:"sha256,omitempty"`
}

// FileFingerprint is what a file was when the dashboard read it (#448): its
// size, its modification time (Unix ms) and the SHA-256 of its content. A
// save sends it back; the node writes only while the file still matches.
type FileFingerprint struct {
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	SHA256 string `json:"sha256"`
}

// String is the fingerprint as the config.FileFingerprintHeaderName header
// carries it: size:mtime:sha256.
func (f FileFingerprint) String() string {
	return strconv.FormatInt(f.Size, 10) + ":" + strconv.FormatInt(f.MTime, 10) + ":" + f.SHA256
}

// ParseFileFingerprint reads String's form; false when s is not one.
func ParseFileFingerprint(s string) (FileFingerprint, bool) {
	a, rest, ok1 := strings.Cut(s, ":")
	b, h, ok2 := strings.Cut(rest, ":")
	size, err1 := strconv.ParseInt(a, 10, 64)
	mtime, err2 := strconv.ParseInt(b, 10, 64)
	if !ok1 || !ok2 || err1 != nil || err2 != nil || size < 0 || len(h) != 64 {
		return FileFingerprint{}, false
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return FileFingerprint{}, false
		}
	}
	return FileFingerprint{Size: size, MTime: mtime, SHA256: h}, true
}

// FileSave is the body of PUT …/terminals/{id}/files/raw?path= (#448): the
// file's new text, at most FileTextMax bytes. The expected fingerprint is
// the config.FileFingerprintHeaderName header, or "*" to save whatever the
// file is now.
type FileSave struct {
	Text string `json:"text"`
}

// FileSaved answers a save: the path, the file as written and its new
// fingerprint, which the next save of the same text sends.
type FileSaved struct {
	Path        string    `json:"path"`
	Stat        FileEntry `json:"stat"`
	Fingerprint string    `json:"fingerprint"`
}

// NodeFileResult answers a NodeFile: Code and Error when it failed; else
// Stat (every op), Entries and Truncated (list) and, for a read, Length:
// how many bytes the FileData frame before it carried.
type NodeFileResult struct {
	Ref       string      `json:"ref"`
	Code      string      `json:"code,omitempty"`
	Error     string      `json:"error,omitempty"`
	Stat      *FileEntry  `json:"stat,omitempty"`
	Entries   []FileEntry `json:"entries,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	Length    int64       `json:"length,omitempty"`
}

// FileListing is the server's answer to a listing or a stat (GET
// …/terminals/{id}/files): the path asked, what it is and, for a
// directory, its entries.
type FileListing struct {
	Path      string      `json:"path"`
	Stat      FileEntry   `json:"stat"`
	Entries   []FileEntry `json:"entries,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}

// FileChange is the body of POST …/terminals/{id}/files?path= (#474, #475):
// Op FileRename gives path the name To; Op FileUpload puts Data (base64 in
// JSON, at most FileUploadMax bytes) in the folder path under Name, over a
// regular file of that name only with Replace.
type FileChange struct {
	Op      string `json:"op"`
	To      string `json:"to,omitempty"`
	Name    string `json:"name,omitempty"`
	Data    []byte `json:"data,omitempty"`
	Replace bool   `json:"replace,omitempty"`
}

// FileChanged answers a delete, a rename or an upload: the path it left
// (a rename's and an upload's new path, a delete's old one) and, but for a
// delete, what is there now.
type FileChanged struct {
	Path string     `json:"path"`
	Stat *FileEntry `json:"stat,omitempty"`
}
