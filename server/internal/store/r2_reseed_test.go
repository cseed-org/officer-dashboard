package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reseedLedger(t *testing.T) (*DB, context.Context, time.Time) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	ctx := context.Background()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if err := d.BindR2(ctx, "account/bucket"); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileR2(ctx, now, 0, nil); err != nil {
		t.Fatal(err)
	}
	return d, ctx, now
}

func rollingR2(t *testing.T, d *DB, now time.Time) (a, b int64) {
	t.Helper()
	if err := d.R.QueryRow(`SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, now.AddDate(0, 0, -31).Format("2006-01-02")).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	return a, b
}

// D-20: Cloudflare's measurement raises both windows and storage, and ends
// the re-seed.
func TestR2AnalyticsReseedRaisesCounters(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	d.RequestR2Reseed(ctx, now)
	seed := R2Seed{MonthA: 1200, MonthB: 34000, RollingA: 2500, RollingB: 40000, Bytes: 7_000_000}
	if err := d.ReseedR2(ctx, now, seed); err != nil {
		t.Fatal(err)
	}
	u, _ := d.R2Usage(ctx, now)
	if u.ClassA != 1200 || u.ClassB != 34000 || u.Bytes != 7_000_000 {
		t.Fatal("month and storage not re-seeded", u)
	}
	if a, b := rollingR2(t, d, now); a != 2500 || b != 40000 {
		t.Fatal("rolling window not re-seeded", a, b)
	}
	if s, _ := d.R2Reseed(ctx); s.Pending {
		t.Fatal("measured re-seed left the marker", s)
	}
	if _, err := d.ReserveR2(ctx, now, "get", "", "", 0); err != nil {
		t.Fatal(err)
	}
	// Round 5: the restored inventory looked fresh but predates the restore.
	if _, err := d.ReserveR2(ctx, now, "put", "k", "alice", 10); !errors.Is(err, ErrR2Budget) {
		t.Fatal("measured re-seed reopened uploads before a fresh inventory", err)
	}
	if err := d.ReconcileR2(ctx, now, 7_000_000, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReserveR2(ctx, now, "put", "k", "alice", 10); err != nil {
		t.Fatal("fresh inventory did not reopen uploads", err)
	}
}

// D-20: max-merge per counter and per window; a lower seed changes nothing.
func TestR2ReseedNeverLowersACounter(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	day := now.Format("2006-01-02")
	earlier := now.AddDate(0, 0, -20).Format("2006-01-02")
	d.W.Exec(`INSERT INTO r2_day VALUES(?,300,9000),(?,200,1000)`, day, earlier)
	d.W.Exec(`INSERT INTO r2_month(month,class_a,class_b) VALUES(?,300,9000)`, now.Format("2006-01"))
	d.W.Exec(`UPDATE r2_budget SET bytes=5000`)
	// Month A lower, month B higher; rolling A higher, rolling B lower.
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: 100, MonthB: 9500, RollingA: 700, RollingB: 10, Bytes: 1}); err != nil {
		t.Fatal(err)
	}
	u, _ := d.R2Usage(ctx, now)
	if u.ClassA != 300 || u.ClassB != 9500 || u.Bytes != 5000 {
		t.Fatal("month or bytes merge lowered or missed a counter", u)
	}
	if a, b := rollingR2(t, d, now); a != 700 || b != 10000 {
		t.Fatal("rolling merge lowered or missed a counter", a, b)
	}
	var old int64
	d.R.QueryRow(`SELECT class_a FROM r2_day WHERE day=?`, earlier).Scan(&old)
	if old != 200 {
		t.Fatal("re-seed rewrote an earlier day", old)
	}
	if err := d.ReseedR2(ctx, now, R2Seed{}); err != nil {
		t.Fatal(err)
	}
	if after, _ := d.R2Usage(ctx, now); after != u {
		t.Fatal("zero seed lowered the ledger", after)
	}
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: -1}); err == nil {
		t.Fatal("negative seed accepted")
	}
}

// D-21: without a measurement nothing is assumed. Billable requests stay
// refused, counters unchanged, until the 32nd UTC day after the restore day,
// when every operation spent before the restore has left the rolling window.
func TestR2RestorePausesUntilBillingWindowRolls(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	// Spending the ledger knows of on the restore day, at the 90% ceiling.
	d.W.Exec(`INSERT INTO r2_day VALUES(?,?,0)`, now.Format("2006-01-02"), R2ClassALimit*9/10)
	if err := d.RequestR2Reseed(ctx, now); err != nil {
		t.Fatal(err)
	}
	before, _ := d.R2Usage(ctx, now)
	until := time.Date(2026, 12, 22, 0, 0, 0, 0, time.UTC)
	if s, _ := d.R2Reseed(ctx); !s.Pending || !s.Until.Equal(until) || !s.AlertedAt.IsZero() {
		t.Fatal("pause state", s)
	}
	for _, at := range []time.Time{now, until.Add(-time.Second)} {
		for _, class := range []string{"put", "list", "get"} {
			if _, err := d.ReserveR2(ctx, at, class, "k", "alice", 10); !errors.Is(err, ErrR2Reseeding) || !errors.Is(err, ErrR2Budget) {
				t.Fatal("paused ledger spent", at, class, err)
			}
		}
		if _, err := d.ReserveR2(ctx, at, "delete", "k", "", 0); err != nil {
			t.Fatal("free deletion paused", err)
		}
		if paused, err := d.ExpireR2Reseed(ctx, at); !paused || err != nil {
			t.Fatal("pause ended early", at, paused, err)
		}
	}
	if after, _ := d.R2Usage(ctx, now); after != before {
		t.Fatal("pause assumed usage", before, after)
	}
	// From the cap on, downloads resume (the restore-day ceiling has left the
	// rolling window); uploads still wait for a fresh inventory.
	for _, class := range []string{"list", "get"} {
		if _, err := d.ReserveR2(ctx, until, class, "", "", 0); err != nil {
			t.Fatal("pause outlived the billing window", class, err)
		}
	}
	if paused, err := d.ExpireR2Reseed(ctx, until); paused || err != nil {
		t.Fatal("expired pause kept", paused, err)
	}
	var reconciled int64
	d.R.QueryRow(`SELECT reconciled_at FROM r2_budget`).Scan(&reconciled)
	if s, _ := d.R2Reseed(ctx); s.Pending || reconciled != 0 {
		t.Fatal("expiry kept the marker or the restored inventory", s, reconciled)
	}
	if _, err := d.ReserveR2(ctx, until, "put", "k", "alice", 10); !errors.Is(err, ErrR2Budget) {
		t.Fatal("expired pause reopened uploads before a fresh inventory", err)
	}
}

// D-20: the normal 90% refusal still applies on top of a re-seeded ledger.
func TestR2RefusalHoldsAfterReseed(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	d.RequestR2Reseed(ctx, now)
	edge := R2ClassALimit*9/10 - 1
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: 10, RollingA: edge, RollingB: R2ClassBLimit * 9 / 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReserveR2(ctx, now, "list", "", "", 0); err != nil {
		t.Fatal("request below 90% refused", err)
	}
	if _, err := d.ReserveR2(ctx, now, "list", "", "", 0); !errors.Is(err, ErrR2Budget) {
		t.Fatal("re-seeded rolling window passed 90%", err)
	}
	if _, err := d.ReserveR2(ctx, now, "get", "", "", 0); !errors.Is(err, ErrR2Budget) {
		t.Fatal("re-seeded Class B passed 90%", err)
	}
}

// A ledger with spending but no mirror beside it came from another volume.
func TestR2LedgerWithoutMirrorRequestsReseed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d.BindR2(ctx, "account/bucket")
	if s, _ := d.R2Reseed(ctx); s.Pending {
		t.Fatal("new empty ledger asked for a re-seed")
	}
	d.ReserveR2(ctx, time.Now(), "list", "", "", 0)
	d.Close()
	if err := os.Remove(filepath.Join(dir, R2MirrorName)); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.BindR2(ctx, "account/bucket"); err != nil {
		t.Fatal(err)
	}
	if s, _ := d.R2Reseed(ctx); !s.Pending {
		t.Fatal("moved ledger not marked for re-seed", s)
	}
}
