package sinkbundle

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// sinkDir is the Worker's source directory, seen from this package.
var sinkDir = filepath.Join("..", "..", "..", "deploy", "backup-sink")

// RepoSources reads the Worker's source files from the repository.
func repoSources(t *testing.T) map[string][]byte {
	t.Helper()
	ms, err := filepath.Glob(filepath.Join(sinkDir, MigrationsDir, "*.sql"))
	if err != nil || len(ms) == 0 {
		t.Fatalf("no migrations in %s (%v)", sinkDir, err)
	}
	var bare []string
	for _, m := range ms {
		bare = append(bare, filepath.Base(m))
	}
	out := map[string][]byte{}
	for _, n := range SourceFiles(bare) {
		raw, err := os.ReadFile(filepath.Join(sinkDir, filepath.FromSlash(n)))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = raw
	}
	return out
}

// The staleness check, source side: CI fails when the Worker's source changes without a
// new revision, so the version the backup service compares never lies.
func TestSourceMatchesThePinnedRevision(t *testing.T) {
	src := repoSources(t)
	if got := SourceRevision(src["src/index.ts"]); got != Revision {
		t.Fatalf("src/index.ts declares SINK_REVISION %d; sinkbundle.Revision is %d. Keep them equal", got, Revision)
	}
	if got := Digest(src); got != SourceDigest {
		t.Fatalf("the backup Worker's source changed (digest %s, pinned %s).\n"+
			"Raise SINK_REVISION in server/deploy/backup-sink/src/index.ts and sinkbundle.Revision to %d, then set sinkbundle.SourceDigest to %q.",
			got, SourceDigest, Revision+1, got)
	}
}

func TestWranglerSettingsMatch(t *testing.T) {
	toml := string(repoSources(t)["wrangler.toml"])
	for key, want := range map[string]string{
		"name":                `"` + ScriptName + `"`,
		"compatibility_date":  `"` + CompatibilityDate + `"`,
		"compatibility_flags": `["` + strings.Join(CompatibilityFlags, `", "`) + `"]`,
		"binding":             `"` + D1Binding + `"`,
		"migrations_dir":      `"` + MigrationsDir + `"`,
	} {
		m := regexp.MustCompile(`(?m)^` + key + ` = (.+?)\r?$`).FindStringSubmatch(toml)
		if m == nil || strings.TrimSpace(m[1]) != want {
			t.Errorf("wrangler.toml %s = %v, sinkbundle has %s", key, m, want)
		}
	}
}

func TestTablesMatchTheMigration(t *testing.T) {
	var got []string
	for n, raw := range repoSources(t) {
		if strings.HasPrefix(n, MigrationsDir+"/") {
			for _, m := range regexp.MustCompile(`(?i)CREATE TABLE (\w+)`).FindAllStringSubmatch(string(raw), -1) {
				got = append(got, m[1])
			}
		}
	}
	sort.Strings(got)
	if !slices.Equal(got, Tables) {
		t.Fatalf("the migrations create %v, sinkbundle.Tables is %v", got, Tables)
	}
}

func TestDigestIgnoresLineEndingsButNotContent(t *testing.T) {
	lf := map[string][]byte{"src/index.ts": []byte("a\nb\n")}
	crlf := map[string][]byte{"src/index.ts": []byte("a\r\nb\r\n")}
	if Digest(lf) != Digest(crlf) {
		t.Fatal("a Windows checkout and a Linux build disagree")
	}
	if Digest(lf) == Digest(map[string][]byte{"src/index.ts": []byte("a\nc\n")}) {
		t.Fatal("the digest does not see a change")
	}
}

// A build without the embedsink tag has no bundle, and says so; go test passes without it.
func TestBuildWithoutTheBundleHasNone(t *testing.T) {
	if _, ok := Embedded(); ok != embedded {
		t.Fatalf("Embedded() = %v in a build where embedded is %v", ok, embedded)
	}
}

// Load and Check on a bundle laid out as the Dockerfile writes it, from the repo sources.
func TestLoadAndCheck(t *testing.T) {
	fsys := fstest.MapFS{MainModule: {Data: []byte("var SINK_REVISION = 2;\nexport default {};\n")}}
	for n, raw := range repoSources(t) {
		fsys[n] = &fstest.MapFile{Data: raw}
	}
	b, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Migrations) != 1 || b.Migrations[0].Name != "0001.sql" || !strings.Contains(b.Migrations[0].SQL, "CREATE TABLE snapshots") {
		t.Fatalf("migrations %+v", b.Migrations)
	}
	if err := b.Check(); err != nil {
		t.Fatal(err)
	}
	fsys["src/index.ts"] = &fstest.MapFile{Data: append([]byte("// edited\n"), repoSources(t)["src/index.ts"]...)}
	if b, err := Load(fsys); err != nil || b.Check() == nil {
		t.Fatalf("a bundle from changed sources passes the check (%v)", err)
	}
	if _, err := Load(fstest.MapFS{MainModule: {Data: []byte("var SINK_REVISION = 2;")}}); err == nil {
		t.Fatal("a bundle without migrations loads")
	}
}
