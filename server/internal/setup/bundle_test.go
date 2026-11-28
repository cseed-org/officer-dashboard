package setup

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"officerdashboard/internal/setup/sinkbundle"
)

// testBundle is the backup Worker bundle as the image carries it, built from the repo's
// real migrations and sources, with a stand-in module (the esbuild output needs Node).
func testBundle(t *testing.T) *sinkbundle.Bundle {
	t.Helper()
	dir := filepath.Join("..", "..", "deploy", "backup-sink")
	fsys := fstest.MapFS{sinkbundle.MainModule: {Data: []byte("var SINK_REVISION = 2;\nexport default { fetch() { return new Response(null, {status: 401}) } };\n")}}
	src, err := sinkbundle.ReadSources(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	for n, raw := range src {
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
