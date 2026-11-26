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

// reseedAfterLag marks a restore R2ReseedLag before now and applies a reading.
func reseedAfterLag(t *testing.T, d *DB, ctx context.Context, now time.Time, measured R2Seed) R2ReseedResult {
	t.Helper()
	if err := d.RequestR2Reseed(ctx, now.Add(-R2ReseedLag)); err != nil {
		t.Fatal(err)
	}
	result, err := d.ReseedR2(ctx, now, measured)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Round 6: twice Cloudflare's reading is merged into both windows and
// storage, ending the pause; uploads still wait for a fresh inventory.
func TestR2AnalyticsReseedRaisesCounters(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	measured := R2Seed{MonthA: 600, MonthB: 17000, RollingA: 1250, RollingB: 20000, Bytes: 3_500_000}
	if r := reseedAfterLag(t, d, ctx, now, measured); r != R2ReseedUnlocked {
		t.Fatal("reading below the guard did not unlock", r)
	}
	u, _ := d.R2Usage(ctx, now)
	if u.ClassA != 1200 || u.ClassB != 34000 || u.Bytes != 7_000_000 {
		t.Fatal("month and storage not seeded with twice the reading", u)
	}
	if a, b := rollingR2(t, d, now); a != 2500 || b != 40000 {
		t.Fatal("rolling window not seeded with twice the reading", a, b)
	}
	if s, _ := d.R2Reseed(ctx); s.Pending {
		t.Fatal("re-seed left the marker", s)
	}
	if _, err := d.ReserveR2(ctx, now, "get", "", "", 0); err != nil {
		t.Fatal(err)
	}
	// Round 5: the restored inventory looked fresh but predates the restore.
	if _, err := d.ReserveR2(ctx, now, "put", "k", "alice", 10); !errors.Is(err, ErrR2Budget) {
		t.Fatal("re-seed reopened uploads before a fresh inventory", err)
	}
	if err := d.ReconcileR2(ctx, now, 7_000_000, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReserveR2(ctx, now, "put", "k", "alice", 10); err != nil {
		t.Fatal("fresh inventory did not reopen uploads", err)
	}
}

// Round 6: a reading taken less than 6 hours after the restore is "not yet":
// nothing is merged and the pause holds.
func TestR2AnalyticsReadInsideLagKeepsPause(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	d.RequestR2Reseed(ctx, now)
	before, _ := d.R2Usage(ctx, now)
	early := now.Add(R2ReseedLag - time.Second)
	if r, err := d.ReseedR2(ctx, early, R2Seed{MonthA: 5}); r != R2ReseedTooEarly || err != nil {
		t.Fatal("early reading accepted", r, err)
	}
	if after, _ := d.R2Usage(ctx, early); after != before {
		t.Fatal("early reading merged", after)
	}
	if s, _ := d.R2Reseed(ctx); !s.Pending || !s.ReadyAt().Equal(now.Add(R2ReseedLag)) {
		t.Fatal("early reading ended the pause", s)
	}
	if _, err := d.ReserveR2(ctx, early, "get", "", "", 0); !errors.Is(err, ErrR2Reseeding) {
		t.Fatal("early reading reopened downloads", err)
	}
	if r, err := d.ReseedR2(ctx, now.Add(R2ReseedLag), R2Seed{MonthA: 5}); r != R2ReseedUnlocked || err != nil {
		t.Fatal("reading after the lag window rejected", r, err)
	}
}

// Max-merge per counter and per window; a lower doubled reading changes nothing.
func TestR2ReseedNeverLowersACounter(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	day := now.Format("2006-01-02")
	earlier := now.AddDate(0, 0, -20).Format("2006-01-02")
	d.W.Exec(`INSERT INTO r2_day VALUES(?,300,9000),(?,200,1000)`, day, earlier)
	d.W.Exec(`INSERT INTO r2_month(month,class_a,class_b) VALUES(?,300,9000)`, now.Format("2006-01"))
	d.W.Exec(`UPDATE r2_budget SET bytes=5000`)
	// Doubled: month A lower (200 < 300), month B higher (19000 > 9000);
	// rolling A higher (1400 > 500), rolling B lower (20 < 10000); bytes lower.
	if r := reseedAfterLag(t, d, ctx, now, R2Seed{MonthA: 100, MonthB: 9500, RollingA: 700, RollingB: 10, Bytes: 1}); r != R2ReseedUnlocked {
		t.Fatal(r)
	}
	u, _ := d.R2Usage(ctx, now)
	if u.ClassA != 300 || u.ClassB != 19000 || u.Bytes != 5000 {
		t.Fatal("month or bytes merge lowered or missed a counter", u)
	}
	if a, b := rollingR2(t, d, now); a != 1400 || b != 10000 {
		t.Fatal("rolling merge lowered or missed a counter", a, b)
	}
	var old int64
	d.R.QueryRow(`SELECT class_a FROM r2_day WHERE day=?`, earlier).Scan(&old)
	if old != 200 {
		t.Fatal("re-seed rewrote an earlier day", old)
	}
	reseedAfterLag(t, d, ctx, now, R2Seed{})
	if after, _ := d.R2Usage(ctx, now); after != u {
		t.Fatal("zero reading lowered the ledger", after)
	}
	if _, err := d.ReseedR2(ctx, now, R2Seed{MonthA: -1}); err == nil {
		t.Fatal("negative reading accepted")
	}
}

// Round 6: if twice the reading reaches the 90% line on any counter, R2 stays
// paused until the 32-day cap, and no later reading can end it sooner.
func TestR2DoubledReadingAtGuardStaysPausedUntilExpiry(t *testing.T) {
	for name, measured := range map[string]R2Seed{
		"month-a":   {MonthA: R2ClassALimit * 9 / 20},
		"rolling-a": {RollingA: R2ClassALimit * 9 / 20},
		"month-b":   {MonthB: R2ClassBLimit * 9 / 20},
		"rolling-b": {RollingB: R2ClassBLimit * 9 / 20},
		"bytes":     {Bytes: R2StorageLimit * 9 / 20},
	} {
		t.Run(name, func(t *testing.T) {
			d, ctx, now := reseedLedger(t)
			if r := reseedAfterLag(t, d, ctx, now, measured); r != R2ReseedOverGuard {
				t.Fatal("doubled reading at 90% unlocked", r)
			}
			s, _ := d.R2Reseed(ctx)
			if !s.Pending || !s.OverGuard.Equal(now) || !s.AlertedAt.IsZero() {
				t.Fatal("over-guard state", s)
			}
			if r, _ := d.ReseedR2(ctx, now.Add(time.Hour), R2Seed{}); r != R2ReseedOverGuard {
				t.Fatal("a later low reading ended an over-guard pause", r)
			}
			for _, at := range []time.Time{now.Add(time.Hour), s.Until.Add(-time.Second)} {
				if _, err := d.ReserveR2(ctx, at, "get", "", "", 0); !errors.Is(err, ErrR2Reseeding) {
					t.Fatal("over-guard pause served a download", at, err)
				}
			}
			if paused, err := d.ExpireR2Reseed(ctx, s.Until); paused || err != nil {
				t.Fatal("over-guard pause outlived the cap", paused, err)
			}
		})
	}
	// Just below the line on every counter still unlocks.
	d, ctx, now := reseedLedger(t)
	below := R2Seed{MonthA: R2ClassALimit*9/20 - 1, RollingA: R2ClassALimit*9/20 - 1, MonthB: R2ClassBLimit*9/20 - 1, RollingB: R2ClassBLimit*9/20 - 1, Bytes: R2StorageLimit*9/20 - 1}
	if r := reseedAfterLag(t, d, ctx, now, below); r != R2ReseedUnlocked {
		t.Fatal("reading below the line kept the pause", r)
	}
}

// Round 6: once a doubled reading reached the line, a later reading cannot end
// the pause, even after a month rollover resets the counter that reached it.
func TestR2OverGuardSurvivesMonthRollover(t *testing.T) {
	d, ctx, _ := reseedLedger(t)
	late := time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)
	if r := reseedAfterLag(t, d, ctx, late, R2Seed{MonthA: R2ClassALimit * 9 / 20}); r != R2ReseedOverGuard {
		t.Fatal(r)
	}
	next := time.Date(2026, 12, 1, 1, 0, 0, 0, time.UTC)
	if r, err := d.ReseedR2(ctx, next, R2Seed{}); r != R2ReseedOverGuard || err != nil {
		t.Fatal("new month's reading ended an over-guard pause", r, err)
	}
	if _, err := d.ReserveR2(ctx, next, "get", "", "", 0); !errors.Is(err, ErrR2Reseeding) {
		t.Fatal("over-guard pause served a download after the rollover", err)
	}
}

// Round 6, the re-review probe: before the restore the bucket had really spent
// 899,999 Class A (or 8,999,999 Class B), the restored ledger knew less, and
// analytics undercounted. The probe then admitted physical operation 1,000,001
// A or 10,000,001 B. Now the probe's readings keep R2 paused, and even a
// reading of only half the real usage admits nothing past the free tier.
func TestR2ReseedProbeCasesRefused(t *testing.T) {
	for _, c := range []struct {
		name, class      string
		actual, measured int64
		limit            int64
		seed             func(int64) R2Seed
	}{
		{"class-a-probe", "list", 899_999, 700_000, R2ClassALimit, func(v int64) R2Seed { return R2Seed{MonthA: v, RollingA: v} }},
		{"class-a-half", "list", 899_999, 449_999, R2ClassALimit, func(v int64) R2Seed { return R2Seed{MonthA: v, RollingA: v} }},
		{"class-b-probe", "get", 8_999_999, 7_000_000, R2ClassBLimit, func(v int64) R2Seed { return R2Seed{MonthB: v, RollingB: v} }},
		{"class-b-half", "get", 8_999_999, 4_499_999, R2ClassBLimit, func(v int64) R2Seed { return R2Seed{MonthB: v, RollingB: v} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ctx, now := reseedLedger(t)
			d.W.Exec(`INSERT INTO r2_day VALUES(?,100000,1000000)`, now.Format("2006-01-02")) // the stale restored ledger
			reseedAfterLag(t, d, ctx, now, c.seed(c.measured))
			admitted := int64(0)
			for ; admitted < c.limit; admitted++ {
				if _, err := d.ReserveR2(ctx, now, c.class, "", "", 0); err != nil {
					if !errors.Is(err, ErrR2Budget) {
						t.Fatal(err)
					}
					break
				}
			}
			if physical := c.actual + admitted; physical > c.limit {
				t.Fatal("admitted past the free tier", physical)
			}
			if c.measured*2 >= c.limit*9/10 && admitted != 0 {
				t.Fatal("probe reading reopened R2", admitted)
			}
		})
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
	if s, _ := d.R2Reseed(ctx); !s.Pending || !s.Until.Equal(until) || !s.AlertedAt.IsZero() || !s.OverGuard.IsZero() {
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

// The normal 90% refusal still applies on top of a re-seeded ledger: a
// doubled reading just below the line admits only the remaining headroom.
func TestR2RefusalHoldsAfterReseed(t *testing.T) {
	d, ctx, now := reseedLedger(t)
	if r := reseedAfterLag(t, d, ctx, now, R2Seed{MonthA: 10, RollingA: R2ClassALimit*9/20 - 1, RollingB: R2ClassBLimit*9/20 - 1}); r != R2ReseedUnlocked {
		t.Fatal(r)
	}
	for _, class := range []string{"list", "get"} {
		for i := 0; i < 2; i++ { // seeded at line-2: two requests fit
			if _, err := d.ReserveR2(ctx, now, class, "", "", 0); err != nil {
				t.Fatal("request below 90% refused", class, err)
			}
		}
		if _, err := d.ReserveR2(ctx, now, class, "", "", 0); !errors.Is(err, ErrR2Budget) {
			t.Fatal("re-seeded rolling window passed 90%", class, err)
		}
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
