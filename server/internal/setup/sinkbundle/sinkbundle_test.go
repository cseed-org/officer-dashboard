package sinkbundle

import (
	"os"
	"path"
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

// repoSources reads the Worker's source files from the repository.
func repoSources(t *testing.T) map[string][]byte {
	t.Helper()
	src, err := ReadSources(os.DirFS(sinkDir))
	if err != nil {
		t.Fatal(err)
	}
	return src
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

// The schema setup expects is the one the migration files make: their tables, their
// triggers and their names (review F-2 of #129).
func TestSchemaMatchesTheMigrations(t *testing.T) {
	var tables, triggers, names []string
	for n, raw := range repoSources(t) {
		if !strings.HasPrefix(n, MigrationsDir+"/") {
			continue
		}
		names = append(names, path.Base(n))
		for _, m := range regexp.MustCompile(`(?i)CREATE TABLE (\w+)`).FindAllStringSubmatch(string(raw), -1) {
			tables = append(tables, m[1])
		}
		for _, m := range regexp.MustCompile(`(?i)CREATE TRIGGER (\w+)`).FindAllStringSubmatch(string(raw), -1) {
			triggers = append(triggers, m[1])
		}
	}
	for _, c := range []struct {
		what      string
		got, want []string
	}{{"tables", tables, Tables}, {"triggers", triggers, Triggers}, {"migration names", names, MigrationNames}} {
		sort.Strings(c.got)
		if !slices.Equal(c.got, c.want) {
			t.Errorf("the migrations make %s %v, sinkbundle lists %v", c.what, c.got, c.want)
		}
	}
}

// Review F-3 of #129: the backstop when a listing is wrong. Without IF NOT EXISTS, the
// migration's first CREATE fails on a database that has the tables, so it can never alter
// one.
func TestMigrationsNeverUseIfNotExists(t *testing.T) {
	for n, raw := range repoSources(t) {
		if strings.HasPrefix(n, MigrationsDir+"/") && regexp.MustCompile(`(?i)IF\s+NOT\s+EXISTS`).Match(raw) {
			t.Errorf("%s uses IF NOT EXISTS", n)
		}
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
	bundleFS := func() fstest.MapFS {
		fsys := fstest.MapFS{MainModule: {Data: []byte("var SINK_REVISION = 2;\nexport default {};\n")}}
		for n, raw := range repoSources(t) {
			fsys[n] = &fstest.MapFile{Data: raw}
		}
		return fsys
	}
	b, err := Load(bundleFS())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Migrations) != 1 || b.Migrations[0].Name != "0001.sql" || !strings.Contains(b.Migrations[0].SQL, "CREATE TABLE snapshots") {
		t.Fatalf("migrations %+v", b.Migrations)
	}
	if err := b.Check(); err != nil {
		t.Fatal(err)
	}
	edited := bundleFS()
	edited["src/index.ts"] = &fstest.MapFile{Data: append([]byte("// edited\n"), repoSources(t)["src/index.ts"]...)}
	if b, err := Load(edited); err != nil || b.Check() == nil {
		t.Fatalf("a bundle from changed sources passes the check (%v)", err)
	}
	// Review F-8 of #129: esbuild bundles whatever index.ts imports, so any new file
	// under src/ is part of the digest.
	extra := bundleFS()
	extra["src/util.ts"] = &fstest.MapFile{Data: []byte("export const x = 1;\n")}
	if b, err := Load(extra); err != nil || b.Check() == nil {
		t.Fatalf("a bundle with a new source file passes the check (%v)", err)
	}
	if _, err := Load(fstest.MapFS{MainModule: {Data: []byte("var SINK_REVISION = 2;")}}); err == nil {
		t.Fatal("a bundle without sources loads")
	}
}

// The person's request in fix round 1 of #129: a table of the right name but another shape
// is drift. The pinned definitions are the migration files' own, normalized.
func TestDefinitionsMatchTheMigrations(t *testing.T) {
	tables, triggers := map[string]string{}, map[string]string{}
	for n, raw := range repoSources(t) {
		if strings.HasPrefix(n, MigrationsDir+"/") {
			tb, tr := Definitions(string(raw))
			for k, v := range tb {
				tables[k] = v
			}
			for k, v := range tr {
				triggers[k] = v
			}
		}
	}
	for name, want := range TableDefinitions {
		if tables[name] != want {
			t.Errorf("table %s: the migration says\n%q\nsinkbundle pins\n%q", name, tables[name], want)
		}
	}
	for name, want := range TriggerDefinitions {
		if triggers[name] != want {
			t.Errorf("trigger %s: the migration says\n%q\nsinkbundle pins\n%q", name, triggers[name], want)
		}
	}
	if len(tables) != len(TableDefinitions) || len(triggers) != len(TriggerDefinitions) {
		t.Errorf("the migrations define %d tables and %d triggers; sinkbundle pins %d and %d", len(tables), len(triggers), len(TableDefinitions), len(TriggerDefinitions))
	}
	for _, name := range Tables {
		if TableDefinitions[name] == "" {
			t.Errorf("no definition pinned for table %s", name)
		}
	}
}

// Spellings SQLite may store for the same statement normalize alike; a different column
// does not.
func TestNormalizeSQL(t *testing.T) {
	a := NormalizeSQL("CREATE TABLE \"budgets\" (day TEXT PRIMARY KEY,\n  writes INTEGER NOT NULL DEFAULT 0, uploads INTEGER NOT NULL DEFAULT 0);")
	b := NormalizeSQL("create table budgets(day text primary key, writes integer not null default 0 , uploads integer not null default 0)")
	if a != b || a != TableDefinitions["budgets"] {
		t.Fatalf("%q\n%q", a, b)
	}
	if NormalizeSQL("CREATE TABLE budgets (day TEXT PRIMARY KEY, writes INTEGER)") == a {
		t.Fatal("a different shape normalizes to the same text")
	}
}
