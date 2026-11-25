package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestR2GuardThresholds(t *testing.T) {
	for _, metric := range []string{"storage", "class_a", "class_b"} {
		t.Run(metric, func(t *testing.T) {
			d, err := Open(t.TempDir() + "/db")
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			ctx := context.Background()
			now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
			if err := d.BindR2(ctx, "test/bucket"); err != nil {
				t.Fatal(err)
			}
			d.ReconcileR2(ctx, now, 0, nil)
			d.ReserveR2(ctx, now, "check", "", "", 0)
			column, table, limit := "bytes", "r2_budget", R2StorageLimit
			class, size := "put", int64(1)
			if metric == "class_a" {
				column, table, limit = "class_a", "r2_month", R2ClassALimit
			}
			if metric == "class_b" {
				column, table, limit = "class_b", "r2_month", R2ClassBLimit
				class, size = "get", 0
			}
			if _, err := d.W.Exec("UPDATE "+table+" SET "+column+"=?", limit*7/10-1); err != nil {
				t.Fatal(err)
			}
			alert, err := d.ReserveR2(ctx, now, class, "key1", "alice", size)
			if err != nil || !alert {
				t.Fatalf("70%% alert: %v %v", alert, err)
			}
			d.AckR2Alert(ctx, now)
			alert, err = d.ReserveR2(ctx, now, "check", "", "", 0)
			if err != nil || alert {
				t.Fatal("duplicate alert")
			}
			d.W.Exec("UPDATE "+table+" SET "+column+"=?", limit*9/10)
			_, err = d.ReserveR2(ctx, now, class, "key2", "alice", size)
			if !errors.Is(err, ErrR2Budget) {
				t.Fatalf("90%% refuse: %v", err)
			}
		})
	}
}

func TestR2GuardAtomicQuotaAndStaleInventory(t *testing.T) {
	d, err := Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	d.BindR2(ctx, "test")
	d.ReconcileR2(ctx, now, 0, nil)
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.ReserveR2(ctx, now, "put", fmt.Sprint(i), "alice", 10)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrR2Budget) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 20 {
		t.Fatal(successes.Load())
	}
	u, _ := d.R2Usage(ctx, now)
	if u.Bytes != 200 || u.ClassA != 20 {
		t.Fatal(u)
	}
	if _, err := d.ReserveR2(ctx, now.Add(26*time.Hour), "put", "new", "bob", 10); !errors.Is(err, ErrR2Budget) {
		t.Fatal("stale inventory allowed", err)
	}
	// An unlisted reservation inside the pending window may still land.
	d.ReconcileR2(ctx, now, 1, nil)
	u, _ = d.R2Usage(ctx, now)
	if u.Bytes != 201 {
		t.Fatal("reconciliation refunded in-flight writes", u)
	}
	var key string
	if err := d.R.QueryRow(`SELECT key FROM r2_objects LIMIT 1`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	d.DeletedR2(ctx, key)
	d.DeletedR2(ctx, key)
	u, _ = d.R2Usage(ctx, now)
	if u.Bytes != 191 {
		t.Fatal("delete released twice", u)
	}
	next := now.AddDate(0, 1, 0)
	d.ReconcileR2(ctx, next, 190, r2Keys(t, d))
	if _, err := d.ReserveR2(ctx, next, "get", "", "", 0); err != nil {
		t.Fatal(err)
	}
	u, _ = d.R2Usage(ctx, next)
	if u.ClassA != 0 || u.ClassB != 1 || u.Bytes != 190 {
		t.Fatal("month reset lost bytes", u)
	}
	if err := d.BindR2(ctx, "different"); err == nil {
		t.Fatal("changed bucket accepted")
	}
}

