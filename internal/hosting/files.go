package hosting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Session folder files, host side (#447): the server asks the record's node
// with a file frame and waits for its file-result frame (and, for a read,
// the FileData frame before it). The node enforces the folder boundary;
// the checks here only refuse early. Neither paths' content nor bytes are
// logged or recorded.

// fileWait is how long the host waits for one file answer.
const fileWait = 30 * time.Second

// Host-side file error codes, besides the node's wire.File* codes.
const (
	fileUnsupported = "unsupported" // the node did not advertise files
	fileOffline     = "offline"     // the record's node is not attached
	fileTimeout     = "timeout"     // the node did not answer in time
	fileBadRequest  = "bad-request" // refused before asking the node
	fileReadOnly    = "read-only"   // a save on a node that does not write (#448)
	fileOldNode     = "old-node"    // a delete, rename or upload on a node without them (#473–#475)
)

// pendingFile is a file frame waiting for its answer from conn.
type pendingFile struct {
	conn *nodeConn
	ch   chan fileAnswer
	data []byte
}

type fileAnswer struct {
	res  wire.NodeFileResult
	data []byte
}

// fileErr is a failed file request: its code (wire.File* or the host's)
// and message.
type fileErr struct{ code, msg string }

func (e *fileErr) Error() string { return e.msg }

// askFile sends a file frame for record id to its node and waits for the
// answer: the result and a read's bytes, or a *fileErr.
func (h *Hub) askFile(id string, req wire.NodeFile) (wire.NodeFileResult, []byte, error) {
	return h.askFileData(id, req, nil)
}

// askFileData is askFile; a write (#448) sends data first, in FileData
// frames of at most wire.FileChunkMax, and only to a node that writes.
func (h *Hub) askFileData(id string, req wire.NodeFile, data []byte) (wire.NodeFileResult, []byte, error) {
	rec, err := h.svc.Get(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return wire.NodeFileResult{}, nil, &fileErr{wire.FileNotFound, "no such hosted session"}
		}
		return wire.NodeFileResult{}, nil, &fileErr{wire.FileFailed, "query failed"}
	}
	h.mu.Lock()
	c, ok := h.conns[rec.Node]
	switch {
	case !ok:
		h.mu.Unlock()
		return wire.NodeFileResult{}, nil, &fileErr{fileOffline, "node " + rec.Node + " is offline"}
	case !c.files:
		h.mu.Unlock()
		return wire.NodeFileResult{}, nil, &fileErr{fileUnsupported, "node " + rec.Node + " does not support files"}
	case req.Op == wire.FileWrite && !c.write:
		h.mu.Unlock()
		return wire.NodeFileResult{}, nil, &fileErr{fileReadOnly, "node " + rec.Node + " does not save files"}
	case (req.Op == wire.FileDelete || req.Op == wire.FileRename || req.Op == wire.FileUpload) && !c.ops:
		h.mu.Unlock()
		return wire.NodeFileResult{}, nil, &fileErr{fileOldNode, "node " + rec.Node + " does not delete, rename or take uploads; update it"}
	}
	if h.files == nil {
		h.files = map[string]*pendingFile{}
	}
	h.fileSeq++
	req.Ref = "f" + strconv.FormatInt(h.fileSeq, 10)
	p := &pendingFile{conn: c, ch: make(chan fileAnswer, 1)}
	h.files[req.Ref] = p
	sent := true
	if req.Op == wire.FileWrite || req.Op == wire.FileUpload {
		req.Length = int64(len(data))
		for off := 0; sent && off < len(data); off += wire.FileChunkMax {
			end := min(off+wire.FileChunkMax, len(data))
			sent = h.sendBinLocked(c, wire.EncodeTermFrame(wire.FileData, req.Ref, data[off:end]))
		}
	}
	sent = sent && h.sendLocked(c, wire.NodeFrame{Type: wire.NodeFileType, ID: id, File: &req})
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.files, req.Ref)
		h.mu.Unlock()
	}()
	if !sent {
		return wire.NodeFileResult{}, nil, &fileErr{fileOffline, "node " + rec.Node + ": send failed"}
	}
	t := time.NewTimer(fileWait)
	defer t.Stop()
	select {
	case a := <-p.ch:
		if a.res.Code != "" {
			return a.res, nil, &fileErr{a.res.Code, a.res.Error}
		}
		if req.Op == wire.FileRead && int64(len(a.data)) != a.res.Length {
			return a.res, nil, &fileErr{wire.FileFailed, "the node's read was cut short"}
		}
		if a.res.Stat == nil {
			return a.res, nil, &fileErr{wire.FileFailed, "the node's answer has no stat"}
		}
		return a.res, a.data, nil
	case <-t.C:
		return wire.NodeFileResult{}, nil, &fileErr{fileTimeout, fmt.Sprintf("node %s did not answer within %s", rec.Node, fileWait)}
	}
}

