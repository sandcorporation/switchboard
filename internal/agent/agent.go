// Package agent is the one seam between switchboard and the agent CLIs it
// hosts (#434). An Adapter holds everything one agent does its own way: its
// executable, its launch and resume arguments, its permission-skip
// arguments, the flags a record may not carry, the flags a role's session may
// carry, the session markers its children must not inherit, how a failed
// resume looks and how its usage-limit message reads.
//
// Adapters live in internal/agent/<name> and import only this package; they
// register themselves by name. Every other package finds an adapter here by
// name and calls the interface. A structure test fails when an adapter's
// words (Adapter.Words) appear in Go code outside internal/agent/<name>.
package agent

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Adapter is one agent CLI.
type Adapter interface {
	// Name is the registry key: lower case letters, digits, '-' and '_'.
	Name() string
	// Executable is the CLI's file name without extension, looked up on PATH
	// (exec.LookPath honours PATHEXT, so a Windows .cmd or .exe is found).
	Executable() string
	// LaunchArgs are the arguments after the executable that start a
	// conversation: a new one with the given id, or (resume) the one with
	// that id. An agent that cannot resume returns its new-conversation
	// arguments for both.
	LaunchArgs(conversation string, resume bool) []string
	// PermissionSkipArgs are the arguments that make the agent run without
	// permission prompts, or nothing when args already choose a permission
	// mode or the agent has no such arguments.
	PermissionSkipArgs(args []string) []string
	// RefusedArgs are the flags a record may not carry, each with the
	// reason: the ones the node sets itself and the ones that make the agent
	// non-interactive.
	RefusedArgs() map[string]string
	// RoleArgs are the flags a role's session may carry, each with the shape
	// its value must have. Nil: a role's session carries no flags.
	RoleArgs() map[string]*regexp.Regexp
	// SessionMarkers are the environment names a running agent sets for its
	// own session and children; a hosted session never inherits them.
	SessionMarkers() []string
	// ResumeFailed reports whether a resumed run that exited with code after
	// ran counts as a failure to resume (the conversation is gone).
	ResumeFailed(code int, ran time.Duration) bool
	// DetectLimit finds the newest usage-limit message on a screen (its
	// rows), read at at.
	DetectLimit(rows []string, at time.Time) (LimitHit, bool)
	// Words are the strings only this adapter's code may contain: its name,
	// its own flags and environment names. Matched without case.
	Words() []string
}

// LimitHit is the newest usage-limit message found on a screen: the text
// from the match on (its fingerprint), the reset time it gave (zero when
// none) and how many times the screen shows that text.
type LimitHit struct {
	Text  string
	Reset time.Time
	Count int
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ErrUnknown is returned for a name no adapter is registered under.
var ErrUnknown = errors.New("agent: unknown adapter")

var (
	mu          sync.RWMutex
	adapters    = map[string]Adapter{}
	defaultName string
)

// Register adds a under its name. A name already taken or not matching
// [a-z0-9][a-z0-9_-]* is refused.
func Register(a Adapter) error {
	name := a.Name()
	if !namePattern.MatchString(name) {
		return fmt.Errorf("agent: adapter name %q (lower case letters, digits, - and _)", name)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := adapters[name]; ok {
		return fmt.Errorf("agent: adapter %q is already registered", name)
	}
	adapters[name] = a
	return nil
}

// MustRegister is Register for an adapter's init; it panics on a refusal.
func MustRegister(a Adapter) {
	if err := Register(a); err != nil {
		panic(err)
	}
}

// SetDefault makes the registered adapter name the one a node launches when
// neither the session nor the node names one.
func SetDefault(name string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := adapters[name]; !ok {
		panic(fmt.Sprintf("agent: default %q is not registered", name))
	}
	defaultName = name
}

// Lookup is the adapter registered under name.
func Lookup(name string) (Adapter, error) {
	mu.RLock()
	defer mu.RUnlock()
	if a, ok := adapters[name]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("%w %q (known: %s)", ErrUnknown, name, strings.Join(namesLocked(), ", "))
}

// Default is the default adapter. It panics when none is set: a binary
// without its built-in adapters is a build mistake.
func Default() Adapter {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := adapters[defaultName]
	if !ok {
		panic("agent: no default adapter registered (import internal/agent/builtin)")
	}
	return a
}

// Names lists the registered adapters, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	return namesLocked()
}

func namesLocked() []string {
	out := make([]string, 0, len(adapters))
	for n := range adapters {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// All is every registered adapter, sorted by name.
func All() []Adapter {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Adapter, 0, len(adapters))
	for _, n := range namesLocked() {
		out = append(out, adapters[n])
	}
	return out
}

// SessionMarkers is every registered adapter's session markers: a node
// started inside any agent's session drops them all from its children.
func SessionMarkers() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range All() {
		for _, m := range a.SessionMarkers() {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// ExecName is an executable's name from its path: the file name, lower case,
// without .exe, .cmd or .bat (`C:\x\tool.cmd` is "tool").
func ExecName(path string) string {
	name := strings.ToLower(path[strings.LastIndexAny(path, `/\`)+1:])
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

// CheckRoleArgs returns an error naming the first arg a role's session of a
// may not carry (#377). A flag takes its value as the next arg or after "=".
func CheckRoleArgs(a Adapter, args []string) error {
	allowed := a.RoleArgs()
	names := make([]string, 0, len(allowed))
	for n := range allowed {
		names = append(names, n)
	}
	sort.Strings(names)
	for i := 0; i < len(args); i++ {
		name, val, inline := strings.Cut(args[i], "=")
		re, ok := allowed[name]
		if !ok {
			return fmt.Errorf("args: %s is not allowed for a role's session (allowed: %s)", args[i], strings.Join(names, ", "))
		}
		if !inline {
			if i+1 >= len(args) {
				return fmt.Errorf("args: %s needs a value", name)
			}
			i++
			val = args[i]
		}
		if !re.MatchString(val) {
			return fmt.Errorf("args: %s value %q is not allowed for a role's session (letters, digits and . _ - only)", name, val)
		}
	}
	return nil
}

// RefusedArg reports whether arg (a flag, or flag=value) is one a refuses.
func RefusedArg(a Adapter, arg string) bool {
	name, _, _ := strings.Cut(arg, "=")
	_, ok := a.RefusedArgs()[name]
	return ok
}
