package setup

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/hostfiles"
)

const acctPath = "/accounts/" + cffake.AccountID

// bearer is the backup bearer `setup host` generated in the rig's deploy directory.
func (r *applyRig) bearer() string { return strings.TrimSpace(r.file("secrets/backup/bearer")) }

func (r *applyRig) requireBearerNeverPrinted(res applyResult) {
	r.t.Helper()
	if b := r.bearer(); len(b) < 32 || strings.Contains(res.stdout+res.stderr, b) {
		r.t.Fatal("the backup bearer was printed (or is missing)")
	}
}

// Fresh account → apply → dry run: every backup piece is there and ok, the backup token's
// secret is where the backup sidecar reads it, and backup.env names the sink.
func TestApplyCreatesTheBackupPiecesAndTheDryRunAfterIsOK(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	r.requireBearerNeverPrinted(res)
	r.requireSecretFile("secrets/backup/access-client-secret", hostfiles.AppIDs)
	r.requireSecretFile("secrets/backup/access-client-id", hostfiles.AppIDs)
	if !r.isSecretFromCloudflare(r.file("secrets/backup/access-client-secret")) || !strings.HasSuffix(r.file("secrets/backup/access-client-id"), ".access") {
		t.Fatal("the backup token's files do not hold what Cloudflare returned")
	}
	if v, _ := env(t, filepath.Join(r.dir, "backup.env")).Get("BACKUP_SINK_URL"); v != "https://"+cffake.Backup {
		t.Fatalf("BACKUP_SINK_URL = %q", v)
	}
	if !strings.Contains(res.stdout, "Backups stay off until BACKUP_AGE_RECIPIENT is set") {
		t.Error("the run does not say backups stay off without the age recipient")
	}
	if v, _ := env(t, filepath.Join(r.dir, "backup.env")).Get("BACKUP_AGE_RECIPIENT"); v != "age1..." {
		t.Errorf("BACKUP_AGE_RECIPIENT was written: %q", v)
	}
	dry := r.run("--dry-run")
	if dry.code != 0 {
		t.Fatalf("dry run: exit %d", dry.code)
	}
	for _, want := range []string{
		"ok          Service token                        dashboard-backups-host",
		"ok          Access application                   Dashboard backups (service token)",
		"ok          D1 database                          dashboard-backups",
		"ok          Worker                               dashboard-backup-sink",
		"ok          Worker version                       revision 2",
		"ok          workers.dev and preview URLs         off",
		"ok          Worker custom domain                 backups.example.org",
	} {
		if !strings.Contains(dry.stdout, want) {
			t.Errorf("dry run lacks %q:\n%s", want, dry.stdout)
		}
	}
	// The engine, which can list the tables, finds them ok on the next apply.
	again := r.run()
	if again.code != 0 || !strings.Contains(again.stdout, "ok          D1 migrations                        dashboard-backups") {
		t.Fatalf("second apply: exit %d\n%s", again.code, again.stdout)
	}
}

// The live-shaped account (Worker, custom domain and backup app present, and a backup
// database whose file size is that of the migrated schema): the Worker is not uploaded
// again and the database gets no migration (review F-4 of #129).
func TestLiveAccountKeepsItsWorkerAndDatabase(t *testing.T) {
	r := newApplyRig(t, cffake.Live())
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if n := len(r.acct.Uploads()); n != 0 {
		t.Fatalf("%d uploads over the live Worker", n)
	}
	if n := r.creates("/d1/database/d1-backups/query"); n != 0 || strings.Contains(res.stdout, "create the backup tables") {
		t.Fatalf("%d migration queries on the migrated database\n%s", n, res.stdout)
	}
	if !strings.Contains(res.stdout, "ok          D1 migrations                        dashboard-backups") {
		t.Errorf("the migrated database is not ok:\n%s", res.stdout)
	}
	for _, w := range r.acct.Writes() {
		if strings.Contains(w.Path, "/workers/") || strings.Contains(w.Path, "/d1/") {
			t.Errorf("touched the live backup pieces: %s %s", w.Method, w.Path)
		}
	}
	// The Worker was not uploaded by this run, so its secret stays unknowable (mutant B18).
	if !strings.Contains(res.stdout, "unknowable: Worker secret BACKUP_BEARER_SECRET") {
		t.Errorf("the Wrangler-made Worker's secret line is hidden:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "unknowable: Worker version revision 2") {
		t.Errorf("the Wrangler-made Worker's revision is not reported unknowable:\n%s", res.stdout)
	}
}

// A backup database with no tables at all gets the migration once, through the command.
func TestEmptyBackupDatabaseIsMigratedOnce(t *testing.T) {
	r := newApplyRig(t, cffake.LiveEmptyD1())
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "create the backup tables in dashboard-backups") || r.creates("/d1/database/d1-backups/query") != 1 || len(r.acct.Uploads()) != 0 {
		t.Fatalf("migration queries %d, uploads %d\n%s", r.creates("/d1/database/d1-backups/query"), len(r.acct.Uploads()), res.stdout)
	}
	if again := r.run(); again.code != 0 || r.creates("/d1/database/d1-backups/query") != 1 {
		t.Fatalf("second run: exit %d, %d migrations", again.code, r.creates("/d1/database/d1-backups/query"))
	}
}

