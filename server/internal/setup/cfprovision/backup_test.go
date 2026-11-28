package cfprovision

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
	"officerdashboard/internal/setup/sinkbundle"
)

const (
	scriptP = acctP + "/workers/scripts/dashboard-backup-sink"
)

func requestIndex(rs []cffake.Request, method, suffix, bodyHas string) int {
	for i, r := range rs {
		if r.Method == method && strings.HasSuffix(r.Path, suffix) && strings.Contains(r.Body, bodyHas) {
			return i
		}
	}
	return -1
}

// The backup hostname is never public without Access: its token, policy and application
// are created and read back before the custom domain is attached.
func TestBackupAccessIsReadBackBeforeTheCustomDomain(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	rs := r.acct.Requests()
	token := requestIndex(rs, http.MethodPost, "/access/service_tokens", "dashboard-backups-host")
	policy := requestIndex(rs, http.MethodPost, "/access/policies", "Backup host token")
	app := requestIndex(rs, http.MethodPost, "/access/apps", "Dashboard backups (service token)")
	domain := requestIndex(rs, http.MethodPut, "/workers/domains", cffake.Backup)
	if token < 0 || policy < token || app < policy || domain < app {
		t.Fatalf("token %d, policy %d, app %d, domain %d", token, policy, app, domain)
	}
	readBack := false
	for _, q := range rs[app+1 : domain] {
		readBack = readBack || (q.Method == http.MethodGet && q.Path == acctP+"/access/apps")
	}
	if !readBack {
		t.Fatal("the custom domain was attached before the backup application was read back")
	}
}

// The bearer leaves the host in exactly one request, the upload that creates the Worker,
// and never reaches output, errors or outputs.
func TestBearerGoesOnlyInTheUploadThatCreatesTheWorker(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var carrying []string
	for _, q := range r.acct.Requests() {
		if strings.Contains(q.Body, testBearer) || strings.Contains(q.Path+q.Query, testBearer) {
			carrying = append(carrying, q.Method+" "+q.Path)
		}
	}
	if !slices.Equal(carrying, []string{"PUT " + scriptP}) {
		t.Fatalf("the bearer went in %v", carrying)
	}
	if strings.Contains(r.out.String(), testBearer) || strings.Contains(fmt.Sprintf("%v %+v", out, out), testBearer) {
		t.Fatal("the bearer was printed")
	}
	// Cloudflare refusing the upload with the bearer in its message: still never printed.
	r2 := newRig(t, cffake.Fresh())
	r2.acct.Inject(cffake.Injection{Method: http.MethodPut, Path: scriptP, Status: 400, Code: 10021, Message: "binding BACKUP_BEARER_SECRET: bad text " + testBearer})
	_, err = r2.apply(context.Background())
	var rej *RejectedError
	if !errors.As(err, &rej) || strings.Contains(errText(err), testBearer) || strings.Contains(r2.out.String(), testBearer) {
		t.Fatalf("err = %v", err)
	}
}

// The upload follows the multipart-upload-metadata page: main_module, the dates and flags
// of wrangler.toml, a D1 binding by database_id (never the deprecated id), the bearer as
// secret_text and the revision as plain_text; the module is the image's bundle.
func TestUploadMetadata(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	ups := r.acct.Uploads()
	if len(ups) != 1 || ups[0].Module != string(r.opt.Bundle.Script) {
		t.Fatalf("uploads %d", len(ups))
	}
	m := ups[0].Metadata
	if m["main_module"] != "worker.js" || m["compatibility_date"] != "2026-07-30" || fmt.Sprint(m["compatibility_flags"]) != "[nodejs_compat]" {
		t.Fatalf("metadata %v", m)
	}
	d1 := byID(r.preview())["d1"].ExistingID
	want := map[string]map[string]any{
		"BACKUPS":              {"type": "d1", "name": "BACKUPS", "database_id": d1},
		"BACKUP_BEARER_SECRET": {"type": "secret_text", "name": "BACKUP_BEARER_SECRET", "text": testBearer},
		"SINK_REVISION":        {"type": "plain_text", "name": "SINK_REVISION", "text": "2"},
	}
	bindings := m["bindings"].([]any)
	if len(bindings) != len(want) {
		t.Fatalf("bindings %v", bindings)
	}
	for _, b := range bindings {
		got := b.(map[string]any)
		if fmt.Sprint(got) != fmt.Sprint(want[fmt.Sprint(got["name"])]) {
			t.Errorf("binding %v, want %v", got, want[fmt.Sprint(got["name"])])
		}
	}
}

