package cfprovision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"officerdashboard/internal/setup/sinkbundle"
)

// testBundle is the backup Worker bundle as the image carries it, built from the repo's
// real migrations and sources, with a stand-in module (the esbuild output needs Node).
func testBundle(t *testing.T) *sinkbundle.Bundle {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "deploy", "backup-sink")
	fsys := fstest.MapFS{sinkbundle.MainModule: {Data: []byte("var SINK_REVISION = 2;\nexport default { fetch() { return new Response(null, {status: 401}) } };\n")}}
	ms, err := filepath.Glob(filepath.Join(dir, sinkbundle.MigrationsDir, "*.sql"))
	if err != nil || len(ms) == 0 {
		t.Fatalf("no migrations (%v)", err)
	}
	var bare []string
	for _, m := range ms {
		bare = append(bare, filepath.Base(m))
	}
	for _, n := range sinkbundle.SourceFiles(bare) {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(n)))
		if err != nil {
			t.Fatal(err)
		}
		fsys[n] = &fstest.MapFile{Data: raw}
	}
	b, err := sinkbundle.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Check(); err != nil {
		t.Fatal(err)
	}
	return &b
}

// testBearer is the host's backup bearer in these tests, built at run time so the source
// holds nothing that looks like a credential. It is 36 bytes, over the Worker's 32.
var testBearer = "bk-" + strings.Repeat("Qw7", 11)