// sendBinLocked queues binary frame b for c, as sendLocked does a frame.
func (h *Hub) sendBinLocked(c *nodeConn, b []byte) bool {
	select {
	case c.out <- nodeOut{bin: b}:
		return true
	default:
		c.cancel()
		return false
	}
}

// fileData keeps the bytes of a FileData frame from c for its pending
// read; only one frame per read, at most a chunk.
func (h *Hub) fileData(c *nodeConn, ref string, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.files[ref]; ok && p.conn == c && p.data == nil && len(b) <= wire.FileChunkMax {
		p.data = append([]byte{}, b...)
	}
}

// fileAnswered hands a file-result frame from c to its waiter.
func (h *Hub) fileAnswered(c *nodeConn, res wire.NodeFileResult) {
	h.mu.Lock()
	p, ok := h.files[res.Ref]
	if ok && p.conn == c {
		delete(h.files, res.Ref)
	}
	h.mu.Unlock()
	if ok && p.conn == c {
		p.ch <- fileAnswer{res: res, data: p.data}
	}
}

// dropFilesLocked ends the waits on c, which is gone.
func (h *Hub) dropFilesLocked(c *nodeConn) {
	for ref, p := range h.files {
		if p.conn == c {
			delete(h.files, ref)
			p.ch <- fileAnswer{res: wire.NodeFileResult{Ref: ref, Code: fileOffline, Error: "node " + c.name + " left"}}
		}
	}
}

// fileStatus is the HTTP status of a file error code.
func fileStatus(code string) int {
	switch code {
	case wire.FileRefused:
		return http.StatusForbidden
	case wire.FileNotFound:
		return http.StatusNotFound
	case wire.FileNotFile, wire.FileNotDir, fileBadRequest:
		return http.StatusBadRequest
	case wire.FileConflict, wire.FileExists, wire.FileNotEmpty:
		return http.StatusConflict
	case wire.FileTooBig:
		return http.StatusRequestEntityTooLarge
	case fileUnsupported, fileReadOnly, fileOldNode:
		return http.StatusNotImplemented
	case fileOffline:
		return http.StatusServiceUnavailable
	case fileTimeout:
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

// fileError is the JSON body of a failed file request.
type fileError struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func writeFileErr(w http.ResponseWriter, err error) {
	fe, ok := err.(*fileErr)
	if !ok {
		fe = &fileErr{wire.FileFailed, err.Error()}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(fileStatus(fe.code))
	_ = json.NewEncoder(w).Encode(fileError{Code: fe.code, Error: fe.msg})
}

// checkFilePath refuses early what no node would answer: NUL, an absolute
// path, a drive letter or ':' and a ".." segment. The node checks again,
// and more (internal/node/files.go).
func checkFilePath(p string) error {
	bad := strings.ContainsRune(p, 0) || len(p) > 4096 || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.ContainsRune(p, ':')
	for _, s := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		bad = bad || s == ".."
	}
	if bad {
		return &fileErr{fileBadRequest, "path must be relative to the session folder"}
	}
	return nil
}

// fileRequest checks the method, the admin's sign-in and the path of a
// files request; false when it answered.
func (h *Hub) fileRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}
	return h.filePath(w, r)
}