// An existing Worker is never uploaded again: an upload replaces its script, bindings and
// secrets.
func TestExistingWorkerIsNeverUploaded(t *testing.T) {
	for name, routes := range map[string]map[string]cffake.Response{"live": cffake.Live(), "complete": cffake.Complete()} {
		r := newRig(t, routes)
		if _, err := r.apply(context.Background()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n := len(r.acct.Uploads()); n != 0 || requestIndex(r.acct.Requests(), http.MethodPut, "/workers/scripts/dashboard-backup-sink", "") >= 0 {
			t.Fatalf("%s: %d uploads", name, n)
		}
	}
}

func setRevisionLabel(r *rig, text string) {
	path := scriptP + "/settings"
	cur, _ := r.acct.Route(path)
	settings := cur.Result.(map[string]any)
	var next []any
	for _, b := range settings["bindings"].([]any) {
		if m := b.(map[string]any); m["type"] == "plain_text" {
			m = map[string]any{"type": "plain_text", "name": m["name"], "text": text}
			b = m
		}
		next = append(next, b)
	}
	settings["bindings"] = next
	r.acct.Set(path, cur)
}

// A Worker older than the image is drift with the upgrade step; setup writes nothing.
func TestOlderWorkerIsDriftWithTheUpgradeStep(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	setRevisionLabel(r, "1")
	a := byID(r.preview())["worker.version"]
	if a.Kind != plan.Drift || !strings.Contains(strings.Join(a.Detail, " "), "never uploads over an existing Worker") || !strings.Contains(strings.Join(a.Detail, " "), "--var SINK_REVISION:2") {
		t.Fatalf("worker.version %s %v", a.Kind, a.Detail)
	}
	n := len(r.acct.Writes())
	if _, err := r.apply(context.Background()); err != nil || len(r.acct.Writes()) != n {
		t.Fatalf("err %v, %d writes", err, len(r.acct.Writes())-n)
	}
}

// A run that stopped between the upload and turning the switches off is completed by
// the next run, which only turns them off; on a Worker setup did not upload they are drift.
func TestWorkerSwitchesLeftOnAreTurnedOffOnlyOnSetupsWorker(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.acct.Inject(cffake.Injection{Method: http.MethodPost, Path: scriptP + "/subdomain", Status: 503})
	if _, err := r.apply(context.Background()); err == nil {
		t.Fatal("the run survived the failed switch-off")
	}
	if a := byID(r.preview())["worker.subdomain"]; a.Kind != plan.Create {
		t.Fatalf("worker.subdomain %s %v", a.Kind, a.Detail)
	}
	n := len(r.acct.Writes())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	ws := r.acct.Writes()[n:]
	if requestIndex(ws, http.MethodPost, "/subdomain", `"enabled":false`) < 0 || len(r.acct.Uploads()) != 1 {
		t.Fatalf("re-run writes %v, %d uploads", ws, len(r.acct.Uploads()))
	}
	if a := byID(r.preview())["worker.subdomain"]; a.Kind != plan.OK {
		t.Fatalf("after: %s", a.Kind)
	}

	c := cffake.Complete()
	c[scriptP+"/subdomain"] = cffake.Response{Result: map[string]any{"enabled": true, "previews_enabled": false}}
	rc := newRig(t, c)
	if _, err := rc.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rc.acct.Writes()) != 0 || byID(rc.preview())["worker.subdomain"].Kind != plan.Drift {
		t.Fatalf("a Wrangler-made Worker's switches were changed: %v", rc.acct.Writes())
	}
}

func setTables(r *rig, uuid string, names ...string) {
	var l []any
	for _, n := range names {
		l = append(l, n)
	}
	r.acct.Set(cffake.D1TablesKey(uuid), cffake.Response{Result: l})
}

func tables(r *rig, uuid string) []string {
	cur, _ := r.acct.Route(cffake.D1TablesKey(uuid))
	var out []string
	for _, n := range cur.Result.([]any) {
		out = append(out, fmt.Sprint(n))
	}
	return out
}

func migrationQueries(r *rig) []cffake.Request {
	var out []cffake.Request
	for _, w := range r.acct.Writes() {
		if strings.HasSuffix(w.Path, "/query") {
			out = append(out, w)
		}
	}
	return out
}