func TestR2RollingBillingWindowAndRestoreReseed(t *testing.T) {
	d, err := Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 30, 23, 59, 45, 0, time.UTC)
	d.BindR2(ctx, "test")
	d.W.Exec(`INSERT INTO r2_day VALUES('2026-11-30',900000,9000000)`)
	next := now.Add(time.Minute)
	for _, class := range []string{"list", "get"} {
		if _, err := d.ReserveR2(ctx, next, class, "", "", 0); !errors.Is(err, ErrR2Budget) {
			t.Fatal("calendar rollover granted budget", class, err)
		}
	}
	if _, err := d.ReserveR2(ctx, now, "delete", "", "", 0); err != nil {
		t.Fatal("free deletion blocked at month edge", err)
	}
	later := now.AddDate(0, 0, 33)
	if _, err := d.ReserveR2(ctx, later, "get", "", "", 0); err != nil {
		t.Fatal("rolling budget did not recover", err)
	}
	// D-20: a restore marker holds billable requests only until the re-seed
	// has raised the ledger, never for days.
	if err := d.RequestR2Reseed(ctx, later); err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"list", "get"} {
		if _, err := d.ReserveR2(ctx, later, class, "", "", 0); !errors.Is(err, ErrR2Reseeding) {
			t.Fatal("restored ledger spent before its re-seed", class, err)
		}
	}
	if err := d.ReseedR2(ctx, later, R2FallbackSeed(), false); err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"list", "get"} {
		if _, err := d.ReserveR2(ctx, later, class, "", "", 0); err != nil {
			t.Fatal("restore still locks R2 after the re-seed", class, err)
		}
	}
}

func TestR2RollbackPreservesCandidateSpending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	live := dir + "/live.db"
	checkpoint := dir + "/before.db"
	d, err := Open(live)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	d.BindR2(ctx, "account/bucket")
	d.ReconcileR2(ctx, now, 0, nil)
	d.ReserveR2(ctx, now, "put", "before", "alice", 10)
	if _, err := d.W.Exec(`VACUUM INTO ?`, checkpoint); err != nil {
		t.Fatal(err)
	}
	d.ReserveR2(ctx, now, "put", "candidate", "alice", 20)
	d.ReserveR2(ctx, now, "get", "", "", 0)
	d.Close()
	if err := MergeR2BeforeRollback(ctx, live, checkpoint); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	u, err := restored.R2Usage(ctx, now)
	if err != nil || u.Bytes != 30 || u.ClassA != 2 || u.ClassB != 1 {
		t.Fatal("rollback lost spending", u, err)
	}
	var a, b int64
	if err := restored.R.QueryRow(`SELECT class_a,class_b FROM r2_day`).Scan(&a, &b); err != nil || a != 2 || b != 1 {
		t.Fatal("rollback lost rolling counters", a, b, err)
	}
	if _, err := restored.ReserveR2(ctx, now, "put", "after", "alice", 1); !errors.Is(err, ErrR2Budget) {
		t.Fatal("rollback inventory must be stale", err)
	}
}

func TestR2AlertClaimsThrottleConcurrentFailures(t *testing.T) {
	d, err := Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	d.BindR2(ctx, "test")
	d.ReconcileR2(ctx, now, 7_000_000_000, nil)
	var claims atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			alert, err := d.ReserveR2(ctx, now, "check", "", "", 0)
			if err != nil {
				t.Error(err)
			}
			if alert {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatal("concurrent requests claimed duplicate warnings", claims.Load())
	}
	now = now.Add(16 * time.Minute)
	if alert, err := d.ReserveR2(ctx, now, "check", "", "", 0); err != nil || !alert {
		t.Fatal("pending warning not retried", alert, err)
	}
	d.AckR2Alert(ctx, now)
	now = now.Add(16 * time.Minute)
	d.W.Exec(`UPDATE r2_month SET class_a=700000`)
	if alert, err := d.ReserveR2(ctx, now, "check", "", "", 0); err != nil || !alert {
		t.Fatal("new metric threshold hidden by earlier warning", alert, err)
	}
}

func r2Keys(t *testing.T, d *DB) map[string]bool {
	t.Helper()
	rows, err := d.R.Query(`SELECT key FROM r2_objects`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	keys := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		keys[key] = true
	}
	return keys
}

