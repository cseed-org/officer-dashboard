package cfprovision

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cfprovision/internal/cfwrite"
	"officerdashboard/internal/setup/plan"
	"officerdashboard/internal/setup/sinkbundle"
)

// The backup Worker's pieces: its D1 database and tables, the Worker itself with its
// workers.dev switches, and its custom domain. Nothing here has run against real
// Cloudflare; each body follows the reference page quoted beside it.

const (
	tablesSQL  = sinkbundle.TablesSQL
	appliedSQL = sinkbundle.AppliedSQL
)

// wranglerMigrationsTable is Wrangler 4.168.0's own table, verbatim from
// getCreateMigrationsTableQuery in packages/wrangler/src/d1/migrations/helpers.ts
// (tag wrangler@4.168.0), with DEFAULT_MIGRATION_TABLE "d1_migrations" from
// packages/wrangler/src/d1/constants.ts. Wrangler records each applied migration with
// buildMigrationQuery: the file's SQL followed by
// INSERT INTO "d1_migrations" (name) values ('<name>'), where the name is the path
// relative to migrations_dir. Writing the same record means `wrangler d1 migrations
// apply` later sees 0001.sql as applied.
const wranglerMigrationsTable = `CREATE TABLE IF NOT EXISTS "d1_migrations"(
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT UNIQUE,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL
);`

// migrationSQL is the one query that creates the backup tables: Wrangler's table, then
// each migration with its record, as Wrangler's apply builds them.
func migrationSQL(b *sinkbundle.Bundle) string {
	var sb strings.Builder
	sb.WriteString(wranglerMigrationsTable)
	for _, m := range b.Migrations {
		fmt.Fprintf(&sb, "\n%s\nINSERT INTO \"d1_migrations\" (name)\nvalues ('%s');", strings.TrimRight(m.SQL, "\n"), strings.ReplaceAll(m.Name, "'", "''"))
	}
	return sb.String()
}

// queryResult is one QueryResult of POST …/d1/database/{id}/query: "array of QueryResult
// { meta, results, success }"
// (https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/query/).
type queryResult struct {
	Results []map[string]any `json:"results"`
	Success *bool            `json:"success"`
}

