// Package builtin registers the adapters built into switchboard. Import it
// for its effect: `import _ ".../internal/agent/builtin"`. It is the only
// package that imports adapter packages.
package builtin

import (
	// Claude Code, the default adapter.
	_ "github.com/sandcorporation/switchboard/internal/agent/claude"
)
