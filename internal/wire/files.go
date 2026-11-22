package wire

// Session folder files (#447): the host asks a node for a listing, a stat
// or a piece of a file under a hosted record's directory with a file frame,
// and the node answers with one file-result frame of the same Ref; a read's
// bytes come first, in one binary frame of kind FileData whose id is the
// Ref. The node enforces the boundary (internal/node/files.go); a node
// sends file frames only to a host whose node advertised
// config.NodeFilesHeaderName, and both sides ignore frame types and binary
// kinds they do not know.
const (
	NodeFileType       = "file"        // host to node: File, ID is the record
	NodeFileResultType = "file-result" // node to host: FileResult
)

// FileData is the binary frame kind carrying a read's bytes, node to host:
// EncodeTermFrame(FileData, ref, bytes).
const FileData byte = 'f'

// File operations.
const (
	FileList = "list" // the entries of a directory, at most FileListMax
	FileStat = "stat" // what the path is
	FileRead = "read" // Length bytes from Offset, at most FileChunkMax
)

// Limits of the file path (#443 §4).
const (
	FileListMax  = 1000    // entries in one listing
	FileChunkMax = 1 << 20 // bytes in one read
	FileTextMax  = 2 << 20 // a text file the dashboard opens whole
)

// File error codes: FileResult.Code, which the server maps to a status.
const (
	FileRefused  = "refused"   // the path is not a relative path inside the session folder
	FileNotFound = "not-found" // nothing there, or no such record on the node
	FileNotFile  = "not-file"  // a read of something that is not a regular file
	FileNotDir   = "not-dir"   // a listing of something that is not a directory
	FileFailed   = "failed"    // any other error
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
// most FileChunkMax; 0 means FileChunkMax) from Offset.
type NodeFile struct {
	Ref    string `json:"ref"`
	Op     string `json:"op"`
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Length int64  `json:"length,omitempty"`
}

// FileEntry is one directory entry, or the stat of a path.
type FileEntry struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // Unix ms
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
