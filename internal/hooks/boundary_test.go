package hooks

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestHooksImportOnlyStoreAndWire keeps hooks a leaf domain (#527): the
// secret store and the alert mail are passed in, so it imports store and
// wire only.
func TestHooksImportOnlyStoreAndWire(t *testing.T) {
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
				t.Errorf("%s imports %s; read the table or take a function instead", path, p)
			}
		}
	}
}
