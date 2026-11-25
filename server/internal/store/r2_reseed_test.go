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
	if err := d.ReseedR2(ctx, now, seed, true); err != nil {
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
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: 100, MonthB: 9500, RollingA: 700, RollingB: 10, Bytes: 1}, true); err != nil {
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
	if err := d.ReseedR2(ctx, now, R2Seed{}, false); err != nil {
		t.Fatal(err)
	}
	if after, _ := d.R2Usage(ctx, now); after != u {
		t.Fatal("zero seed lowered the ledger", after)
	}
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: -1}, true); err == nil {
		t.Fatal("negative seed accepted")
	}
}

// D-20: without a measurement, half of each allowance is assumed used; the
// marker stays (a later measurement can still raise it) but requests resume.
func TestR2FallbackReseedAssumesHalfAndKeepsWorking(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	d.RequestR2Reseed(ctx, now)
	if err := d.ReseedR2(ctx, now, R2FallbackSeed(), false); err != nil {
		t.Fatal(err)
	}
	u, _ := d.R2Usage(ctx, now)
	if u.ClassA != R2ClassALimit/2 || u.ClassB != R2ClassBLimit/2 || u.Bytes != R2StorageLimit/2 {
		t.Fatal("fallback did not assume 50%", u)
	}
	if a, b := rollingR2(t, d, now); a != R2ClassALimit/2 || b != R2ClassBLimit/2 {
		t.Fatal("fallback did not cover the rolling window", a, b)
	}
	if s, _ := d.R2Reseed(ctx); !s.Pending || !s.FallbackApplied || s.Alerted {
		t.Fatal("fallback state", s)
	}
	for _, class := range []string{"list", "get"} {
		if _, err := d.ReserveR2(ctx, now, class, "", "", 0); err != nil {
			t.Fatal("fallback re-seed blocked R2", class, err)
		}
	}
	// A measurement that is higher still raises the ledger and ends the re-seed.
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: R2ClassALimit * 6 / 10}, true); err != nil {
		t.Fatal(err)
	}
	if u, _ := d.R2Usage(ctx, now); u.ClassA != R2ClassALimit*6/10 {
		t.Fatal(u)
	}
	if s, _ := d.R2Reseed(ctx); s.Pending {
		t.Fatal(s)
	}
}

// D-20: the normal 90% refusal still applies on top of a re-seeded ledger.
func TestR2RefusalHoldsAfterReseed(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	d.RequestR2Reseed(ctx, now)
	edge := R2ClassALimit*9/10 - 1
	if err := d.ReseedR2(ctx, now, R2Seed{MonthA: 10, RollingA: edge, RollingB: R2ClassBLimit * 9 / 10}, true); err != nil {
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
	// The fallback alone leaves 40 points of headroom before refusing.
	d2, ctx2, now2 := reseedLedger(t)
	d2.ReseedR2(ctx2, now2, R2FallbackSeed(), false)
	d2.W.Exec(`UPDATE r2_day SET class_a=class_a+? WHERE day=?`, R2ClassALimit*4/10-1, now2.Format("2006-01-02"))
	if _, err := d2.ReserveR2(ctx2, now2, "list", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.ReserveR2(ctx2, now2, "list", "", "", 0); !errors.Is(err, ErrR2Budget) {
		t.Fatal("fallback ledger passed 90%", err)
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
	if s, _ := d.R2Reseed(ctx); !s.Pending || s.FallbackApplied {
		t.Fatal("moved ledger not marked for re-seed", s)
	}
}