// A run interrupted after each write (the process dies with the write stored), then run
// again through the command: no second database, Worker, application or token, the
// bearer untouched, and each token secret either kept or reported for rotation.
func TestBackupApplyInterruptedAfterEachWriteThenRerun(t *testing.T) {
	full := newApplyRig(t, cffake.Fresh())
	if res := full.run(append([]string{"--yes"}, groupArgs...)...); res.code != 0 {
		t.Fatalf("full run: exit %d\n%s", res.code, res.stderr)
	}
	total := len(full.acct.Writes())
	for k := 1; k <= total; k++ {
		t.Run(fmt.Sprintf("after write %d", k), func(t *testing.T) {
			r := newApplyRig(t, cffake.Fresh())
			bearer := r.bearer()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r.ctx = ctx
			n := 0
			var crashed cffake.Request
			r.acct.OnWrite(func(req cffake.Request) {
				if n++; n == k {
					crashed = req
					cancel()
				}
			})
			if res := r.run(append([]string{"--yes"}, groupArgs...)...); res.code == 0 {
				t.Fatalf("the run survived a crash after %s %s", crashed.Method, crashed.Path)
			}
			r.acct.OnWrite(nil)
			r.ctx = context.Background()
			// The cancel races with the fake's answer: the crashed run may still have got the
			// backup token's secret, and then it must have kept it.
			_, keptErr := os.Lstat(filepath.Join(r.dir, "secrets/backup/access-client-secret"))
			res := r.run(append([]string{"--yes"}, groupArgs...)...)
			if res.code != 0 {
				t.Fatalf("re-run after %s %s: exit %d\n%s\n%s", crashed.Method, crashed.Path, res.code, res.stdout, res.stderr)
			}
			r.requireBearerNeverPrinted(res)
			if r.bearer() != bearer {
				t.Fatal("the bearer file changed")
			}
			for _, c := range []struct{ suffix, name string }{
				{"/d1/database", "dashboard-backups"}, {"/access/service_tokens", "dashboard-backups-host"},
				{"/access/service_tokens", "officer-dashboard services"}, {"/access/apps", "Dashboard backups (service token)"},
				{"/access/policies", "Backup host token"},
			} {
				if got := r.createsNamed(c.suffix, c.name); got != 1 {
					t.Errorf("%s %q created %d times", c.suffix, c.name, got)
				}
			}
			if ups := len(r.acct.Uploads()); ups != 1 {
				t.Errorf("%d Worker uploads", ups)
			}
			for _, q := range r.acct.Requests() {
				if q.Method != http.MethodGet && q.Method != http.MethodPost && q.Method != http.MethodPut {
					t.Errorf("sent %s %s", q.Method, q.Path)
				}
			}
			lostBackupSecret := strings.HasSuffix(crashed.Path, "/access/service_tokens") && strings.Contains(crashed.Body, "dashboard-backups-host") && keptErr != nil
			_, err := os.Lstat(filepath.Join(r.dir, "secrets/backup/access-client-secret"))
			switch {
			case lostBackupSecret && (err == nil || !strings.Contains(res.stdout, "Service Tokens → dashboard-backups-host → Rotate")):
				t.Errorf("the lost backup token secret is not reported for rotation:\n%s", res.stdout)
			case !lostBackupSecret && (err != nil || !r.isSecretFromCloudflare(r.file("secrets/backup/access-client-secret"))):
				t.Errorf("the backup token's secret was lost after %s %s", crashed.Method, crashed.Path)
			}
		})
	}
}

