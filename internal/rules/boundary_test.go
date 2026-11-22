package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestRulesImportOnlyLeaves keeps the ledger a domain package that takes
// what it needs from others as functions (#406): it imports store and wire,
// never identity, asks, mail, server or dashboard.
func TestRulesImportOnlyLeaves(t *testing.T) {
	allowed := map[string]bool{"store": true, "wire": true}
	const prefix = "github.com/sandcorporation/switchboard/internal/"
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if rest, ok := strings.CutPrefix(p, prefix); ok && !allowed[rest] {
				t.Errorf("%s imports %s; take it as a function in Hooks or Caller", path, p)
			}
		}
	}
}

// TestRuleTablesStayInRules keeps the ledger's tables in this package: no
// other package (but store, which holds the schema) names rules,
// rule_topics, service_tags or role_tags in SQL. A failure here means a
// rule about rules grew a second home; add a function here instead.
func TestRuleTablesStayInRules(t *testing.T) {
	root := ".."
	table := regexp.MustCompile(`(?i)\b(from|join|update|into|table)\s+(rules|rule_topics|service_tags|role_tags)\b`)
	files := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if (d.Name() == "rules" || d.Name() == "store") && filepath.Dir(path) == root || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && table.MatchString(lit.Value) {
				t.Errorf("%s: names the rules ledger's tables outside internal/rules", fset.Position(lit.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 20 {
		t.Fatalf("walked %d files; the walk lost its root", files)
	}
}
