package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A stop file still held open by its writer cannot be removed on Windows;
// the watch waits for the handle to close, removes it, and only then
// reports it seen (#492).
func TestStopFileHeldOpenIsSeenOnceItCanBeRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), upStopFile)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := watchStopFile(ctx, path, 10*time.Millisecond)
	select {
	case <-seen:
		t.Fatal("seen while the file could not be removed")
	case <-time.After(100 * time.Millisecond):
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop file did not stop it once closed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stop file left after it was seen: %v", err)
	}
}