// filePath checks the admin's sign-in and the path of a files request of
// any method; false when it answered.
func (h *Hub) filePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	a := h.adminHooks()
	if a.Session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if _, _, ok := a.Session(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	p := r.URL.Query().Get("path")
	if err := checkFilePath(p); err != nil {
		writeFileErr(w, err)
		return "", false
	}
	return p, true
}

// filesAPI serves GET /api/admin/terminals/{id}/files?path=: what the path
// is and, for a directory, its entries (at most wire.FileListMax).
func (h *Hub) filesAPI(w http.ResponseWriter, r *http.Request, id string) {
	p, ok := h.fileRequest(w, r)
	if !ok {
		return
	}
	res, _, err := h.askFile(id, wire.NodeFile{Op: wire.FileStat, Path: p})
	if err == nil && res.Stat.Kind == wire.FileKindDir {
		res, _, err = h.askFile(id, wire.NodeFile{Op: wire.FileList, Path: p})
	}
	if err != nil {
		writeFileErr(w, err)
		return
	}
	out := wire.FileListing{Path: p, Stat: *res.Stat, Entries: res.Entries, Truncated: res.Truncated}
	if out.Entries == nil && out.Stat.Kind == wire.FileKindDir {
		out.Entries = []wire.FileEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// fileTypes is the allow-list of content types by extension; anything else
// is application/octet-stream. HTML, scripts and the like are text/plain:
// a file is shown, never run.
var fileTypes = func() map[string]string {
	m := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".m4a": "audio/mp4", ".flac": "audio/flac",
		".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".ogv": "video/ogg",
	}
	for _, e := range strings.Fields(".txt .md .markdown .go .mod .sum .js .mjs .cjs .ts .tsx .jsx .py .rb .rs .java .kt .c .h .cc .cpp .hpp .cs .sh .bash .zsh .ps1 .bat .cmd " +
		".json .jsonl .yaml .yml .toml .ini .cfg .conf .env .xml .html .htm .css .scss .csv .tsv .log .sql .diff .patch .gitignore .dockerignore .lock .vue .svelte") {
		m[e] = "text/plain; charset=utf-8"
	}
	return m
}()

// FileContentType is the content type of a file content response for name.
func FileContentType(name string) string {
	if t, ok := fileTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// parseRange reads a single-range Range header against size: the first and
// last byte. ok is false without one (serve it all, 200); satisfiable is
// false for a range past the end (416). A multi-range is served whole.
func parseRange(h string, size int64) (first, last int64, ok, satisfiable bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(h), "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false, true
	}
	a, b, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false, true
	}
	if a == "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, false, true
		}
		if n == 0 || size == 0 {
			return 0, 0, true, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, true
	}
	first, err := strconv.ParseInt(a, 10, 64)
	if err != nil || first < 0 {
		return 0, 0, false, true
	}
	last = size - 1
	if b != "" {
		l, err := strconv.ParseInt(b, 10, 64)
		if err != nil || l < first {
			return 0, 0, false, true
		}
		if l < last {
			last = l
		}
	}
	if first >= size {
		return 0, 0, true, false
	}
	return first, last, true, true
}

