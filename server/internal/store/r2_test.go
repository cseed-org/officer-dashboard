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
			d.ReconcileR2(ctx, now, 0)
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
	d.ReconcileR2(ctx, now, 0)
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
	d.ReconcileR2(ctx, now, 1)
	u, _ = d.R2Usage(ctx, now)
	if u.Bytes != 200 {
		t.Fatal("reconciliation refunded uncertain writes")
	}
	var key string
	if err := d.R.QueryRow(`SELECT key FROM r2_objects LIMIT 1`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	d.DeletedR2(ctx, key)
	d.DeletedR2(ctx, key)
	u, _ = d.R2Usage(ctx, now)
	if u.Bytes != 190 {
		t.Fatal("delete released twice", u)
	}
	next := now.AddDate(0, 1, 0)
	d.ReconcileR2(ctx, next, 0)
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

func TestR2RollingBillingWindowAndRestoreLock(t *testing.T) {
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
	d.W.Exec(`INSERT INTO r2_restore_lock VALUES(1,?)`, later.Add(32*24*time.Hour).Format("2006-01-02T15:04:05Z"))
	for _, class := range []string{"list", "get"} {
		if _, err := d.ReserveR2(ctx, later, class, "", "", 0); !errors.Is(err, ErrR2Budget) {
			t.Fatal("restored ledger granted budget", class, err)
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
	d.ReconcileR2(ctx, now, 0)
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
	d.ReconcileR2(ctx, now, 7_000_000_000)
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
