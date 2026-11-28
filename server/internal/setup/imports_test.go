package setup

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// talksHTTP are the only packages under internal/setup allowed to import net/http:
// the read-only client, the write client (internal to cfprovision, so only the apply
// engine can construct it) and the fake API used by tests.
var talksHTTP = map[string]bool{
	"cfapi":                        true,
	"cfprovision/internal/cfwrite": true,
	"cffake":                       true,
	// Discord, not Cloudflare: the Discord client and its fake (setup discord).
	"discord":             true,
	"discord/discordfake": true,
}

// sourceFiles returns every non-test Go file under internal/setup, by package directory.
func sourceFiles(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		out[dir] = append(out[dir], path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOnlyCfapiTalksHTTP keeps the read-only guarantee structural: discovery, the plan,
// the apply engine and the command reach Cloudflare only through cfapi.Reader (GET) or
// the cfwrite client (POST, PUT), never through net/http directly.
func TestOnlyCfapiTalksHTTP(t *testing.T) {
	files := sourceFiles(t)
	for _, must := range []string{"cfprovision", "plan", "cfapi", "cfprovision/internal/cfwrite"} {
		if len(files[must]) == 0 {
			t.Fatalf("no source files found in %s; the walk is broken", must)
		}
	}
	for dir, paths := range files {
		if talksHTTP[dir] {
			continue
		}
		for _, path := range paths {
			f, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if p == "net" || strings.HasPrefix(p, "net/http") {
					t.Errorf("%s imports %s; only %v may talk HTTP", path, p, talksHTTP)
				}
			}
		}
	}
}

// TestNoDeleteAnywhere fails if any setup source file can send a DELETE: no
// http.MethodDelete, no "DELETE" string literal and no method named Delete on a client.
// Setup never removes anything from a Cloudflare account.
func TestNoDeleteAnywhere(t *testing.T) {
	for _, paths := range sourceFiles(t) {
		for _, path := range paths {
			f, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if x.Sel.Name == "MethodDelete" {
						t.Errorf("%s uses http.MethodDelete", path)
					}
				case *ast.BasicLit:
					if x.Kind == gotoken.STRING && strings.EqualFold(strings.Trim(x.Value, "\"`"), "DELETE") {
						t.Errorf("%s has the string literal %s", path, x.Value)
					}
				case *ast.FuncDecl:
					if x.Recv != nil && strings.EqualFold(x.Name.Name, "delete") {
						t.Errorf("%s declares a method %s", path, x.Name.Name)
					}
				}
				return true
			})
		}
	}
}
