package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStopFileStopsOnceItAppearsAndIsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), upStopFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := watchStopFile(ctx, path, 10*time.Millisecond)
	select {
	case <-seen:
		t.Fatal("stopped with no stop file")
	case <-time.After(50 * time.Millisecond):
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop file did not stop it")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stop file left after it was seen: %v", err)
	}
}

func TestStopFileWatchEndsWithItsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), upStopFile)
	ctx, cancel := context.WithCancel(context.Background())
	seen := watchStopFile(ctx, path, 10*time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
		t.Fatal("stopped after its context ended")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStopFileLeftFromBeforeIsCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), upStopFile)
	if err := clearStopFile(path); err != nil {
		t.Fatalf("no stop file: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearStopFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stop file left: %v", err)
	}
}
