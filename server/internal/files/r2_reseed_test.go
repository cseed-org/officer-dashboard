package files

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"officerdashboard/internal/store"
)

const fakeAnalyticsToken = "test-analytics-token" // a test value, never a credential

func fakeAnalytics(t *testing.T, r *R2, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	r.Analytics = &R2Analytics{Endpoint: srv.URL, Account: strings.Repeat("a", 32), Bucket: "receipts", Token: fakeAnalyticsToken, HTTP: srv.Client()}
}

func rolling(t *testing.T, r *R2) (a, b int64) {
	t.Helper()
	if err := r.DB.R.QueryRow(`SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, r.Now().AddDate(0, 0, -31).Format("2006-01-02")).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	return a, b
}

// D-20: after a restore, Cloudflare's GraphQL analytics re-seed the ledger.
func TestR2ReseedFromCloudflareAnalytics(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	r.DB.RequestR2Reseed(ctx, r.Now())
	if _, err := r.DB.ReserveR2(ctx, r.Now(), "get", "", "", 0); !errors.Is(err, store.ErrR2Reseeding) {
		t.Fatal("spent before the re-seed", err)
	}
	fakeAnalytics(t, r, func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Query     string
			Variables map[string]string
		}
		json.NewDecoder(req.Body).Decode(&body)
		v := body.Variables
		if req.Method != "POST" || req.Header.Get("Authorization") != "Bearer "+fakeAnalyticsToken || !strings.Contains(body.Query, "r2OperationsAdaptiveGroups") || !strings.Contains(body.Query, "r2StorageAdaptiveGroups") ||
			v["account"] != strings.Repeat("a", 32) || v["bucket"] != "receipts" || v["month"] != "2026-11-01T00:00:00Z" || v["oldest"] != "2026-10-15T00:00:00Z" || v["rolling"] != "2026-10-16T00:00:00Z" || !strings.Contains(body.Query, "datetime_lt: $rolling") {
			t.Errorf("unexpected analytics request %s %v", req.Method, v)
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"data":{"viewer":{"accounts":[{
			"month":[{"sum":{"requests":1000},"dimensions":{"actionType":"PutObject"}},{"sum":{"requests":200},"dimensions":{"actionType":"ListObjectsV2"}},{"sum":{"requests":5000},"dimensions":{"actionType":"GetObject"}},{"sum":{"requests":99999},"dimensions":{"actionType":"DeleteObject"}}],
			"oldest":[{"sum":{"requests":500},"dimensions":{"actionType":"ListObjects"}},{"sum":{"requests":1000},"dimensions":{"actionType":"HeadObject"}}],
			"rolling":[{"sum":{"requests":2500},"dimensions":{"actionType":"PutObject"}},{"sum":{"requests":7000},"dimensions":{"actionType":"GetObject"}}],
			"storage":[{"max":{"payloadSize":4000,"metadataSize":10}}]}]}},"errors":null}`))
	})
	alerts := 0
	r.Alert = func(context.Context, string) bool { alerts++; return true }
	again, err := r.Reseed(ctx)
	if err != nil || again {
		t.Fatal("measured re-seed incomplete", again, err)
	}
	u, _ := r.DB.R2Usage(ctx, r.Now())
	if u.ClassA != 1200 || u.ClassB != 5000 || u.Bytes != 4010 {
		t.Fatal("month counters not re-seeded from analytics", u)
	}
	if a, b := rolling(t, r); a != 3000 || b != 8000 {
		t.Fatal("rolling window not re-seeded from analytics", a, b)
	}
	if alerts != 0 {
		t.Fatal("successful re-seed alerted")
	}
	if _, err := r.DB.ReserveR2(ctx, r.Now(), "get", "", "", 0); err != nil {
		t.Fatal("restore still locks downloads", err)
	}
}