// The live account's backup database lists no tables: the migration is applied there once,
// recorded as Wrangler records it, and never again.
func TestLiveEmptyDatabaseGetsTheMigrationOnce(t *testing.T) {
	r := newRig(t, cffake.Live())
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	if q := migrationQueries(r); len(q) != 1 || !strings.Contains(q[0].Path, "d1-backups") || !strings.Contains(q[0].Body, `INSERT INTO \"d1_migrations\" (name)\nvalues ('0001.sql')`) {
		t.Fatalf("migration queries %v", q)
	}
	for _, want := range []string{"budgets", "chunks", "snapshots", "d1_migrations"} {
		if !slices.Contains(tables(r, "d1-backups"), want) {
			t.Errorf("no table %s after the run", want)
		}
	}
	if a := byID(out.Final)["d1.migrations"]; a.Kind != plan.OK {
		t.Fatalf("d1.migrations %s %v", a.Kind, a.Detail)
	}
	if _, err := r.apply(context.Background()); err != nil || len(migrationQueries(r)) != 1 {
		t.Fatalf("second run: %v, %d migrations", err, len(migrationQueries(r)))
	}
}

// A database that has tables is never altered: ok when they are the Worker's, drift when not.
func TestDatabaseWithTablesIsNeverAltered(t *testing.T) {
	r := newRig(t, cffake.Live())
	setTables(r, "d1-backups", "_cf_KV", "members")
	rev, err := NewEngine("test-token", r.d, r.sink, &r.out, r.opt, WithAPIBase(r.acct.URL), WithReadRetry()).Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a := byID(rev.Actions)["d1.migrations"]; a.Kind != plan.Drift || !strings.Contains(strings.Join(a.Detail, " "), "never alters") {
		t.Fatalf("d1.migrations %s %v", a.Kind, a.Detail)
	}
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q := migrationQueries(r); len(q) != 0 || !slices.Equal(tables(r, "d1-backups"), []string{"_cf_KV", "members"}) {
		t.Fatalf("the database was altered: %v", q)
	}
}

// A table that appears after the plan was made stops the run before the migration.
func TestTableAppearingBeforeTheMigrationStopsIt(t *testing.T) {
	r := newRig(t, cffake.Live())
	r.acct.OnWrite(func(req cffake.Request) {
		if strings.HasSuffix(req.Path, "/access/identity_providers") {
			setTables(r, "d1-backups", "_cf_KV", "someone_elses")
		}
	})
	_, err := r.apply(context.Background())
	var rej *RejectedError
	if !errors.As(err, &rej) || !strings.Contains(err.Error(), "never alters a database that has tables") || len(migrationQueries(r)) != 0 {
		t.Fatalf("err = %v, %d migrations", err, len(migrationQueries(r)))
	}
}

// Every SQL statement setup sends is one of the two read-only listings or the one
// migration, and the migration goes only to a database that had no tables.
func TestEveryQueryIsReadOnlyOrTheMigration(t *testing.T) {
	for name, routes := range map[string]map[string]cffake.Response{"fresh": cffake.Fresh(), "live": cffake.Live(), "complete": cffake.Complete()} {
		r := newRig(t, routes)
		if _, err := r.apply(context.Background()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(r.acct.Queries()) == 0 && name != "fresh" {
			t.Errorf("%s: the tables were never listed", name)
		}
		for _, q := range r.acct.Queries() {
			if !slices.ContainsFunc(sinkbundle.ReadOnlySQL, func(s string) bool { return strings.Contains(q.Body, strings.ReplaceAll(s, `"`, `\"`)) }) {
				t.Errorf("%s: query %s", name, q.Body)
			}
		}
		for _, m := range migrationQueries(r) {
			if !strings.Contains(m.Body, `CREATE TABLE IF NOT EXISTS \"d1_migrations\"`) {
				t.Errorf("%s: non-migration SQL sent as a write: %s", name, m.Body)
			}
		}
		if name == "complete" && len(migrationQueries(r)) != 0 {
			t.Errorf("complete: migrated a database that has its tables")
		}
	}
}

func listOf(n int, item func(i int) any) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = item(i)
	}
	return out
}

// The Free plan's limits are counted before any write.
func TestFreePlanLimitsAreCountedBeforeAnyWrite(t *testing.T) {
	d1 := cffake.Fresh()
	d1[acctP+"/d1/database"] = cffake.Response{Result: listOf(10, func(i int) any {
		return map[string]any{"uuid": fmt.Sprintf("d1-%d", i), "name": fmt.Sprintf("db%d", i)}
	})}
	scripts := cffake.Fresh()
	scripts[acctP+"/workers/scripts"] = cffake.Response{Result: listOf(100, func(i int) any { return map[string]any{"id": fmt.Sprintf("w%d", i)} })}
	for name, c := range map[string]struct {
		routes map[string]cffake.Response
		want   string
	}{"d1": {d1, "Workers Free plan's limit of 10"}, "workers": {scripts, "Workers Free plan's limit of 100"}} {
		r := newRig(t, c.routes)
		_, err := r.apply(context.Background())
		var blocked *BlockedError
		if !errors.As(err, &blocked) || !strings.Contains(err.Error(), c.want) || len(r.acct.Writes()) != 0 {
			t.Errorf("%s: err = %v, %d writes", name, err, len(r.acct.Writes()))
		}
	}
}

