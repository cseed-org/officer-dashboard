package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// BenchmarkFixup1B covers only handlers changed by the review follow-up. The same
// file compiles on current main and on the branch so three paired rounds are comparable.
func BenchmarkFixup1B(b *testing.B) {
	e := newEnv(b, func(c *Config) { c.MCPRateLimit = -1 })
	ctx := context.Background()
	for account, token := range map[string]string{"lp-account-alex": "fixup1b-builder", "lp-account-maya": "fixup1b-mentor"} {
		account, token := account, token
		if err := e.db.Write(ctx, func(q *store.Queries) error {
			return q.InsertLaunchpadSession(ctx, auth.SHA256Hex(token), account, e.clock.Add(24*time.Hour).UnixMilli(), "bench", e.clock.UnixMilli())
		}); err != nil {
			b.Fatal(err)
		}
	}
	accessToken := "fixup1b-mcp-token"
	now := e.clock.UnixMilli()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		if err := q.InsertOAuthClient(ctx, store.OAuthClient{ID: "fixup1b-client", Name: "Benchmark", RedirectURIs: []string{"https://example.com/callback"}, CreatedAt: store.Ms(now)}); err != nil {
			return err
		}
		return q.InsertOAuthGrant(ctx, store.OAuthGrant{ID: "fixup1b-grant", ClientID: "fixup1b-client", UserID: "user-director", AccessHash: auth.SHA256Hex(accessToken), AccessExpiresAt: now + time.Hour.Milliseconds(), RefreshHash: "unused", RefreshExpiresAt: now + time.Hour.Milliseconds(), CreatedAt: now, LastUsedAt: &now})
	}); err != nil {
		b.Fatal(err)
	}

	for _, path := range []string{"/api/buildspace-launchpad", "/api/design-requests"} {
		b.Run(path, func(b *testing.B) {
			req := httptest.NewRequest("GET", "http://localhost:8090"+path, nil)
			req.AddCookie(e.login("user-director"))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				e.h.ServeHTTP(rec, req)
				if rec.Code != 200 {
					b.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
				}
			}
		})
	}
	for path, token := range map[string]string{"/api/portal/builder": "fixup1b-builder", "/api/portal/mentor": "fixup1b-mentor"} {
		b.Run(path, func(b *testing.B) {
			req := httptest.NewRequest("GET", "http://localhost:8090"+path, nil)
			req.AddCookie(&http.Cookie{Name: e.srv.portalCookieName(), Value: token})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				e.h.ServeHTTP(rec, req)
				if rec.Code != 200 {
					b.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
				}
			}
		})
	}
	b.Run("/mcp ping", func(b *testing.B) {
		body := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		req := httptest.NewRequest("POST", "http://localhost:8090/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req.Body = io.NopCloser(bytes.NewReader(body))
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			if rec.Code != 200 {
				b.Fatalf("mcp: %d %s", rec.Code, rec.Body.String())
			}
		}
	})
}
