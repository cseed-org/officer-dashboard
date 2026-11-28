// Package sinkbundle is the backup Worker (server/deploy/backup-sink) as the image carries
// it: the esbuild bundle of src/index.ts and its D1 migrations, so `dashboard setup` can
// deploy the Worker without Node on the host.
//
// The release image builds the bundle in a Dockerfile stage and compiles it in with the
// embedsink build tag (embed_on.go). Any other build has no bundle (embed_off.go):
// Embedded reports false, and setup refuses to create the Worker or apply its migration.
//
// The Worker's version is Revision, an integer the Worker's own source also carries
// (SINK_REVISION in src/index.ts) and reports on its authenticated GET /version. Setup
// also uploads it as the plain-text binding RevisionBinding, which Cloudflare's settings
// API shows, so a preview can read it without the Worker's secrets. SourceDigest pins the
// Worker's source: a test fails when the source changes without a new Revision, and the
// image build fails when the embedded bundle was not built from that source.
package sinkbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Revision is the backup Worker's revision. Raise it, with SINK_REVISION in
// server/deploy/backup-sink/src/index.ts, whenever the Worker's source, migrations or
// wrangler.toml settings change; TestSourceMatchesThePinnedRevision says when.
const Revision = 2

// SourceDigest is Digest over the Worker's source files (SourceFiles) at Revision.
const SourceDigest = "sha256:2b804614d90a3c57d9571fa8a037c47f4ed71b2c83ff9e9b984298c1636c4636"

// MinRevision is the oldest Worker revision the backup service can work with. The upload
// protocol has not changed since revision 1 (revision 2 only added GET /version), so it is
// 1, and no deployed Worker is too old. Raise it only together with a way to upgrade an
// existing Worker: the backup service alerts every install whose Worker reports less.
const MinRevision = 1

// From server/deploy/backup-sink/wrangler.toml (TestWranglerSettingsMatch checks them).
const (
	ScriptName        = "dashboard-backup-sink" // name
	CompatibilityDate = "2026-07-30"            // compatibility_date
	D1Binding         = "BACKUPS"               // [[d1_databases]] binding
	MigrationsDir     = "migrations"            // migrations_dir
)

// CompatibilityFlags is wrangler.toml's compatibility_flags.
var CompatibilityFlags = []string{"nodejs_compat"}

// MainModule is the upload's module part name (metadata main_module).
const MainModule = "worker.js"

// RevisionBinding is the plain-text binding setup uploads with the Worker, holding
// Revision as text.
const RevisionBinding = "SINK_REVISION"

// The only SQL setup sends to a D1 database besides the migration itself. Both only read.
const (
	// TablesSQL lists a database's tables. The D1 list's num_tables is "Deprecated … This
	// count is no longer accurate and should not be relied upon"
	// (https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/list/),
	// so whether a database is empty is decided by this query alone.
	TablesSQL = "SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name"
	// AppliedSQL lists the migrations Wrangler's d1_migrations table records.
	AppliedSQL = `SELECT name FROM "d1_migrations" ORDER BY id`
)

// ReadOnlySQL are the statements setup sends without changing a database.
var ReadOnlySQL = []string{TablesSQL, AppliedSQL}

// Tables are the tables the migrations create (TestTablesMatchTheMigration checks them).
var Tables = []string{"budgets", "chunks", "snapshots"}

// Migration is one D1 migration file: Name is its path relative to migrations_dir, which
// is how Wrangler records it in d1_migrations.
type Migration struct {
	Name string
	SQL  string
}

// Bundle is what the image carries.
type Bundle struct {
	Script     []byte
	Migrations []Migration
	// Sources are the files the bundle was built from, by SourceFiles name, so the image
	// build can check them against SourceDigest.
	Sources map[string][]byte
}

// SourceFiles names the files Digest covers, relative to server/deploy/backup-sink: the
// Worker's code and settings and every migration. Development dependencies (the
// lockfile) are left out, so a test-tool update needs no new revision.
func SourceFiles(migrations []string) []string {
	ms := append([]string(nil), migrations...)
	sort.Strings(ms)
	out := []string{"src/index.ts", "wrangler.toml"}
	for _, m := range ms {
		out = append(out, path.Join(MigrationsDir, m))
	}
	return out
}

// Digest hashes files (by SourceFiles name) with line endings normalized to LF, so a
// Windows checkout and a Linux build agree. Each file is framed by its name and length.
func Digest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		body := strings.ReplaceAll(string(files[n]), "\r\n", "\n")
		fmt.Fprintf(h, "%s\x00%d\x00%s", n, len(body), body)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

var revisionInSource = regexp.MustCompile(`(?m)^(?:const|var|let) SINK_REVISION = (\d+);`)

// SourceRevision is the SINK_REVISION a Worker source or bundle declares, or -1.
func SourceRevision(code []byte) int {
	if m := revisionInSource.FindSubmatch(code); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			return n
		}
	}
	return -1
}

// Load reads a bundle laid out as the Dockerfile's sink stage writes it: worker.js, the
// sources under their SourceFiles names, and migrations/*.sql.
func Load(fsys fs.FS) (Bundle, error) {
	var b Bundle
	var err error
	if b.Script, err = fs.ReadFile(fsys, MainModule); err != nil {
		return Bundle{}, fmt.Errorf("backup Worker bundle: %w", err)
	}
	names, err := fs.Glob(fsys, MigrationsDir+"/*.sql")
	if err != nil || len(names) == 0 {
		return Bundle{}, errors.New("backup Worker bundle: no migrations")
	}
	sort.Strings(names)
	var bare []string
	for _, n := range names {
		raw, err := fs.ReadFile(fsys, n)
		if err != nil {
			return Bundle{}, fmt.Errorf("backup Worker bundle: %w", err)
		}
		name := strings.TrimPrefix(n, MigrationsDir+"/")
		bare = append(bare, name)
		b.Migrations = append(b.Migrations, Migration{Name: name, SQL: strings.ReplaceAll(string(raw), "\r\n", "\n")})
	}
	b.Sources = map[string][]byte{}
	for _, n := range SourceFiles(bare) {
		if b.Sources[n], err = fs.ReadFile(fsys, n); err != nil {
			return Bundle{}, fmt.Errorf("backup Worker bundle: %w", err)
		}
	}
	return b, nil
}

// Check reports why b is not the bundle of the pinned source at Revision, or nil.
func (b Bundle) Check() error {
	if got := Digest(b.Sources); got != SourceDigest {
		return fmt.Errorf("the backup Worker bundle was built from other sources (%s) than this binary pins (%s)", got, SourceDigest)
	}
	if got := SourceRevision(b.Script); got != Revision {
		return fmt.Errorf("the backup Worker bundle declares revision %d, not %d", got, Revision)
	}
	return nil
}