// rawAPI serves GET /api/admin/terminals/{id}/files/raw?path=: the file's
// bytes, read from the node a chunk (wire.FileChunkMax) at a time, whole
// (200) or one Range (206). Every answer is sandboxed and never sniffed.
func (h *Hub) rawAPI(w http.ResponseWriter, r *http.Request, id string) {
	p, ok := h.fileRequest(w, r)
	if !ok {
		return
	}
	st, _, err := h.askFile(id, wire.NodeFile{Op: wire.FileStat, Path: p})
	if err == nil && st.Stat.Kind != wire.FileKindFile {
		err = &fileErr{wire.FileNotFile, "not a regular file"}
	}
	if err != nil {
		writeFileErr(w, err)
		return
	}
	size := st.Stat.Size
	hd := w.Header()
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Cache-Control", "no-store")
	hd.Set("Accept-Ranges", "bytes")
	first, last := int64(0), size-1
	code := http.StatusOK
	if rg := r.Header.Get("Range"); rg != "" {
		f, l, ranged, satisfiable := parseRange(rg, size)
		if !satisfiable {
			hd.Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
			http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if ranged {
			first, last, code = f, l, http.StatusPartialContent
			hd.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, size))
		}
	}
	hd.Set("Content-Type", FileContentType(p))
	if code == http.StatusOK && r.Method == http.MethodGet && size <= wire.FileTextMax {
		// A whole text-sized read (#448): read it all first, so the answer
		// carries the fingerprint a save sends back. Bytes that changed
		// while being read give a fingerprint no file has: that save is a
		// conflict.
		var body []byte
		for int64(len(body)) < size {
			_, data, err := h.askFile(id, wire.NodeFile{Op: wire.FileRead, Path: p, Offset: int64(len(body)), Length: size - int64(len(body))})
			if err != nil {
				writeFileErr(w, err)
				return
			}
			if len(data) == 0 {
				break
			}
			body = append(body, data...)
		}
		sum := sha256.Sum256(body)
		fp := wire.FileFingerprint{Size: size, MTime: st.Stat.MTime, SHA256: hex.EncodeToString(sum[:])}
		hd.Set(config.HeaderPrefix+config.FileFingerprintHeaderName, fp.String())
		hd.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(code)
		_, _ = w.Write(body)
		return
	}
	hd.Set("Content-Length", strconv.FormatInt(last-first+1, 10))
	w.WriteHeader(code)
	if r.Method == http.MethodHead {
		return
	}
	for off := first; off <= last; {
		n := last - off + 1
		if n > wire.FileChunkMax {
			n = wire.FileChunkMax
		}
		_, data, err := h.askFile(id, wire.NodeFile{Op: wire.FileRead, Path: p, Offset: off, Length: n})
		if err != nil || len(data) == 0 {
			// The status is out: a short body ends the response.
			return
		}
		if _, err := w.Write(data); err != nil {
			return
		}
		off += int64(len(data))
	}
}

// saveAPI serves PUT /api/admin/terminals/{id}/files/raw?path= (#448): the
// body (wire.FileSave) replaces the regular file at path, if it still has
// the fingerprint of the config.FileFingerprintHeaderName header ("*":
// whatever it has). A changed file is 409, more than wire.FileTextMax 413.
// The server's admin guard has already checked the write (JSON, the
// dashboard header, same site). The save is recorded by path and size,
// never its content.
func (h *Hub) saveAPI(w http.ResponseWriter, r *http.Request, id string) {
	p, ok := h.filePath(w, r)
	if !ok {
		return
	}
	var expect *wire.FileFingerprint
	switch fp := r.Header.Get(config.HeaderPrefix + config.FileFingerprintHeaderName); fp {
	case "*":
	case "":
		writeFileErr(w, &fileErr{fileBadRequest, "a save needs the fingerprint the file was read with, or *"})
		return
	default:
		f, ok := wire.ParseFileFingerprint(fp)
		if !ok {
			writeFileErr(w, &fileErr{fileBadRequest, "bad fingerprint"})
			return
		}
		expect = &f
	}
	if p == "" {
		writeFileErr(w, &fileErr{wire.FileNotFile, "the session folder is not a file"})
		return
	}
	// JSON escapes a control byte in six: room for that, no more.
	var body wire.FileSave
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 6*wire.FileTextMax+64)).Decode(&body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeFileErr(w, &fileErr{wire.FileTooBig, "more than 2 MiB"})
			return
		}
		writeFileErr(w, &fileErr{fileBadRequest, "bad body"})
		return
	}
	if len(body.Text) > wire.FileTextMax {
		writeFileErr(w, &fileErr{wire.FileTooBig, "more than 2 MiB"})
		return
	}
	res, _, err := h.askFileData(id, wire.NodeFile{Op: wire.FileWrite, Path: p, Expect: expect}, []byte(body.Text))
	if err != nil {
		writeFileErr(w, err)
		return
	}
	if res.Stat.SHA256 == "" {
		writeFileErr(w, &fileErr{wire.FileFailed, "the node's answer has no hash"})
		return
	}
	h.fileSaved(r, id, p, res.Stat.Size)
	fp := wire.FileFingerprint{Size: res.Stat.Size, MTime: res.Stat.MTime, SHA256: res.Stat.SHA256}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(wire.FileSaved{Path: p, Stat: *res.Stat, Fingerprint: fp.String()})
}

