package cfsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

func TestPullIdempotentAndFailureRetainsMirror(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir() + "/sync.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fail := false
	calls := 0
	count := 1001
	fake := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request %s %s (auth %q)", r.Method, r.URL.Path, r.Header.Get("Authorization"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		calls++
		var body struct {
			SQL    string   `json:"sql"`
			Params []string `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.HasSuffix(r.URL.Path, "graphql") && len(body.Params) == 0 {
			t.Errorf("D1 query without params: %q", body.SQL)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, "graphql") {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"accounts":[{"rumPageloadEventsAdaptiveGroups":[{"count":25,"sum":{"visits":12}}]}]}}}`))
			return
		}
		if fail && strings.Contains(body.SQL, "Membership_submissions") {
			http.Error(w, "no", 500)
			return
		}
		rows := []any{}
		if strings.Contains(body.SQL, "People") {
			for i := 1; i <= count; i++ {
				id := fmt.Sprintf("p%04d", i)
				if id > body.Params[0] && len(rows) < 250 {
					rows = append(rows, cloudflare.Person{ID: id, FirstName: "Member", LastName: id, JoinDate: "2026-10-31"})
				}
			}
		} else {
			if body.Params[0] == "" {
				rows = append(rows, cloudflare.Submission{ID: "s1", PersonID: "p0001", Payload: `{"ok":true}`, ReceivedAt: "2026-10-31"})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{map[string]any{"success": true, "results": rows}}})
	})
	client := cloudflare.New(cloudflare.Config{Token: "secret", AccountID: "account", ZoneID: "zone", DatabaseID: "db"})
	// Serve the fake API in-process rather than on a loopback port: anything else on the
	// machine that connects to a listening port (health checks, port probes) would
	// otherwise reach the handler and break the exact request count below.
	client.BaseURL = "http://cloudflare.test"
	client.HTTP = &http.Client{Transport: handlerTransport{fake}}
	svc := &Service{Client: client, DB: db}
	for i := 0; i < 2; i++ {
		if err = svc.Run(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 20 {
		t.Fatalf("expected 5 people pages + 1 submission page + 4 analytics calls per run, got %d", calls)
	}
	items, total, err := db.Read().Members(ctx, store.MemberFilter{Limit: 50})
	if err != nil || total != 1001 || len(items) != 50 {
		t.Fatalf("page=%d total=%d err=%v", len(items), total, err)
	}
	for table, want := range map[string]int{"members_mirror": 1001, "members_submissions_mirror": 1, "cloudflare_snapshots": 4} {
		var n int
		_ = db.R.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
		if n != want {
			t.Errorf("%s=%d want %d", table, n, want)
		}
	}
	// A partial pull must not delete any of the previous mirror.
	fail = true
	count = 1
	if err = svc.Run(ctx, nil); err == nil {
		t.Fatal("expected upstream failure")
	}
	_, total, _ = db.Read().Members(ctx, store.MemberFilter{Limit: 1})
	if total != 1001 {
		t.Fatal("failed pull replaced mirror")
	}
	last, msg, err := db.Read().SyncStatus(ctx, "members")
	if err != nil || last == nil || msg == "" {
		t.Fatal("last success/error not retained")
	}
	fail = false
	if err = svc.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	_, total, _ = db.Read().Members(ctx, store.MemberFilter{Limit: 1})
	if total != 1 {
		t.Fatal("source deletions not mirrored")
	}
}

// handlerTransport answers requests by calling the handler directly, with no listener.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func TestMissingConfigDoesNotCallHTTP(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/skip.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client := cloudflare.New(cloudflare.Config{})
	client.BaseURL = "http://127.0.0.1:1"
	svc := &Service{Client: client, DB: db}
	if err = svc.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !svc.Lock() {
		t.Fatal("lock not released")
	}
	if err = svc.Run(context.Background(), nil); err != ErrBusy {
		t.Fatalf("want busy got %v", err)
	}
	svc.Unlock()
}
