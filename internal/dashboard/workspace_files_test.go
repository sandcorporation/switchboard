package dashboard

import (
	"os"
	"os/exec"
	"testing"
)

// TestWorkspaceFiles runs workspace/files.test.cjs (#447) under node,
// skipped without node unless SWITCHBOARD_TEST_NODE=require.
func TestWorkspaceFiles(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("SWITCHBOARD_TEST_NODE") == "require" {
			t.Fatal("node not found and SWITCHBOARD_TEST_NODE=require")
		}
		t.Skip("node not installed")
	}
	cmd := exec.Command(node, "--test", "files.test.cjs")
	cmd.Dir = "workspace"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test workspace/files.test.cjs: %v\n%s", err, out)
	}
}