// query runs one read-only statement (one of ReadOnlySQL) and returns the "name" column.
// It is a POST (the only way D1 runs SQL) but changes nothing, so it is not a write:
// Outputs.Writes does not list it.
func (e *Engine) query(ctx context.Context, dbID, sql string) ([]string, error) {
	if !slices.Contains(sinkbundle.ReadOnlySQL, sql) {
		return nil, fmt.Errorf("refusing to send %q as a read-only query", sql)
	}
	var res []queryResult
	if err := e.write.Post(ctx, e.accountPath("/d1/database/"+url.PathEscape(dbID)+"/query"), map[string]any{"sql": sql}, &res); err != nil {
		return nil, err
	}
	// An answer in a shape setup does not know must never read as "no tables": that would
	// let the migration run on a database that has them.
	if len(res) == 0 {
		return nil, errors.New("Cloudflare answered the D1 query with no result")
	}
	var names []string
	for _, r := range res {
		if r.Success != nil && !*r.Success {
			return nil, errors.New("Cloudflare answered the D1 query with success false")
		}
		if r.Results == nil {
			return nil, errors.New("Cloudflare's D1 query answer has no results list")
		}
		for _, row := range r.Results {
			if n, ok := row["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	return names, nil
}

// d1Tables reads the backup database's tables (once per discovery round; see discover).
func (e *Engine) d1Tables(ctx context.Context) plan.Tables {
	if !e.o.D1.Found || e.o.D1.Unknown != "" {
		return plan.Tables{}
	}
	if t, ok := e.tables[e.o.D1.ID]; ok {
		return t
	}
	names, err := e.query(ctx, e.o.D1.ID, tablesSQL)
	t := plan.Tables{Read: err == nil, Names: names}
	if err != nil {
		perm := ""
		if cfapi.IsForbidden(err) {
			perm = " (the token needs D1 Edit)"
		}
		t = plan.Tables{Unknown: fmt.Sprintf("setup could not list the database's tables%s: %v", perm, err)}
	}
	e.tables[e.o.D1.ID] = t
	return t
}

func userTables(names []string) []string {
	var out []string
	for _, n := range names {
		if !strings.HasPrefix(n, "sqlite_") && !strings.HasPrefix(n, "_cf_") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// createD1: POST /accounts/{account_id}/d1/database {"name"}
// https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/create/
// (name: "D1 database name."; the result's ID is uuid: "D1 database identifier (UUID).")
func (e *Engine) createD1(ctx context.Context, s Step) (string, error) {
	var r struct {
		UUID string `json:"uuid"`
	}
	if err := e.send(ctx, s, "POST", e.accountPath("/d1/database"),
		"https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/create/",
		"D1 Edit", map[string]any{"name": e.d.D1Database}, &r); err != nil {
		return "", err
	}
	if r.UUID == "" {
		return "", &ShapeError{Step: s.Title, Missing: "the database uuid", Recovery: "Run setup again: it finds the database by its name"}
	}
	e.created["d1:"+r.UUID] = true
	return r.UUID, e.verify(ctx, s, r.UUID)
}

// migrate applies the Worker's migrations to a database with no tables, in one query:
// POST /accounts/{account_id}/d1/database/{database_id}/query {"sql"}
// https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/query/
// ("Supports multiple statements, joined by semicolons, which will be executed as a
// batch."). The tables are listed again right before, and the run stops if any is there:
// setup never alters a database that has tables.
func (e *Engine) migrate(ctx context.Context, s Step) error {
	if e.opt.Bundle == nil {
		return &RejectedError{Step: s.Title, Method: "POST", Path: e.accountPath("/d1/database"), Cause: "this build carries no backup Worker bundle"}
	}
	id := e.o.D1.ID
	path := e.accountPath("/d1/database/" + url.PathEscape(id) + "/query")
	names, err := e.query(ctx, id, tablesSQL)
	if err != nil {
		return e.readErr("D1 Edit", "POST", path, err)
	}
	if have := userTables(names); len(have) > 0 {
		return &RejectedError{Step: s.Title, Method: "POST", Path: path,
			Cause: fmt.Sprintf("database %s now has tables (%s); setup never alters a database that has tables", id, strings.Join(have, ", "))}
	}
	if err := e.send(ctx, s, "POST", path, "https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/query/",
		"D1 Edit", map[string]any{"sql": migrationSQL(e.opt.Bundle)}, nil); err != nil {
		return err
	}
	delete(e.tables, id)
	// Read back: every table, and Wrangler's record of every migration.
	names, err = e.query(ctx, id, tablesSQL)
	if err != nil {
		return fmt.Errorf("%s: the write succeeded, but listing the tables failed: %w", s.Title, err)
	}
	applied, err := e.query(ctx, id, appliedSQL)
	if err != nil {
		return fmt.Errorf("%s: the write succeeded, but reading d1_migrations failed: %w", s.Title, err)
	}
	var missing []string
	for _, t := range append(append([]string(nil), e.d.D1Tables...), "d1_migrations") {
		if !slices.Contains(names, t) {
			missing = append(missing, "table "+t)
		}
	}
	for _, m := range e.opt.Bundle.Migrations {
		if !slices.Contains(applied, m.Name) {
			missing = append(missing, "d1_migrations row "+m.Name)
		}
	}
	if len(missing) > 0 {
		return &VerifyError{Step: s.Title, ResourceID: id, Expected: "the Worker's tables and Wrangler's record of its migrations",
			Got: []string{"tables: " + strings.Join(names, ", "), "applied: " + strings.Join(applied, ", "), "missing: " + strings.Join(missing, ", ")}}
	}
	return e.verify(ctx, s, "")
}

// uploadWorker creates the backup Worker, then turns off its workers.dev URL and preview
// URLs at once, and reads both back together.
//
// PUT /accounts/{account_id}/workers/scripts/{script_name}, multipart/form-data
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/update/
// with the metadata of https://developers.cloudflare.com/workers/configuration/multipart-upload-metadata/:
// main_module ("The part name that contains the module entry point"), compatibility_date
// and compatibility_flags from wrangler.toml, and bindings: {"type":"d1","name",
// "database_id"} ("id … has been renamed to database_id"), {"type":"secret_text","name",
// "text"} and {"type":"plain_text","name","text"}. The module part is
// application/javascript+module.
//
// An upload replaces a script whole, so it is sent only for a Worker that does not exist:
// the plan says so, and the scripts list is read again right before. This is the only
// request that ever carries the bearer.
func (e *Engine) uploadWorker(ctx context.Context, s Step) (string, error) {
	b := e.opt.Bundle
	script := e.accountPath("/workers/scripts/" + url.PathEscape(e.d.Worker.Script))
	if b == nil {
		return "", &RejectedError{Step: s.Title, Method: "PUT", Path: script, Cause: "this build carries no backup Worker bundle"}
	}
	if exists, err := e.scriptExists(ctx); err != nil {
		return "", err
	} else if exists {
		return "", &RejectedError{Step: s.Title, Method: "PUT", Path: script,
			Cause: "a Worker named " + e.d.Worker.Script + " now exists; setup never uploads over a Worker, which would replace its script, bindings and secrets"}
	}
	bearer, err := e.sink.Load(ctx, SecretBackupBearer)
	if err != nil {
		return "", fmt.Errorf("%s: cannot read the backup bearer from secrets/backup/bearer (%v); run `scripts/setup host --yes`, which generates it", s.Title, e.out.scrub(err.Error()))
	}
	e.out.add(bearer)
	meta := map[string]any{
		"main_module":         sinkbundle.MainModule,
		"compatibility_date":  sinkbundle.CompatibilityDate,
		"compatibility_flags": sinkbundle.CompatibilityFlags,
		"bindings": []any{
			map[string]any{"type": "d1", "name": e.d.Worker.D1Binding, "database_id": e.o.D1.ID},
			map[string]any{"type": "secret_text", "name": e.d.Worker.Secret, "text": bearer},
			map[string]any{"type": "plain_text", "name": plan.WorkerRevisionBinding, "text": strconv.Itoa(e.d.Worker.Revision)},
		},
	}
	e.outputs.Writes = append(e.outputs.Writes, Write{Method: "PUT", Path: script,
		Ref: "https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/update/"})
	fmt.Fprintf(e.out, "  PUT %s (multipart: %s and the metadata)\n", script, sinkbundle.MainModule)
	var r struct {
		ID string `json:"id"`
	}
	err = e.write.PutModule(ctx, script, meta, []cfwrite.Module{{Name: sinkbundle.MainModule, ContentType: "application/javascript+module", Body: b.Script}}, &r)
	if err != nil {
		return "", e.writeErr(s, "PUT", script, "Workers Scripts Edit", err)
	}
	e.outputs.UploadedWorker = true
	// Turned off before anything else, so the workers.dev URL is up only for these two
	// requests; the Worker refuses every request without the bearer meanwhile.
	if err := e.subdomainOff(ctx, s); err != nil {
		return e.d.Worker.Script, err
	}
	return e.d.Worker.Script, e.verify(ctx, s, "", "worker.version", "worker.subdomain")
}

func (e *Engine) scriptExists(ctx context.Context) (bool, error) {
	path := e.accountPath("/workers/scripts")
	type script struct {
		ID string `json:"id"`
	}
	scripts, err := cfapi.ListAll[script](ctx, e.read, path, nil)
	if err != nil {
		return false, e.readErr("Workers Scripts Read", "GET", path, err)
	}
	for _, sc := range scripts {
		if sc.ID == e.d.Worker.Script {
			return true, nil
		}
	}
	return false, nil
}

// subdomainOff: POST /accounts/{account_id}/workers/scripts/{script_name}/subdomain
// {"enabled": false, "previews_enabled": false}
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/subdomain/methods/create/
// ("enabled: Whether the Worker should be available on the workers.dev subdomain.";
// "previews_enabled: Whether the Worker's Preview URLs should be available on the
// workers.dev subdomain.")
func (e *Engine) subdomainOff(ctx context.Context, s Step) error {
	return e.send(ctx, s, "POST", e.accountPath("/workers/scripts/"+url.PathEscape(e.d.Worker.Script)+"/subdomain"),
		"https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/subdomain/methods/create/",
		"Workers Scripts Edit", map[string]any{"enabled": false, "previews_enabled": false}, nil)
}

// workerSubdomain finishes a Worker setup uploaded whose switches an interrupted run left on.
func (e *Engine) workerSubdomain(ctx context.Context, s Step) error {
	if err := e.subdomainOff(ctx, s); err != nil {
		return err
	}
	return e.verify(ctx, s, "")
}

// workerDomain: PUT /accounts/{account_id}/workers/domains {"hostname", "service", "zone_id"}
// https://developers.cloudflare.com/api/resources/workers/subresources/domains/methods/update/
// ("Attaches a domain that routes traffic to a Worker."; hostname and service are
// required; zone_id "ID of the zone containing the domain hostname."). "Cloudflare will
// create DNS records and issue necessary certificates on your behalf"
// (https://developers.cloudflare.com/workers/configuration/routing/custom-domains/). It is
// sent only after the backup hostname's Access application reads back ok, so the
// hostname is never public without Access.
func (e *Engine) workerDomain(ctx context.Context, s Step) (string, error) {
	if a := e.o.Apps[plan.AppBackups]; !a.Found || a.Unknown != "" {
		return "", &RejectedError{Step: s.Title, Method: "PUT", Path: e.accountPath("/workers/domains"), Cause: "the backup hostname's Access application is not there"}
	}
	if len(e.o.BackupDNS.Records) > 0 || e.o.BackupDNS.Unknown != "" {
		return "", &RejectedError{Step: s.Title, Method: "PUT", Path: e.accountPath("/workers/domains"), Cause: "the backup hostname now has a DNS record, or its records could not be read"}
	}
	body := map[string]any{"hostname": e.d.Worker.Hostname, "service": e.d.Worker.Script, "zone_id": e.o.Zone.ID}
	var r struct {
		ID string `json:"id"`
	}
	if err := e.send(ctx, s, "PUT", e.accountPath("/workers/domains"),
		"https://developers.cloudflare.com/api/resources/workers/subresources/domains/methods/update/",
		"Workers Scripts Edit", body, &r); err != nil {
		return "", err
	}
	return r.ID, e.verify(ctx, s, r.ID)
}

// backupCounts reads what the Free-plan limits cap, before any write: every D1 database
// in the account (GET /accounts/{account_id}/d1/database,
// https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/list/)
// and every Worker (GET /accounts/{account_id}/workers/scripts,
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/list/).
func (e *Engine) backupCounts(ctx context.Context, f *Facts, actions []plan.Action) {
	by := map[string]plan.Kind{}
	for _, a := range actions {
		by[a.ID] = a.Kind
	}
	if by["d1"] == plan.Create {
		type db struct{ UUID string }
		all, err := cfapi.ListAll[db](ctx, e.read, e.accountPath("/d1/database"), nil)
		if err != nil {
			f.D1CountUnknown = err.Error()
		}
		f.D1Count = len(all)
	}
	if by["worker"] == plan.Create {
		type script struct{ ID string }
		all, err := cfapi.ListAll[script](ctx, e.read, e.accountPath("/workers/scripts"), nil)
		if err != nil {
			f.ScriptCountUnknown = err.Error()
		}
		f.ScriptCount = len(all)
	}
}

// bearerFacts reads the host's backup bearer: whether it is there and fit to upload.
func (e *Engine) bearerFacts(ctx context.Context, f *Facts) error {
	v, err := e.sink.Load(ctx, SecretBackupBearer)
	switch {
	case errors.Is(err, ErrNotKept):
		return nil
	case err != nil:
		return fmt.Errorf("cannot check the host's secret files: %w", err)
	}
	e.out.add(v)
	f.BearerKept = true
	// The Worker refuses every request when its bearer is under 32 bytes (src/index.ts).
	if len(v) < 32 {
		f.BearerProblem = fmt.Sprintf("secrets/backup/bearer holds %d bytes; the Worker refuses every request with a bearer under 32 bytes. Generate a new one with `scripts/setup host --yes --rotate backup-bearer`", len(v))
	}
	return nil
}
