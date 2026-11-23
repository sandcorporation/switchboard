package watch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTablesStayInWatch keeps the watches and watch_seen tables in this
// package (#528): no other Go code reads or writes them (the store's
// migration aside), so a secret header value has one way in and none out.
func TestTablesStayInWatch(t *testing.T) {
	table := regexp.MustCompile(`(?i)\b(from|join|update|into)\s+(watches|watch_seen)\b`)
	root := ".."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (d.Name() == "watch" && filepath.Dir(path) == root || d.Name() == "store" || d.Name() == "testdata") {
				return filepath.SkipDir
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
		if table.Match(b) {
			t.Errorf("%s reads or writes the watch tables; go through internal/watch", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
