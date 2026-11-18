package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/store"
)

type googleDeliveryFake struct {
	mu          sync.Mutex
	requests    []googleDeliveryRequest
	refreshes   int
	created     int
	patchStatus int
	revoked     bool
}

type googleDeliveryRequest struct {
	method, path, bearer string
	body                 map[string]any
}

func (f *googleDeliveryFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "refresh" {
			http.Error(w, `{"error":"invalid_request"}`, 400)
			return
		}
		f.refreshes++
		if f.revoked {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"refreshed","expires_in":3600}`))
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.requests = append(f.requests, googleDeliveryRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
	if r.Method == "PATCH" && f.patchStatus != 0 {
		w.WriteHeader(f.patchStatus)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/calendars/primary/events/")
	if r.Method == "POST" {
		f.created++
		id = fmt.Sprintf("google-%d", f.created)
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func googleDeliveryEnv(t *testing.T) (*testEnv, *googleDeliveryFake) {
	t.Helper()
	f := &googleDeliveryFake{}
	remote := httptest.NewServer(f)
	t.Cleanup(remote.Close)
	e := newEnv(t, func(c *Config) {
		g := calendar.NewGoogle("client", "secret", "http://localhost/auth/google/callback")
		g.TokenURL, g.APIBase = remote.URL+"/token", remote.URL
		c.Google = g
	})
	access, expires := e.srv.sealer.Seal("initial"), e.clock.Add(time.Hour).UnixMilli()
	err := e.db.Write(context.Background(), func(q *store.Queries) error {
		if err := q.SaveGoogleToken(context.Background(), store.GoogleToken{
			UserID: "user-priya", SealedRefresh: e.srv.sealer.Seal("refresh"),
			SealedAccess: &access, AccessExpires: &expires, ConnectedAt: e.clock.UnixMilli(),
		}); err != nil {
			return err
		}
		return q.SetDeliveryMode(context.Background(), "user-priya", "google")
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, f
}

func googleDeliveryEvent(e *testEnv) string {
	e.t.Helper()
	r := e.mustOK(e.post("/api/events", map[string]any{"name": "Google delivery", "startsAt": "2026-12-01T18:00:00-08:00"}, e.officer()))
	e.srv.WaitBackground()
	return r.str("event", "id")
}

func TestGoogleDeliveryDeleteCancelsAndExpiredAccessRefreshes(t *testing.T) {
	e, fake := googleDeliveryEnv(t)
	id := googleDeliveryEvent(e)
	e.clock = e.clock.Add(2 * time.Hour)
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "Refreshed event"}, e.officer()))
	e.srv.WaitBackground()
	token, err := e.db.Read().GoogleTokenFor(context.Background(), "user-priya")
	if err != nil {
		t.Fatal(err)
	}
	access, err := e.srv.sealer.Open(*token.SealedAccess)
	if err != nil || access != "refreshed" || *token.AccessExpires != e.clock.Add(time.Hour).UnixMilli() {
		t.Fatalf("refresh not persisted: token=%+v access=%q error=%v", token, access, err)
	}
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.created != 1 || fake.refreshes != 1 || len(fake.requests) != 3 {
		t.Fatalf("creates=%d refreshes=%d requests=%+v", fake.created, fake.refreshes, fake.requests)
	}
	update, cancel := fake.requests[1], fake.requests[2]
	if update.method != "PATCH" || update.bearer != "Bearer refreshed" || update.body["summary"] != "Refreshed event" {
		t.Fatalf("update %+v", update)
	}
	if cancel.method != "PATCH" || cancel.path != "/calendars/primary/events/google-1" || cancel.body["status"] != "cancelled" || cancel.bearer != "Bearer refreshed" {
		t.Fatalf("cancellation %+v", cancel)
	}
}

func TestGoogleDeliveryRecreatesMissingRemoteEvent(t *testing.T) {
	for _, status := range []int{404, 410} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			e, fake := googleDeliveryEnv(t)
			id := googleDeliveryEvent(e)
			fake.mu.Lock()
			fake.patchStatus = status
			fake.mu.Unlock()
			e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "Recreated"}, e.officer()))
			e.srv.WaitBackground()
			grant, err := e.db.Read().GoogleTokenFor(context.Background(), "user-priya")
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := e.db.Read().DeliveryFor(context.Background(), id, "user-priya", grant.Destination())
			if err != nil || delivery.GoogleEventID == nil || *delivery.GoogleEventID != "google-2" || delivery.Status != "sent" {
				t.Fatalf("recreated delivery=%+v error=%v", delivery, err)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if len(fake.requests) != 3 || fake.requests[1].method != "PATCH" || fake.requests[2].method != "POST" || fake.requests[2].body["summary"] != "Recreated" {
				t.Fatalf("recreate requests %+v", fake.requests)
			}
		})
	}
}

func TestGoogleDeliveryRevokedGrantExplainsReconnectInSettings(t *testing.T) {
	e, fake := googleDeliveryEnv(t)
	e.clock = e.clock.Add(2 * time.Hour)
	fake.mu.Lock()
	fake.revoked = true
	fake.mu.Unlock()
	result := e.mustOK(e.post("/api/me/calendar/test", nil, e.officer()))
	if result.str("status") != "failed" || !strings.Contains(result.str("message"), "revoked") || !strings.Contains(result.str("message"), "Reconnect") {
		t.Fatalf("settings test response %s", result.Raw)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.refreshes != 1 || len(fake.requests) != 0 {
		t.Fatalf("revoked grant delivered: refreshes=%d requests=%+v", fake.refreshes, fake.requests)
	}
}