// A CNAME on the backup hostname blocks the run, with the step, before any write.
func TestBackupHostnameCNAMEBlocksTheRun(t *testing.T) {
	routes := cffake.Fresh()
	routes["/zones/"+cffake.ZoneID+"/dns_records"] = cffake.Response{Result: []any{
		map[string]any{"id": "dns-old", "name": cffake.Backup, "type": "CNAME", "content": "old-host.example.net", "proxied": true}}}
	r := newApplyRig(t, routes)
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 1 || r.writes() != 0 {
		t.Fatalf("exit %d, %d writes\n%s", res.code, r.writes(), res.stdout)
	}
	for _, want := range []string{"backups.example.org already has a CNAME record to old-host.example.net [dns-old]", "setup never removes a record", "--backup-hostname"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, res.stdout)
		}
	}
}

// A binary built without the bundle (any build but the release image's) creates nothing.
func TestApplyWithoutTheBundleWritesNothing(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	r.bundle = nil
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 1 || r.writes() != 0 || !strings.Contains(res.stdout, "carries no backup Worker bundle") {
		t.Fatalf("exit %d, %d writes\n%s", res.code, r.writes(), res.stdout)
	}
}

// The bearer is generated by `setup host` only: without it the run stops and points there.
func TestApplyWithoutTheBearerPointsAtTheHostPhase(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	if err := os.Remove(filepath.Join(r.dir, "secrets/backup/bearer")); err != nil {
		t.Fatal(err)
	}
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 1 || r.writes() != 0 || !strings.Contains(res.stdout, "secrets/backup/bearer is missing: run `scripts/setup host --yes` first") {
		t.Fatalf("exit %d, %d writes\n%s", res.code, r.writes(), res.stdout)
	}
	if _, err := os.Lstat(filepath.Join(r.dir, "secrets/backup/bearer")); err == nil {
		t.Fatal("apply made a bearer")
	}
}

// An existing backup.env value is kept under the adopt rule, and reported.
func TestBackupEnvAdoptRule(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	// The template's example value (https://backups.example.org) counts as unset under the
	// adopt rule, so the admin's own value is a different one.
	path := filepath.Join(r.dir, "backup.env")
	raw := strings.Replace(r.file("backup.env"), "BACKUP_SINK_URL=https://"+cffake.Backup, "BACKUP_SINK_URL=https://store.example.org", 1)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	res := r.run(append([]string{"--yes", "--backup-hostname", "vault.example.org"}, groupArgs...)...)
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if v, _ := env(t, path).Get("BACKUP_SINK_URL"); v != "https://store.example.org" {
		t.Fatalf("BACKUP_SINK_URL was replaced: %q", v)
	}
	if !regexp.MustCompile(`conflict +backup\.env BACKUP_SINK_URL`).MatchString(res.stdout) || !strings.Contains(res.stdout, `setup would write "https://vault.example.org"; kept`) {
		t.Fatalf("no conflict line:\n%s", res.stdout)
	}
}

// A kept backup token secret without its client ID file is reported until it is there,
// with the owner the backup sidecar needs.
func TestMissingBackupClientIDIsReported(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	if res := r.run(append([]string{"--yes"}, groupArgs...)...); res.code != 0 {
		t.Fatalf("exit %d", res.code)
	}
	if err := os.Remove(filepath.Join(r.dir, "secrets/backup/access-client-id")); err != nil {
		t.Fatal(err)
	}
	res := r.run()
	if res.code != 1 || !strings.Contains(res.stdout, "missing: secrets/backup/access-client-id. The backup token's client secret is kept") ||
		!strings.Contains(res.stdout, "chown 10001:10001 secrets/backup/access-client-id") {
		t.Fatalf("exit %d\n%s", res.code, res.stdout)
	}
}

// The template's example value counts as unset under the adopt rule, so it is replaced
// (mutant B22: planning without the template would keep it as a conflict).
func TestBackupEnvReplacesTheTemplateExample(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	res := r.run(append([]string{"--yes", "--backup-hostname", "vault.example.org"}, groupArgs...)...)
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if v, _ := env(t, filepath.Join(r.dir, "backup.env")).Get("BACKUP_SINK_URL"); v != "https://vault.example.org" {
		t.Fatalf("BACKUP_SINK_URL = %q\n%s", v, res.stdout)
	}
}
