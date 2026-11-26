package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
	"officerdashboard/internal/store"
)

// driveAccountSchema mirrors PR A's 0037_drive_account.sql: the stored Drive sign-in an
// owner connects in Settings.
const driveAccountSchema = `CREATE TABLE drive_google_account (
	id INTEGER PRIMARY KEY CHECK(id=1),
	subject TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '',
	sealed_refresh TEXT NOT NULL DEFAULT '', sealed_access TEXT NOT NULL DEFAULT '',
	expires_at INTEGER NOT NULL DEFAULT 0,
	connected_by TEXT REFERENCES users(id) ON DELETE SET NULL, connected_at INTEGER NOT NULL DEFAULT 0,
	revoked_at INTEGER,
	root_folder_id TEXT NOT NULL DEFAULT ''
);`

const testClientSecret = "synthetic-test-client-secret"

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func insertDriveAccount(t *testing.T, db *store.DB) {
	t.Helper()
	if _, err := db.W.Exec(driveAccountSchema); err != nil {
		t.Fatal(err)
	}
	sealer := calendar.NewSealer(testClientSecret)
	_, err := db.W.Exec(`INSERT INTO drive_google_account(id,subject,email,sealed_refresh,sealed_access,expires_at,connected_at,root_folder_id)
		VALUES(1,'sub-test','drive@example.org',?,?,0,1,'root')`,
		sealer.Seal("synthetic-refresh-token"), sealer.Seal("synthetic-stale-access"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestConnectDriveUsesStoredAccount(t *testing.T) {
	db := testDB(t)
	insertDriveAccount(t, db)
	fake := drivefake.New()
	s := settings{GoogleClientID: "synthetic-client-id", GoogleSecret: testClientSecret}
	drive, err := connectDrive(context.Background(), s, db, fake)
	if err != nil {
		t.Fatal(err)
	}
	// The refresh token is exchanged through the fake token endpoint, then Drive works.
	if _, err := drive.EnsureFolder(context.Background(), []string{"Backups"}); err != nil {
		t.Fatal(err)
	}
	if fake.Path("Backups") == nil {
		t.Fatal("Drive folder not created through the stored account")
	}
	// The minted access token was persisted, sealed, replacing the stale one.
	var sealedAccess string
	var expires int64
	if err := db.R.QueryRow(`SELECT sealed_access, expires_at FROM drive_google_account WHERE id=1`).Scan(&sealedAccess, &expires); err != nil {
		t.Fatal(err)
	}
	sealer := calendar.NewSealer(testClientSecret)
	access, err := sealer.Open(sealedAccess)
	if err != nil || access != "fake-access-token" {
		t.Fatalf("persisted access token: %q, %v", access, err)
	}
	if expires <= time.Now().UnixMilli() {
		t.Fatalf("access expiry not updated: %d", expires)
	}
	if strings.Contains(sealedAccess, "fake-access-token") {
		t.Fatal("token persisted in clear")
	}
}

func TestConnectDriveRevokedMarksDisconnected(t *testing.T) {
	db := testDB(t)
	insertDriveAccount(t, db)
	fake := drivefake.New()
	fake.Fail = func(req *http.Request) *http.Response {
		if req.URL.Host == "oauth2.googleapis.com" {
			return drivefake.Error(http.StatusBadRequest, "invalid_grant")
		}
		return nil
	}
	s := settings{GoogleClientID: "synthetic-client-id", GoogleSecret: testClientSecret}
	drive, err := connectDrive(context.Background(), s, db, fake)
	if err != nil {
		t.Fatal(err)
	}
	_, err = drive.EnsureFolder(context.Background(), []string{"Backups"})
	if !errors.Is(err, files.ErrReconnect) {
		t.Fatalf("error = %v, want ErrReconnect", err)
	}
	var refresh string
	var revoked *int64
	if err := db.R.QueryRow(`SELECT sealed_refresh, revoked_at FROM drive_google_account WHERE id=1`).Scan(&refresh, &revoked); err != nil {
		t.Fatal(err)
	}
	if refresh != "" || revoked == nil {
		t.Fatalf("account not marked disconnected: refresh=%q revoked=%v", refresh, revoked)
	}
}

func TestConnectDriveServiceAccountFallback(t *testing.T) {
	db := testDB(t) // no drive_google_account table at all
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := json.Marshal(map[string]string{
		"client_email": "synthetic@example.invalid",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	fake := drivefake.New()
	s := settings{ServiceAccountJSON: string(account), DriveRoot: fake.RootID}
	drive, err := connectDrive(context.Background(), s, db, fake)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drive.EnsureFolder(context.Background(), []string{"Backups"}); err != nil {
		t.Fatal(err)
	}
	if fake.Path("Backups") == nil {
		t.Fatal("service-account fallback did not reach Drive")
	}
}

func TestConnectDriveNotConnected(t *testing.T) {
	db := testDB(t)
	if _, err := connectDrive(context.Background(), settings{}, db, nil); !errors.Is(err, files.ErrNotConnected) {
		t.Fatalf("error = %v, want ErrNotConnected", err)
	}
}