// N1 at the ledger: an unlisted reservation survives inventories inside the
// pending window, then is released, and its migration journal row is reopened.
func TestR2InventoryReleasesStaleReservations(t *testing.T) {
	d, err := Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	d.BindR2(ctx, "test")
	d.ReconcileR2(ctx, now, 0, nil)
	d.ReserveR2(ctx, now, "put", "files/landed", "alice", 10)
	d.R2MigrationKey(ctx, "source", "files/lost")
	d.R2MigrationUploaded(ctx, "source")
	d.ReserveR2(ctx, now, "put", "files/lost", "alice", 20)
	listed := map[string]bool{"files/landed": true, "files/untracked": true}
	d.ReconcileR2(ctx, now.Add(30*time.Minute), 15, listed)
	if u, _ := d.R2Usage(ctx, now); u.Bytes != 35 {
		t.Fatal("pending reservation released too early", u)
	}
	later := now.Add(2 * time.Hour)
	d.ReconcileR2(ctx, later, 15, listed)
	if u, _ := d.R2Usage(ctx, now); u.Bytes != 15 {
		t.Fatal("stale reservation kept or listed bytes lost", u)
	}
	if keys := r2Keys(t, d); len(keys) != 1 || !keys["files/landed"] {
		t.Fatal(keys)
	}
	var uploaded bool
	if err := d.R.QueryRow(`SELECT uploaded FROM r2_migrations WHERE source='source'`).Scan(&uploaded); err != nil || uploaded {
		t.Fatal("released migration key still marked uploaded", uploaded, err)
	}
	// A retry of the same key reserves its bytes again.
	if _, err := d.ReserveR2(ctx, later, "put", "files/lost", "alice", 20); err != nil {
		t.Fatal(err)
	}
	if u, _ := d.R2Usage(ctx, now); u.Bytes != 35 {
		t.Fatal(u)
	}
}

// Review #2: spend spread across the end of one calendar month still counts
// after the month rolls over, until those days leave the 32-day window.
func TestR2CalendarRolloverGrantsNoSecondAllowance(t *testing.T) {
	d, err := Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	d.BindR2(ctx, "test")
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 14; i++ {
		d.W.Exec(`INSERT INTO r2_day VALUES(?,64285,0)`, start.AddDate(0, 0, i).Format("2006-01-02"))
	}
	d.W.Exec(`INSERT INTO r2_month(month,class_a) VALUES('2026-11',899990)`)
	december := time.Date(2026, 12, 1, 0, 0, 30, 0, time.UTC)
	for i := 0; i < 10; i++ {
		if _, err := d.ReserveR2(ctx, december, "list", "", "", 0); err != nil {
			t.Fatal("remaining headroom refused", i, err)
		}
	}
	if _, err := d.ReserveR2(ctx, december, "list", "", "", 0); !errors.Is(err, ErrR2Budget) {
		t.Fatal("calendar rollover granted a second allowance", err)
	}
	if _, err := d.ReserveR2(ctx, time.Date(2026, 12, 18, 23, 0, 0, 0, time.UTC), "list", "", "", 0); !errors.Is(err, ErrR2Budget) {
		t.Fatal("first spend day left the window too early", err)
	}
	if _, err := d.ReserveR2(ctx, time.Date(2026, 12, 19, 0, 0, 0, 0, time.UTC), "list", "", "", 0); err != nil {
		t.Fatal("window did not recover once Nov 17 aged out", err)
	}
}

// N4 follow-up: a 90% refusal alerts once even after the 70% warning was
// acknowledged, moves no counters, and does not repeat after acknowledgement.
func TestR2RefusalAlertsOnceWithoutSpending(t *testing.T) {
	d, err := Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	d.BindR2(ctx, "test")
	d.ReconcileR2(ctx, now, 0, nil)
	d.W.Exec(`INSERT INTO r2_month(month,class_a,alerted) VALUES('2026-11',900000,?)`, R2AlertClassAWarn)
	alert, err := d.ReserveR2(ctx, now, "list", "", "", 0)
	if !errors.Is(err, ErrR2Budget) || !alert {
		t.Fatal("90% refusal did not claim its alert", alert, err)
	}
	if mask, _ := d.R2AlertClaim(ctx, now); mask != R2AlertClassARefused {
		t.Fatal("claim mask", mask)
	}
	if u, _ := d.R2Usage(ctx, now); u.ClassA != 900000 {
		t.Fatal("refusal spent budget", u)
	}
	var days int
	d.R.QueryRow(`SELECT count(*) FROM r2_day`).Scan(&days)
	if days != 0 {
		t.Fatal("refusal recorded rolling usage")
	}
	d.AckR2Alert(ctx, now)
	for _, at := range []time.Time{now.Add(time.Minute), now.Add(time.Hour)} {
		if alert, err := d.ReserveR2(ctx, at, "list", "", "", 0); !errors.Is(err, ErrR2Budget) || alert {
			t.Fatal("acknowledged refusal alert repeated", alert, err)
		}
	}
}
