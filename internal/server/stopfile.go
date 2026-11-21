package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"
)

// upStopFile is the file in up's data directory that asks a running up to
// stop the way SIGINT or SIGTERM does (#388): the node stops the sessions
// whose records are gone before up exits (runLocalNode). On Windows it is
// the only graceful way: up runs from a scheduled task with no console, so
// no console event reaches it, and Stop-Process ends it at once. sbstop
// (docs/hosting-pc.md) writes it; up removes it when it sees it.
const upStopFile = "up.stop"

// stopFilePoll is how often up looks for its stop file.
const stopFilePoll = 500 * time.Millisecond

// clearStopFile removes a stop file left from before this start, which
// would otherwise stop up at once.
func clearStopFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("up: stop file left from before: %w", err)
	}
	return nil
}

// watchStopFile returns a channel closed once path exists, after removing
// it. It looks every interval until ctx ends; the channel then stays open.
func watchStopFile(ctx context.Context, path string, interval time.Duration) <-chan struct{} {
	seen := make(chan struct{})
	goSafe("stop file", func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			if _, err := os.Stat(path); err == nil {
				if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					log.Printf("stop file: %v", err)
				}
				close(seen)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})
	return seen
}
