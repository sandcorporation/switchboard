//go:build windows

package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/term/conpty"
	"github.com/sandcorporation/switchboard/internal/winjob"
)

// e2eBackendFor is the ConPTY keeper up starts; the cleanup asks it to shut
// down, which ends every child it holds.
func e2eBackendFor(t *testing.T) e2eBackend {
	t.Helper()
	if err := conpty.CheckWindows(); err != nil {
		e2eSkip(t, "ConPTY unavailable: %v", err)
	}
	return e2eBackend{
		// No console signal reaches a process without one: the stop
		// file, as sbstop writes it (#388).
		stop: func(_ *exec.Cmd, dir string) {
			if err := os.WriteFile(filepath.Join(dir, upStopFile), nil, 0o600); err != nil {
				t.Logf("stop file: %v", err)
			}
		},
		alive: winjob.Alive,
		end: func(instance string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			c := conpty.NewClient(conpty.DialFor(instance))
			defer c.Close()
			if err := c.Shutdown(ctx, time.Second); err != nil {
				t.Logf("keeper shutdown: %v", err)
			}
		},
	}
}
