//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/store"
)

// The previous image predates R2; the candidate is R2-aware. Digests only
// select behaviour inside the fake compose below.
var (
	preR2Image = "example/dashboard@sha256:" + strings.Repeat("a", 64)
	r2Image    = "example/dashboard@sha256:" + strings.Repeat("b", 64)
)

const candidateClassA, candidateClassB = 5, 3

// Cross-review major: scripts/update rolls back with the PREVIOUS image's
// dashboard-backup. When that image predates R2, its rollback is a plain copy
// of the pre-R2 checkpoint, which erased the candidate's R2 ledger and granted
// a fresh allowance on retry while Cloudflare still counted the spent
// requests. This drives the real updater through: pre-R2 previous image,
// candidate spends, backup sidecar fails, rollback via the pre-R2 helper,
// automatic retry. Spending from both attempts must survive.
func TestUpdaterRollbackThroughPreR2HelperKeepsR2Spending(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("python3 is required for the updater regression test")
		}
		t.Skip("python3 not installed")
	}
	updater, err := filepath.Abs("../../../scripts/update")
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	data, deploy, bin := filepath.Join(root, "data"), filepath.Join(root, "deploy"), filepath.Join(root, "bin")
	for _, dir := range []string{data, deploy, bin} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	preR2Database(t, filepath.Join(data, "dashboard.db"))
	for name, body := range map[string]string{
		filepath.Join(deploy, "prod-image.env"): "DASHBOARD_IMAGE=" + preR2Image + "\n",
		filepath.Join(deploy, ".env"):           "",
		filepath.Join(data, "fail-sidecar"):     "",
	} {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexec \"$FAKE_COMPOSE_BIN\" -test.run='^TestFakeComposeHelper$' -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	poll := func() (string, error) {
		cmd := exec.Command(python, updater, "--deploy-dir", deploy, "--image", r2Image)
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_COMPOSE_BIN="+self, "FAKE_COMPOSE_DIR="+data)
		out, err := cmd.CombinedOutput()
		t.Logf("updater: %s", out)
		return string(out), err
	}

	out, err := poll()
	if err == nil || !strings.Contains(out, "previous image and pre-update database restored") {
		t.Fatal("sidecar failure should roll the update back", err)
	}
	if _, err := os.Stat(filepath.Join(data, store.R2MirrorName)); err != nil {
		t.Fatal("precondition: the candidate should have spent R2 operations", err)
	}
	if prod, _ := os.ReadFile(filepath.Join(deploy, "prod-image.env")); !strings.Contains(string(prod), preR2Image) {
		t.Fatal("rollback did not restore the previous pin", string(prod))
	}
	if ledgerTables(t, filepath.Join(data, "dashboard.db")) != 0 {
		t.Fatal("precondition: the pre-R2 helper should restore a database without the R2 ledger")
	}
	if out, err := poll(); err != nil || !strings.Contains(out, "Production update succeeded") {
		t.Fatal("automatic retry failed", err)
	}
	db, err := store.Open(filepath.Join(data, "dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, err := db.R2Usage(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if u.ClassA != 2*candidateClassA || u.ClassB != 2*candidateClassB {
		t.Fatalf("rollback re-granted spent R2 operations: %+v, want A=%d B=%d", u, 2*candidateClassA, 2*candidateClassB)
	}
	var rolling int64
	if err := db.R.QueryRow(`SELECT coalesce(sum(class_a),0) FROM r2_day`).Scan(&rolling); err != nil || rolling != 2*candidateClassA {
		t.Fatal("rolling window lost the first attempt", rolling, err)
	}
}

// TestFakeComposeHelper stands in for `docker compose` when the updater test
// runs scripts/update. It executes real dashboard-backup and store code.
func TestFakeComposeHelper(t *testing.T) {
	data := os.Getenv("FAKE_COMPOSE_DIR")
	if data == "" {
		t.Skip("fake compose for the updater regression test")
	}
	i := slices.Index(os.Args, "--")
	args := os.Args[i+1:]
	if len(args) < 7 || args[0] != "compose" {
		fakeFail("unexpected docker invocation: %v", args)
	}
	args = args[7:] // compose --env-file X --env-file Y -f Z
	image := os.Getenv("DASHBOARD_IMAGE")
	db := filepath.Join(data, "dashboard.db")
	os.Setenv("DB_PATH", db)
	last := args[len(args)-1]
	switch {
	case args[0] == "pull", args[0] == "stop", args[0] == "exec":
	case args[0] == "run" && slices.Contains(args, "/app/dashboard-backup"):
		switch last {
		case "backup":
			fmt.Print("snapshot-test")
		case "rollback":
			if image == preR2Image {
				// The pre-R2 helper's rollback: replace the database, no ledger merge.
				for _, suffix := range []string{"-wal", "-shm"} {
					os.Remove(db + suffix)
				}
				fakeCheck(copyDatabase(filepath.Join(data, ".update-before.db"), db))
			} else {
				fakeCheck(run([]string{"rollback"}))
			}
		default:
			fakeCheck(run([]string{last}))
		}
	case args[0] == "run" && last == "migrate":
		opened, err := store.Open(filepath.Join(data, ".migration-check.db"))
		fakeCheck(err)
		opened.Close()
	case args[0] == "up" && image == r2Image && slices.Contains(args, "dashboard"):
		// The candidate app starts, binds its ledger and spends R2 operations.
		opened, err := store.Open(db)
		fakeCheck(err)
		ctx := context.Background()
		fakeCheck(opened.BindR2(ctx, strings.Repeat("a", 32)+"/receipts"))
		for n := 0; n < candidateClassA+candidateClassB; n++ {
			class := "list"
			if n >= candidateClassA {
				class = "get"
			}
			_, err := opened.ReserveR2(ctx, time.Now(), class, "", "", 0)
			fakeCheck(err)
		}
		opened.Close()
	case args[0] == "up" && image == r2Image && slices.Equal(args[len(args)-1:], []string{"backup"}):
		if err := os.Remove(filepath.Join(data, "fail-sidecar")); err == nil {
			fakeFail("backup sidecar failed to start")
		}
	case args[0] == "up":
		// The pre-R2 previous image never touches R2.
	default:
		fakeFail("unexpected compose arguments: %v", args)
	}
	os.Exit(0)
}

func fakeCheck(err error) {
	if err != nil {
		fakeFail("%v", err)
	}
}

func fakeFail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// preR2Database builds what an image before migration 0037 leaves behind.
func preR2Database(t *testing.T, path string) {
	t.Helper()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"r2_budget", "r2_month", "r2_day", "r2_restore_lock", "r2_objects", "r2_user_day", "r2_migrations"} {
		if _, err := db.W.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.W.Exec(`DELETE FROM schema_migrations WHERE name='0037_r2.sql'`); err != nil {
		t.Fatal(err)
	}
}

func ledgerTables(t *testing.T, path string) int {
	t.Helper()
	db, err := openDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE 'r2\_%' ESCAPE '\'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