// Without the bundle or the host's bearer, nothing is written.
func TestBackupPrerequisitesBlockBeforeAnyWrite(t *testing.T) {
	cases := map[string]struct {
		routes map[string]cffake.Response
		setup  func(r *rig)
		want   string
	}{
		"no bundle, fresh": {cffake.Fresh(), func(r *rig) { r.opt.Bundle = nil }, "carries no backup Worker bundle"},
		"no bundle, live":  {cffake.Live(), func(r *rig) { r.opt.Bundle = nil }, "carries no backup Worker bundle"},
		"no bearer":        {cffake.Fresh(), func(r *rig) { r.sink = &MemorySink{} }, "secrets/backup/bearer is missing: run `scripts/setup host --yes` first"},
		"short bearer": {cffake.Fresh(), func(r *rig) {
			_ = r.sink.Store(context.Background(), Secret{Name: SecretBackupBearer, Value: "short-bearer"})
		}, "the Worker refuses every request with a bearer under 32 bytes"},
	}
	for name, c := range cases {
		r := newRig(t, c.routes)
		c.setup(r)
		_, err := r.apply(context.Background())
		var blocked *BlockedError
		if !errors.As(err, &blocked) || !strings.Contains(err.Error(), c.want) || len(r.acct.Writes()) != 0 {
			t.Errorf("%s: err = %v, %d writes", name, err, len(r.acct.Writes()))
		}
	}
}

// A Worker that appears after the plan was made is not uploaded over.
func TestWorkerAppearingBeforeTheUploadIsNotOverwritten(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.acct.OnWrite(func(req cffake.Request) {
		if strings.HasSuffix(req.Path, "/query") {
			cur, _ := r.acct.Route(acctP + "/workers/scripts")
			r.acct.Set(acctP+"/workers/scripts", cffake.Response{Result: append(cur.Result.([]any), map[string]any{"id": "dashboard-backup-sink"})})
			// Someone deployed it with Wrangler meanwhile.
			r.acct.Set(scriptP+"/settings", cffake.Response{Result: map[string]any{"bindings": []any{}}})
			r.acct.Set(scriptP+"/secrets", cffake.Response{Result: []any{}})
			r.acct.Set(scriptP+"/subdomain", cffake.Response{Result: map[string]any{"enabled": true}})
		}
	})
	_, err := r.apply(context.Background())
	var rej *RejectedError
	if !errors.As(err, &rej) || !strings.Contains(err.Error(), "never uploads over a Worker") || len(r.acct.Uploads()) != 0 {
		t.Fatalf("err = %v, %d uploads", err, len(r.acct.Uploads()))
	}
}

// The custom domain waits for the backup application: one that is drift blocks it.
func TestCustomDomainNeedsAnOKBackupApp(t *testing.T) {
	routes := cffake.Live()
	routes[acctP+"/workers/domains"] = cffake.Response{Result: []any{}}
	apps := routes[acctP+"/access/apps"].Result.([]any)
	backup := apps[0].(map[string]any)
	backup["policies"] = append(backup["policies"].([]any), map[string]any{"id": "pol-open", "name": "open", "decision": "allow",
		"include": []any{map[string]any{"everyone": map[string]any{}}}, "exclude": []any{}, "require": []any{}, "precedence": 2})
	r := newRig(t, routes)
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "attach the custom domain backups.example.org to Worker dashboard-backup-sink needs Access application") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v, %d writes", err, len(r.acct.Writes()))
	}
}

// A CNAME on the backup hostname blocks the run with the step; no record is ever removed.
func TestRecordOnTheBackupHostnameBlocks(t *testing.T) {
	routes := cffake.Fresh()
	routes["/zones/"+cffake.ZoneID+"/dns_records"] = cffake.Response{Result: []any{map[string]any{"id": "dns-old", "name": cffake.Backup, "type": "CNAME", "content": "old.example.net"}}}
	r := newRig(t, routes)
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "setup never removes a record") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v, %d writes", err, len(r.acct.Writes()))
	}
}
