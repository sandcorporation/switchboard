// Package generic is the adapter configured from a file (#434): a node
// operator describes an agent CLI (its executable, new-conversation and
// resume arguments, permission-skip arguments, environment names to drop and
// usage-limit messages) and it becomes an adapter without code.
//
// The file is JSON:
//
//	{"adapters": [{
//	  "name": "acme",
//	  "executable": "acme-agent",
//	  "new_args": ["--conversation", "{id}"],
//	  "resume_args": ["--reopen", "{id}"],
//	  "permission_skip_args": ["--no-confirm"],
//	  "drop_env": ["ACME_SESSION"],
//	  "limit_patterns": ["(?i)quota exhausted"]
//	}]}
//
// "{id}" in an argument is the conversation id the node gives the session.
// Without resume_args the agent cannot resume: every launch starts a new
// conversation.
package generic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent"
)

// IDPlaceholder stands for the conversation id in new_args and resume_args.
const IDPlaceholder = "{id}"

// ResumeFailRun: a resumed run that exits non-zero within it found no
// conversation to resume.
const ResumeFailRun = 15 * time.Second

// Spec is one adapter in the file.
type Spec struct {
	Name               string   `json:"name"`
	Executable         string   `json:"executable"`
	NewArgs            []string `json:"new_args"`
	ResumeArgs         []string `json:"resume_args,omitempty"`
	PermissionSkipArgs []string `json:"permission_skip_args,omitempty"`
	DropEnv            []string `json:"drop_env,omitempty"`
	LimitPatterns      []string `json:"limit_patterns,omitempty"`
}

// File is the configuration file.
type File struct {
	Adapters []Spec `json:"adapters"`
}

// Adapter is a configured agent.
type Adapter struct {
	spec   Spec
	limits []*regexp.Regexp
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// New checks spec and builds its adapter.
func New(spec Spec) (*Adapter, error) {
	fail := func(format string, a ...any) (*Adapter, error) {
		return nil, fmt.Errorf("adapter %q: "+format, append([]any{spec.Name}, a...)...)
	}
	if spec.Name == "" {
		return nil, errors.New("adapter: name is required")
	}
	if strings.TrimSpace(spec.Executable) == "" {
		return fail("executable is required")
	}
	if len(spec.ResumeArgs) > 0 {
		if !hasID(spec.ResumeArgs) {
			return fail("resume_args must contain %s", IDPlaceholder)
		}
		if !hasID(spec.NewArgs) {
			return fail("resume_args resumes by %s, so new_args must give it", IDPlaceholder)
		}
	}
	for _, e := range spec.DropEnv {
		if !envNamePattern.MatchString(e) {
			return fail("drop_env: invalid name %q", e)
		}
	}
	a := &Adapter{spec: spec}
	for _, p := range spec.LimitPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fail("limit_patterns: %v", err)
		}
		a.limits = append(a.limits, re)
	}
	return a, nil
}

func hasID(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, IDPlaceholder) })
}

// Parse reads a configuration file; unknown fields are an error.
func Parse(r io.Reader) ([]*Adapter, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("adapters file: %w", err)
	}
	var out []*Adapter
	seen := map[string]bool{}
	for _, s := range f.Adapters {
		if seen[s.Name] {
			return nil, fmt.Errorf("adapters file: %q appears twice", s.Name)
		}
		seen[s.Name] = true
		a, err := New(s)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// Load reads the file at path and registers its adapters. An adapter
// already registered from the same spec (a second load in one process) is
// kept; any other name already taken is an error.
func Load(path string, readFile func(string) ([]byte, error)) ([]string, error) {
	if readFile == nil {
		readFile = os.ReadFile
	}
	b, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("adapters file: %w", err)
	}
	list, err := Parse(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var names []string
	for _, a := range list {
		if have, err := agent.Lookup(a.Name()); err == nil {
			if g, ok := have.(*Adapter); ok && g.equal(a) {
				names = append(names, a.Name())
				continue
			}
			return nil, fmt.Errorf("%s: adapter %q is already registered", path, a.Name())
		}
		if err := agent.Register(a); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		names = append(names, a.Name())
	}
	return names, nil
}

func (a *Adapter) equal(b *Adapter) bool {
	x, _ := json.Marshal(a.spec)
	y, _ := json.Marshal(b.spec)
	return bytes.Equal(x, y)
}

func (a *Adapter) Name() string       { return a.spec.Name }
func (a *Adapter) Executable() string { return a.spec.Executable }

// LaunchArgs fills the placeholder in resume_args (resume) or new_args.
func (a *Adapter) LaunchArgs(conversation string, resume bool) []string {
	args := a.spec.NewArgs
	if resume && len(a.spec.ResumeArgs) > 0 {
		args = a.spec.ResumeArgs
	}
	out := make([]string, len(args))
	for i, s := range args {
		out[i] = strings.ReplaceAll(s, IDPlaceholder, conversation)
	}
	return out
}

// PermissionSkipArgs are permission_skip_args, unless args already carry
// one of them.
func (a *Adapter) PermissionSkipArgs(args []string) []string {
	for _, s := range args {
		name, _, _ := strings.Cut(s, "=")
		if slices.Contains(a.spec.PermissionSkipArgs, name) {
			return nil
		}
	}
	return slices.Clone(a.spec.PermissionSkipArgs)
}

// RefusedArgs are the flags of new_args and resume_args: the node sets
// them itself.
func (a *Adapter) RefusedArgs() map[string]string {
	out := map[string]string{}
	for _, s := range slices.Concat(a.spec.NewArgs, a.spec.ResumeArgs) {
		if name, _, _ := strings.Cut(s, "="); strings.HasPrefix(name, "-") && !strings.Contains(name, IDPlaceholder) {
			out[name] = "the node sets the conversation itself"
		}
	}
	return out
}

// RoleArgs: a role's session of a configured agent carries no flags.
func (a *Adapter) RoleArgs() map[string]*regexp.Regexp { return nil }

func (a *Adapter) SessionMarkers() []string { return slices.Clone(a.spec.DropEnv) }

// ResumeFailed: a resumed run that exits non-zero within ResumeFailRun.
// An agent without resume_args never resumes, so never fails to.
func (a *Adapter) ResumeFailed(code int, ran time.Duration) bool {
	return len(a.spec.ResumeArgs) > 0 && code != 0 && ran < ResumeFailRun
}

func (a *Adapter) DetectLimit(rows []string, at time.Time) (agent.LimitHit, bool) {
	if len(a.limits) == 0 {
		return agent.LimitHit{}, false
	}
	return agent.MatchLimit(a.limits, rows, at)
}

// Words: a configured agent's strings live in the operator's file, not in
// code.
func (a *Adapter) Words() []string { return nil }

// Spec is the adapter's configuration.
func (a *Adapter) Spec() Spec {
	s := a.spec
	s.NewArgs, s.ResumeArgs = slices.Clone(s.NewArgs), slices.Clone(s.ResumeArgs)
	return s
}
