// Package builtin registers the adapters built into switchboard, and loads
// the ones a node's operator configures from a file. Import it for its
// effect (`import _ ".../internal/agent/builtin"`) or for LoadConfigured.
// It is the only package that imports adapter packages.
package builtin

import (
	// Claude Code, the default adapter.
	_ "github.com/sandcorporation/switchboard/internal/agent/claude"
	"github.com/sandcorporation/switchboard/internal/agent/generic"
)

// LoadConfigured registers the adapters described in the file at path
// (internal/agent/generic) and returns their names. readFile nil is
// os.ReadFile.
func LoadConfigured(path string, readFile func(string) ([]byte, error)) ([]string, error) {
	return generic.Load(path, readFile)
}
