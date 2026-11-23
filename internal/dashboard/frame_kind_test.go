package dashboard

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The session card's first-line tag is the session kind (#517): the role
// name without its number tail. The function runs under node; skipped
// without node unless SWITCHBOARD_TEST_NODE=require.
func TestFrameKind(t *testing.T) {
	src, _ := pageFS.ReadFile("page/frame.js")
	// The kind leads line one (hover: the whole address); the CLI moves to
	// line two beside the machine.
	for _, want := range []string{
		"const kind=node('span',frameKind(r.role),'tag');kind.title=r.role;l1.append(kind);",
		"if(r.machine)l2.append(node('span',r.machine,'tag'));",
		"if(r.cli&&r.cli!=='unknown'){const c=node('span',r.cli.split(/\\s/)[0],'tag');c.title=r.cli;l2.append(c)}",
	} {
		if !strings.Contains(string(src), want) {
			t.Fatalf("frame.js missing %q", want)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("SWITCHBOARD_TEST_NODE") == "require" {
			t.Fatal("node not found and SWITCHBOARD_TEST_NODE=require")
		}
		t.Skip("node not installed")
	}
	// No $: a CRLF checkout (Windows) leaves \r before the line end.
	fn := regexp.MustCompile(`(?m)^function frameKind\(role\)\{.*\}`).Find(src)
	if fn == nil {
		t.Fatal("frame.js has no one-line frameKind")
	}
	js := string(fn) + `
const a=require('node:assert');
a.strictEqual(frameKind('team/boss-1'),'boss');
a.strictEqual(frameKind('team/worker-1-2'),'worker');
a.strictEqual(frameKind('team/coordinator'),'coordinator');
a.strictEqual(frameKind('solo'),'solo');
a.strictEqual(frameKind('team/build-2fa'),'build-2fa');
`
	if out, err := exec.Command(node, "-e", js).CombinedOutput(); err != nil {
		t.Fatalf("frameKind: %v\n%s", err, out)
	}
}
