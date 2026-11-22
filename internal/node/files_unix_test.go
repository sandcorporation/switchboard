//go:build unix

package node

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// A FIFO in the folder is refused at once, not waited on.
func TestFolderFileRefusesFIFO(t *testing.T) {
	dir, _ := folder(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan wire.NodeFileResult, 1)
	go func() {
		res, _ := FolderFile(dir, wire.NodeFile{Op: wire.FileRead, Path: "pipe"})
		done <- res
	}()
	select {
	case res := <-done:
		if res.Code != wire.FileNotFile {
			t.Fatalf("read of a FIFO = %+v, want not-file", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read of a FIFO blocked")
	}
}