// fileSaved records a save as an admin action: the session, the path and
// the size, never the content.
func (h *Hub) fileSaved(r *http.Request, id, p string, size int64) {
	h.svc.recordActionDetail(r, wire.ActionFileSave, id, p+" · "+strconv.FormatInt(size, 10)+" B")
}

// changeAPI serves the files pane's changes on
// /api/admin/terminals/{id}/files?path=: DELETE removes the entry at path
// (a file, a symbolic link, an empty folder: #473); POST with
// wire.FileChange renames it (Op rename, To a name in the same folder:
// #474) or puts an uploaded file in the folder path (Op upload, Name, Data
// at most wire.FileUploadMax bytes, Replace to write over a regular file of
// that name: #475). A name already there or a folder with entries is 409.
// The server's admin guard has checked the write and the files permission.
// Each is recorded by path, never content.
func (h *Hub) changeAPI(w http.ResponseWriter, r *http.Request, id string) {
	p, ok := h.filePath(w, r)
	if !ok {
		return
	}
	if p == "" && r.Method == http.MethodDelete {
		writeFileErr(w, &fileErr{wire.FileRefused, "the session folder itself"})
		return
	}
	var req wire.NodeFile
	var data []byte
	var action, detail string
	if r.Method == http.MethodDelete {
		req, action, detail = wire.NodeFile{Op: wire.FileDelete, Path: p}, wire.ActionFileDelete, p
	} else {
		// base64 is 4 bytes for 3; room for that and the JSON around it.
		var c wire.FileChange
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, wire.FileUploadMax/3*4+8192)).Decode(&c); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeFileErr(w, &fileErr{wire.FileTooBig, "more than 4 MiB"})
				return
			}
			writeFileErr(w, &fileErr{fileBadRequest, "bad body"})
			return
		}
		switch c.Op {
		case wire.FileRename:
			if p == "" {
				writeFileErr(w, &fileErr{wire.FileRefused, "the session folder itself"})
				return
			}
			if err := checkFileName(c.To); err != nil {
				writeFileErr(w, err)
				return
			}
			to := path.Join(path.Dir(p), c.To)
			req, action, detail = wire.NodeFile{Op: wire.FileRename, Path: p, To: c.To}, wire.ActionFileRename, p+" → "+to
		case wire.FileUpload:
			if err := checkFileName(c.Name); err != nil {
				writeFileErr(w, err)
				return
			}
			if len(c.Data) > wire.FileUploadMax {
				writeFileErr(w, &fileErr{wire.FileTooBig, "more than 4 MiB"})
				return
			}
			to := c.Name
			if p != "" {
				to = p + "/" + c.Name
			}
			data = c.Data
			req, action, detail = wire.NodeFile{Op: wire.FileUpload, Path: to, Replace: c.Replace}, wire.ActionFileUpload, to+" · "+strconv.Itoa(len(data))+" B"
		default:
			writeFileErr(w, &fileErr{fileBadRequest, `op must be "rename" or "upload"`})
			return
		}
	}
	res, _, err := h.askFileData(id, req, data)
	if err != nil {
		writeFileErr(w, err)
		return
	}
	h.svc.recordActionDetail(r, action, id, detail)
	out := wire.FileChanged{Path: req.Path}
	if req.Op != wire.FileDelete {
		st := *res.Stat
		out.Stat = &st
		if req.Op == wire.FileRename {
			out.Path = path.Join(path.Dir(p), req.To)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// checkFileName refuses early a new name that is not one plain segment.
func checkFileName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") || len(name) > 255 {
		return &fileErr{fileBadRequest, "a name is one segment, without / \\ :"}
	}
	return nil
}