// D-20: analytics failure assumes 50% and alerts once; R2 keeps working, and
// the measurement is retried so the ledger can still rise to real usage.
func TestR2ReseedFallsBackToHalfWhenAnalyticsFails(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"graphql-error": func(w http.ResponseWriter) {
			w.Write([]byte(`{"data":null,"errors":[{"message":"not authorized"}]}`))
		},
		"http-error": func(w http.ResponseWriter) { w.WriteHeader(500) },
		"truncated":  func(w http.ResponseWriter) { w.Write([]byte(`{"data":{"viewer":{"accounts":[{"month":[`)) },
	} {
		t.Run(name, func(t *testing.T) {
			r := testR2(t)
			ctx := context.Background()
			r.DB.RequestR2Reseed(ctx, r.Now())
			var healthy atomic.Bool
			fakeAnalytics(t, r, func(w http.ResponseWriter, _ *http.Request) {
				if healthy.Load() {
					w.Write([]byte(`{"data":{"viewer":{"accounts":[{"month":[{"sum":{"requests":700000},"dimensions":{"actionType":"PutObject"}}],"rolling":[],"storage":[]}]}}}`))
					return
				}
				reply(w)
			})
			var reasons []string
			delivered := false
			r.Alert = func(_ context.Context, reason string) bool { reasons = append(reasons, reason); return delivered }
			again, err := r.Reseed(ctx)
			if !again || err == nil {
				t.Fatal("failed measurement not retried", again, err)
			}
			if strings.Contains(err.Error(), fakeAnalyticsToken) {
				t.Fatal("error exposes the token")
			}
			u, _ := r.DB.R2Usage(ctx, r.Now())
			if u.ClassA != store.R2ClassALimit/2 || u.ClassB != store.R2ClassBLimit/2 {
				t.Fatal("fallback did not assume 50%", u)
			}
			if a, b := rolling(t, r); a != store.R2ClassALimit/2 || b != store.R2ClassBLimit/2 {
				t.Fatal("fallback missed the rolling window", a, b)
			}
			if _, err := r.DB.ReserveR2(ctx, r.Now(), "get", "", "", 0); err != nil {
				t.Fatal("fallback blocked downloads", err)
			}
			delivered = true
			r.Reseed(ctx) // re-sends the undelivered alert
			r.Reseed(ctx)
			if len(reasons) != 2 || !strings.Contains(reasons[0], "half of each free-tier allowance") {
				t.Fatal("fallback alert not sent exactly once after delivery", reasons)
			}
			if u2, _ := r.DB.R2Usage(ctx, r.Now()); u2.ClassA != u.ClassA {
				t.Fatal("repeated fallback moved counters", u2)
			}
			healthy.Store(true)
			if again, err := r.Reseed(ctx); again || err != nil {
				t.Fatal("recovered analytics did not finish the re-seed", again, err)
			}
			if u, _ := r.DB.R2Usage(ctx, r.Now()); u.ClassA != 700000 {
				t.Fatal("later measurement did not raise the fallback", u)
			}
		})
	}
}

// Without an analytics token the fallback applies, alerts once, and is done.
func TestR2ReseedWithoutAnalyticsIsDoneAfterOneAlert(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	r.DB.RequestR2Reseed(ctx, r.Now())
	alerts := 0
	r.Alert = func(_ context.Context, reason string) bool {
		alerts++
		return strings.Contains(reason, "not configured")
	}
	if again, err := r.Reseed(ctx); again || err != nil {
		t.Fatal(again, err)
	}
	r.Reseed(ctx)
	if s, _ := r.DB.R2Reseed(ctx); s.Pending || alerts != 1 {
		t.Fatal("unconfigured re-seed", s, alerts)
	}
}

// D-20 end to end: Daily re-seeds before its first inventory, without
// waiting on analytics past the timeout, and uploads then open normally.
func TestR2DailyReseedsThenOpensUploads(t *testing.T) {
	r := testR2(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.DB.RequestR2Reseed(ctx, r.Now())
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			if req.URL.Query().Has("uploads") {
				w.Write([]byte(`<ListMultipartUploadsResult></ListMultipartUploadsResult>`))
				return
			}
			w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
		}
	}))
	defer fake.Close()
	r.Endpoint = fake.URL
	r.Alert = func(context.Context, string) bool { return true }
	started := time.Now()
	scanned := make(chan struct{})
	go r.Daily(ctx, func(error) {})
	go func() {
		for ctx.Err() == nil {
			var reconciled int64
			r.DB.R.QueryRow(`SELECT reconciled_at FROM r2_budget`).Scan(&reconciled)
			if s, _ := r.DB.R2Reseed(ctx); !s.Pending && reconciled != 0 {
				close(scanned)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	select {
	case <-scanned:
	case <-time.After(10 * time.Second):
		t.Fatal("Daily did not re-seed and scan")
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("re-seed waited too long")
	}
	cancel()
	if _, err := r.Put(context.Background(), PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}); err != nil {
		t.Fatal("uploads stayed closed after the restore re-seed", err)
	}
}
