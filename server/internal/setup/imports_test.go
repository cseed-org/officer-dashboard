package setup

import (
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestOnlyCfapiTalksHTTP keeps the read-only guarantee structural: discovery and the plan
// can reach Cloudflare only through cfapi.Reader, never through net/http directly.
func TestOnlyCfapiTalksHTTP(t *testing.T) {
	for _, dir := range []string{"cfprovision", "plan"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(gotoken.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if path == "net" || strings.HasPrefix(path, "net/http") {
					t.Errorf("%s/%s imports %s; only cfapi may talk HTTP", dir, name, path)
				}
			}
		}
	}
}
