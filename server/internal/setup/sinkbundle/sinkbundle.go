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

// SetupUploadBinding marks a Worker that setup itself uploaded: a plain-text binding no
// upgrade step tells anyone to set, so only setup's own upload carries it. Setup turns off
// the workers.dev switches only on a Worker that carries it.
const SetupUploadBinding = "DASHBOARD_SETUP_UPLOAD"

// The only SQL setup sends to a D1 database besides the migration itself. Both only read.
const (
	// SchemaSQL lists a database's tables and triggers. The D1 list's num_tables is
	// "Deprecated … This count is no longer accurate and should not be relied upon"
	// (https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/list/),
	// so whether a database is empty, or fully migrated, is decided by this query.
	SchemaSQL = "SELECT type, name FROM sqlite_master WHERE type IN ('table', 'trigger') ORDER BY type, name"
	// AppliedSQL lists the migrations Wrangler's d1_migrations table records.
	AppliedSQL = `SELECT name FROM "d1_migrations" ORDER BY id`
)

// ReadOnlySQL are the statements setup sends without changing a database.
var ReadOnlySQL = []string{SchemaSQL, AppliedSQL}

// The schema the migrations make (TestSchemaMatchesTheMigrations checks each list against
// the migration files): the tables, the triggers (reserve is the database-side budget),
// and the names Wrangler records in d1_migrations.
var (
	Tables         = []string{"budgets", "chunks", "snapshots"}
	Triggers       = []string{"reserve"}
	MigrationNames = []string{"0001.sql"}
)

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
	// Sources are the files the bundle was built from (ReadSources), so the image build
	// can check them against SourceDigest.
	Sources map[string][]byte
}

// ReadSources reads the files Digest covers from a backup-sink directory (or a bundle
// laid out like one): every file under src/ (esbuild bundles whatever index.ts imports),
// wrangler.toml, and every migrations/*.sql. Development dependencies (the lockfile) are
// left out, so a test-tool update needs no new revision.
func ReadSources(fsys fs.FS) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(fsys, "src", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		out[p], err = fs.ReadFile(fsys, p)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("backup Worker sources: %w", err)
	}
	if _, ok := out["src/index.ts"]; !ok {
		return nil, errors.New("backup Worker sources: no src/index.ts")
	}
	if out["wrangler.toml"], err = fs.ReadFile(fsys, "wrangler.toml"); err != nil {
		return nil, fmt.Errorf("backup Worker sources: %w", err)
	}
	ms, err := fs.Glob(fsys, MigrationsDir+"/*.sql")
	if err != nil || len(ms) == 0 {
		return nil, errors.New("backup Worker sources: no migrations")
	}
	for _, m := range ms {
		if out[m], err = fs.ReadFile(fsys, m); err != nil {
			return nil, fmt.Errorf("backup Worker sources: %w", err)
		}
	}
	return out, nil
}

// Digest hashes files (by their path in the backup-sink directory) with line endings
// normalized to LF, so a Windows checkout and a Linux build agree. Each file is framed by
// its name and length.
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

// Load reads a bundle laid out as the Dockerfile's sink stage writes it: worker.js plus
// the sources as ReadSources finds them.
func Load(fsys fs.FS) (Bundle, error) {
	var b Bundle
	var err error
	if b.Script, err = fs.ReadFile(fsys, MainModule); err != nil {
		return Bundle{}, fmt.Errorf("backup Worker bundle: %w", err)
	}
	if b.Sources, err = ReadSources(fsys); err != nil {
		return Bundle{}, err
	}
	var names []string
	for n := range b.Sources {
		if strings.HasPrefix(n, MigrationsDir+"/") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b.Migrations = append(b.Migrations, Migration{Name: path.Base(n), SQL: strings.ReplaceAll(string(b.Sources[n]), "\r\n", "\n")})
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
