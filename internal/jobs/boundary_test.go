package jobs

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

// TestLedgerRulesStayInJobs keeps the ledger's read rules in this package
// (#404): which rows a view shows, and that a list carries no command and no
// prompt. Outside it, no code may blank a Command or Prompt field, read the
// jobs or schedules tables, and the server and dashboard may not handle
// wire.Job or wire.Schedule values (they pass a View in and write what comes
// back). A fails here means a rule grew a second home; move it into View.
func TestLedgerRulesStayInJobs(t *testing.T) {
	root := filepath.Join("..")
	table := regexp.MustCompile(`(?i)\b(from|join|update|into)\s+(jobs|schedules)\b`)
	files := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "jobs" && filepath.Dir(path) == root || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		pkg := filepath.Base(filepath.Dir(path))
		if pkg == "store" { // the schema and its migrations
			return nil
		}
		files++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		guarded := pkg == "server" || pkg == "dashboard"
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "Prompt") || i >= len(n.Rhs) {
						continue
					}
					if lit, ok := n.Rhs[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, _ := strconv.Unquote(lit.Value); v == "" {
							t.Errorf("%s: blanks .%s outside internal/jobs; use jobs.View", fset.Position(n.Pos()), sel.Sel.Name)
						}
					}
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING && table.MatchString(n.Value) {
					t.Errorf("%s: reads the ledger's tables outside internal/jobs", fset.Position(n.Pos()))
				}
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); guarded && ok && x.Name == "wire" && (n.Sel.Name == "Job" || n.Sel.Name == "Schedule") {
					t.Errorf("%s: %s handles wire.%s; let jobs.View filter and shape the rows", fset.Position(n.Pos()), pkg, n.Sel.Name)
				}
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
