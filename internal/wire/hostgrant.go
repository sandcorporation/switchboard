package wire

import (
	"path"
	"regexp"
	"strings"
)

// HostGrant is what a role may host under itself with its own token (#369):
// sessions of the listed base roles, on the listed nodes, in a directory
// under one of DirRoots. The admin sets it (host grant) and removes it (host
// revoke-grant); a role has at most one.
type HostGrant struct {
	Role     string   `json:"role"`
	Bases    []string `json:"bases"`
	Nodes    []string `json:"nodes"`
	DirRoots []string `json:"dir_roots"`
	// Adapters are the agent adapters the role may choose (#434); empty
	// allows every one.
	Adapters []string `json:"adapters,omitempty"`
	// Groups are the work groups the role may choose (#435); empty allows
	// only sessions without a group.
	Groups    []string `json:"groups,omitempty"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

// Hosted session event kinds of the record itself (#369): a role added or
// removed it with its own token. The event's Actor is that role.
const (
	HostedEventAdd    = "add"
	HostedEventRemove = "remove"
)

var winDrivePattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// CleanDir is an absolute directory of either kind of node in one spelling,
// with "." and ".." resolved lexically (never above the root or volume): /…
// for Linux; C:\… or \\server\share\… for Windows, whose separators become
// \. Key is the spelling to compare: Windows paths are case-insensitive, so
// Key is lower case for them. ok is false for anything else.
func CleanDir(dir string) (clean, key string, ok bool) {
	switch {
	case strings.HasPrefix(dir, "/") && !strings.HasPrefix(dir, "//"):
		c := path.Clean(dir)
		return c, c, true
	case winDrivePattern.MatchString(dir):
		vol, rest := dir[:2], strings.ReplaceAll(dir[2:], `\`, "/")
		c := strings.ToUpper(vol[:1]) + vol[1:] + strings.ReplaceAll(path.Clean(rest), "/", `\`)
		return c, strings.ToLower(c), true
	case strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, "//"):
		parts := strings.SplitN(strings.TrimLeft(strings.ReplaceAll(dir, "/", `\`), `\`), `\`, 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
			return "", "", false
		}
		rest := "/"
		if len(parts) == 3 {
			rest = path.Clean("/" + strings.ReplaceAll(parts[2], `\`, "/"))
		}
		c := `\\` + parts[0] + `\` + parts[1]
		if rest != "/" {
			c += strings.ReplaceAll(rest, "/", `\`)
		}
		return c, strings.ToLower(c), true
	}
	return "", "", false
}

// DirWithin reports whether dir is root or under it, after CleanDir on both:
// "/work/app" is within "/work", "/workshop" and "/work/../etc" are not, and
// on Windows "c:/Work/App" is within `C:\work`. It compares spellings only;
// a symbolic link is the node's to resolve (node.checkDirRoot).
func DirWithin(dir, root string) bool {
	_, d, ok1 := CleanDir(dir)
	_, r, ok2 := CleanDir(root)
	if !ok1 || !ok2 {
		return false
	}
	if d == r {
		return true
	}
	sep := "/"
	if !strings.HasPrefix(r, "/") {
		sep = `\`
	}
	if !strings.HasSuffix(r, sep) {
		r += sep
	}
	return strings.HasPrefix(d, r)
}
