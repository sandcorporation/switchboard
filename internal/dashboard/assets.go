package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// vendored holds third-party files served by the binary, never from a CDN:
// xterm.js and its fit addon for the terminal view (assets/xterm/VERSIONS
// pins them; the licences sit beside them), CodeMirror for the file editor
// (assets/codemirror, #448) and the design system stylesheet
// (assets/sandcorp.css, inlined into the page by DesignCSS).
//
//go:embed assets
var vendored embed.FS

// AssetsPrefix is the URL path the vendored files are served under.
const AssetsPrefix = "/assets/"

// TerminalAssets are the terminal view's files, loaded by the page only when
// the admin bootstrap reports hosting on: stylesheets, then scripts in order.
var TerminalAssets = []string{
	AssetsPrefix + "xterm/xterm.css",
	AssetsPrefix + "xterm/xterm.js",
	AssetsPrefix + "xterm/addon-fit.js",
}

// EditorAssets are the file editor's files (#448): CodeMirror 5 and its
// modes for Go, JavaScript (and JSON), Python, Markdown, YAML and the shell
// (assets/codemirror/VERSIONS pins them). The bootstrap lists them with
// hosting on; the workspace loads them when it first opens a file.
var EditorAssets = []string{
	AssetsPrefix + "codemirror/codemirror.css",
	AssetsPrefix + "codemirror/codemirror.js",
	AssetsPrefix + "codemirror/mode-go.js",
	AssetsPrefix + "codemirror/mode-javascript.js",
	AssetsPrefix + "codemirror/mode-python.js",
	AssetsPrefix + "codemirror/mode-markdown.js",
	AssetsPrefix + "codemirror/mode-yaml.js",
	AssetsPrefix + "codemirror/mode-shell.js",
}

// workspace holds the host-mode workspace (#444): the panel layout, the
// right column (#445), the terminal panels, the 노드 index (#446), the frame
// and its stylesheet. The node unit tests (*.test.cjs) are not embedded.
//
//go:embed workspace/layout.js workspace/side.js workspace/files.js workspace/nodes.js workspace/term.js workspace/nodeview.js workspace/workspace.js workspace/workspace.css
var workspace embed.FS

// WorkspaceAssets are the workspace's files, loaded by the page only when the
// admin bootstrap reports hosting on: the stylesheet, then scripts in order.
var WorkspaceAssets = []string{
	AssetsPrefix + "workspace/workspace.css",
	AssetsPrefix + "workspace/layout.js",
	AssetsPrefix + "workspace/side.js",
	AssetsPrefix + "workspace/files.js",
	AssetsPrefix + "workspace/nodes.js",
	AssetsPrefix + "workspace/term.js",
	AssetsPrefix + "workspace/nodeview.js",
	AssetsPrefix + "workspace/workspace.js",
}

var assetTypes = map[string]string{
	".js":  "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8",
}

// Assets serves one vendored file (GET or HEAD). Directories are not listed.
func Assets(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutPrefix(r.URL.Path, AssetsPrefix)
	if !ok || name == "" || path.Clean(name) != name {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b []byte
	var err error
	if strings.HasPrefix(name, "workspace/") {
		b, err = fs.ReadFile(workspace, name)
	} else {
		b, err = fs.ReadFile(vendored, "assets/"+name)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	typ, ok := assetTypes[path.Ext(name)]
	if !ok {
		typ = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b)
}

// DesignCSS is the design system stylesheet (assets/sandcorp.css, copied from
// sandcorporation/sandcorp-design by scripts/sync-design-css.sh). The page
// inlines it, so it reaches the browser with hosting off as well.
func DesignCSS() string {
	b, err := fs.ReadFile(vendored, "assets/sandcorp.css")
	if err != nil {
		panic("dashboard: embedded sandcorp.css missing: " + err.Error())
	}
	return string(b)
}
