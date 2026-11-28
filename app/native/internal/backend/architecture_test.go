package backend

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These rules constrain real imports, not file size or formatting. Keep the
// Git engine independent and presentation code outside the business owner.
func TestNativeArchitectureDependencyBoundaries(t *testing.T) {
	for _, directory := range []string{".", "../gitcli"} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(directory, name)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range file.Imports {
				importPath, err := strconv.Unquote(item.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if directory == "../gitcli" {
					first, _, _ := strings.Cut(importPath, "/")
					if strings.Contains(first, ".") {
						t.Errorf("%s: Git execution/cache must depend only on the standard library, got %s", path, importPath)
					}
				} else if strings.HasPrefix(importPath, "github.com/egoist/mygo/ui") || strings.Contains(importPath, "/app/native/internal/arcmd") {
					t.Errorf("%s: presentation belongs outside the backend, got %s", path, importPath)
				}
			}
		}
	}
}
