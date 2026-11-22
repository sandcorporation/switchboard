package builtin_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/agent"
	_ "github.com/sandcorporation/switchboard/internal/agent/builtin"
)

const module = "github.com/sandcorporation/switchboard/"

// repoRoot is the module root, three levels up from internal/agent/builtin.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// words is every registered adapter's words, lower case.
func words() []string {
	var out []string
	for _, a := range agent.All() {
		for _, w := range a.Words() {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

// hit is one adapter word found outside the adapters.
type hit struct {
	file string
	line int
	word string
}

// scan walks root's Go code outside internal/agent/<name>/ — tests and the
// fake agent (internal/node/fakeagent) excepted — for words, without case.
func scan(t *testing.T, root string, words []string) []hit {
	t.Helper()
	adapters := filepath.Join(root, "internal", "agent") + string(filepath.Separator)
	fake := filepath.Join(root, "internal", "node", "fakeagent")
	var out []hit
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case path == fake, d.Name() == "testdata", d.Name() == ".git", d.Name() == "vendor":
				return filepath.SkipDir
			case strings.HasPrefix(path, adapters) && filepath.Dir(path)+string(filepath.Separator) == adapters:
				return filepath.SkipDir // internal/agent/<name>
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(strings.ToLower(string(b)), "\n") {
			for _, w := range words {
				if strings.Contains(line, w) {
					rel, _ := filepath.Rel(root, path)
					out = append(out, hit{rel, i + 1, w})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestAgentWordsStayInAdapters is the adapter boundary (#434): an agent's
// name, its own flags and environment names appear only in its adapter
// (internal/agent/<name>). Everything else calls the agent.Adapter
// interface. A failure here means agent-specific code grew outside its
// adapter; move it behind the interface.
func TestAgentWordsStayInAdapters(t *testing.T) {
	ws := words()
	if len(ws) == 0 {
		t.Fatal("no adapter words: nothing registered")
	}
	for _, h := range scan(t, repoRoot(t), ws) {
		t.Errorf("%s:%d: %q belongs to an adapter (internal/agent/<name>)", h.file, h.line, h.word)
	}
}

// The scan does catch a word outside the adapters, and leaves the adapters,
// tests and the fake agent alone.
func TestAgentWordScanCatchesAWordOutsideAdapters(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module x\n")
	write("internal/node/launch.go", "package node\n\nvar flag = \"--Fixture-Only\"\n")
	write("internal/agent/fixture/fixture.go", "package fixture\n\nvar flag = \"--fixture-only\"\n")
	write("internal/node/launch_test.go", "package node\n\nvar flag = \"--fixture-only\"\n")
	write("internal/node/fakeagent/fake.go", "package fakeagent\n\nvar flag = \"--fixture-only\"\n")
	write("internal/agent/registry.go", "package agent\n\nvar flag = \"--fixture-only\"\n")
	got := scan(t, root, []string{"--fixture-only"})
	var files []string
	for _, h := range got {
		files = append(files, filepath.ToSlash(h.file))
	}
	want := []string{"internal/agent/registry.go", "internal/node/launch.go"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Fatalf("hits %v, want %v", files, want)
	}
}

// TestAdapterImports: an adapter package (internal/agent/<name>) imports
// only internal/agent among the module's packages, and only
// internal/agent/builtin imports adapter packages.
func TestAdapterImports(t *testing.T) {
	root := repoRoot(t)
	adapters := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "agent"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "builtin" {
			adapters[module+"internal/agent/"+e.Name()] = true
		}
	}
	if len(adapters) == 0 {
		t.Fatal("no adapter packages found")
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		pkg := module + filepath.ToSlash(rel)
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if !strings.HasPrefix(p, module) {
				continue
			}
			switch {
			case adapters[pkg] && p != module+"internal/agent" && !strings.HasSuffix(path, "_test.go"):
				t.Errorf("%s imports %s; an adapter imports only internal/agent", path, p)
			case adapters[p] && pkg != module+"internal/agent/builtin" && !adapters[pkg]:
				t.Errorf("%s imports adapter %s; use the registry (internal/agent) and internal/agent/builtin", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
