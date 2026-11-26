package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
	"officerdashboard/internal/store"
)

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

// insertDriveAccount connects a member account with a stale access token, so the first use
// refreshes through the token endpoint.
func insertDriveAccount(t *testing.T, db *store.DB) {
	t.Helper()
	sealer := calendar.NewSealer(testClientSecret)
	err := db.Write(context.Background(), func(q *store.Queries) error {
		return q.ConnectDriveGoogle(context.Background(), store.DriveGoogleAccount{
			Subject: "sub-test", Email: "drive@example.org",
			SealedRefresh: sealer.Seal("synthetic-refresh-token"),
			SealedAccess:  sealer.Seal("synthetic-stale-access"),
			ConnectedAt:   1,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConnectDriveUsesStoredAccount(t *testing.T) {
	db := testDB(t)
	insertDriveAccount(t, db)
	fake := drivefake.New()
	s := settings{GoogleClientID: "synthetic-client-id", GoogleSecret: testClientSecret, DriveRoot: fake.RootID}
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
	fake.RefreshError = "invalid_grant"
	s := settings{GoogleClientID: "synthetic-client-id", GoogleSecret: testClientSecret, DriveRoot: fake.RootID}
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
	db := testDB(t) // no member account connected
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
