package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestOAuthRefreshHistoryLifecycle(t *testing.T) {
	db, err := Open(t.TempDir() + "/oauth.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.W.ExecContext(ctx, `INSERT INTO users (id, discord_id, name) VALUES ('u', 'discord-u', 'User')`); err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, func(q *Queries) error {
		if err := q.InsertOAuthClient(ctx, OAuthClient{ID: "client", Name: "Client", RedirectURIs: []string{"https://example.com/callback"}, CreatedAt: 1}); err != nil {
			return err
		}
		return q.InsertOAuthGrant(ctx, OAuthGrant{ID: "grant", ClientID: "client", UserID: "u", AccessHash: "a0", AccessExpiresAt: 1000, RefreshHash: "r0", RefreshExpiresAt: 1000, CreatedAt: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(q *Queries) error {
		if err := q.RotateGrant(ctx, "grant", "a1", 2000, "r1", 2000, 10); err != nil {
			return err
		}
		return q.RotateGrant(ctx, "grant", "a2", 3000, "r2", 3000, 20)
	}); err != nil {
		t.Fatal(err)
	}
	q := db.Read()
	for hash, wantRotation := range map[string]int64{"r0": 10, "r1": 20} {
		g, err := q.GrantByRotatedRefreshHash(ctx, hash)
		if err != nil {
			t.Fatalf("history lookup %q: %v", hash, err)
		}
		if g.ID != "grant" || g.RotatedAt == nil || *g.RotatedAt != wantRotation {
			t.Fatalf("history lookup %q: grant=%+v rotation=%v", hash, g, g.RotatedAt)
		}
	}
	var rows int
	if err := db.R.QueryRowContext(ctx, `SELECT count(*) FROM oauth_refresh_history`).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("history rows = %d, err=%v", rows, err)
	}
	if err := db.Write(ctx, func(q *Queries) error { _, err := q.PurgeOAuth(ctx, 3001); return err }); err != nil {
		t.Fatal(err)
	}
	if err := db.R.QueryRowContext(ctx, `SELECT count(*) FROM oauth_refresh_history`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("history after grant expiry = %d, err=%v", rows, err)
	}
	if _, err := q.GrantByID(ctx, "grant"); err != nil {
		t.Fatalf("expired grant audit row was deleted: %v", err)
	}
}

func TestOAuthUnusedClientRowCap(t *testing.T) {
	db, err := Open(t.TempDir() + "/oauth-clients.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Write(ctx, func(q *Queries) error {
		for i := 0; i < oauthUnusedClientMaxRows+3; i++ {
			if err := q.InsertOAuthClient(ctx, OAuthClient{ID: fmt.Sprintf("client-%04d", i), Name: "Client", RedirectURIs: []string{"https://example.com/cb"}, CreatedAt: Ms((i + 1) * 3_600_001)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.R.QueryRowContext(ctx, `SELECT count(*) FROM oauth_clients`).Scan(&count); err != nil || count != oauthUnusedClientMaxRows {
		t.Fatalf("client rows = %d, want %d (err=%v)", count, oauthUnusedClientMaxRows, err)
	}
	if _, err := db.Read().OAuthClientByID(ctx, "client-0000"); err == nil {
		t.Fatal("oldest unused client survived the row cap")
	}
	if _, err := db.Read().OAuthClientByID(ctx, fmt.Sprintf("client-%04d", oauthUnusedClientMaxRows+2)); err != nil {
		t.Fatalf("newest client missing: %v", err)
	}
}

func TestRegistrationRetainsYoungClientsWhenFull(t *testing.T) {
	db, err := Open(t.TempDir() + "/cap.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	err = db.Write(ctx, func(q *Queries) error {
		for i := 0; i < oauthUnusedClientMaxRows; i++ {
			if err := q.InsertOAuthClient(ctx, OAuthClient{ID: fmt.Sprint(i), Name: "Client", RedirectURIs: []string{"https://client.example/cb"}, CreatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, func(q *Queries) error {
		return q.InsertOAuthClient(ctx, OAuthClient{ID: "attacker", Name: "New", RedirectURIs: []string{"https://client.example/cb"}, CreatedAt: 2})
	})
	if !errors.Is(err, ErrOAuthClientCapacity) {
		t.Fatalf("young registration evicted: %v", err)
	}
	if _, err := db.Read().OAuthClientByID(ctx, "0"); err != nil {
		t.Fatal("original client evicted")
	}
}
