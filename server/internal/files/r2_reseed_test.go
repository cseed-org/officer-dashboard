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

const emptyAnalytics = `{"data":{"viewer":{"accounts":[{"month":[],"oldest":[],"rolling":[],"storage":[]}]}}}`

func fakeAnalytics(t *testing.T, r *R2, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	r.Analytics = &R2Analytics{Endpoint: srv.URL, Account: strings.Repeat("a", 32), Bucket: "receipts", Token: fakeAnalyticsToken, HTTP: srv.Client()}
}

// fakeBucket is an empty R2 bucket that counts the requests reaching it.
// While listOK is false, inventory listings fail.
type fakeBucket struct{ requests, puts atomic.Int64 }

func newFakeBucket(t *testing.T, r *R2, listOK *atomic.Bool) *fakeBucket {
	t.Helper()
	b := &fakeBucket{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b.requests.Add(1)
		switch {
		case req.Method == "PUT":
			b.puts.Add(1)
		case req.Method != "GET":
		case strings.HasPrefix(req.URL.Path, "/receipts/files/"):
			w.Header().Set("Content-Type", "application/pdf")
			w.Write([]byte("%PDF-x"))
		case listOK != nil && !listOK.Load():
			w.WriteHeader(500)
		case req.URL.Query().Has("uploads"):
			w.Write([]byte(`<ListMultipartUploadsResult></ListMultipartUploadsResult>`))
		default:
			w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
		}
	}))
	t.Cleanup(srv.Close)
	r.Endpoint = srv.URL
	return b
}

func rolling(t *testing.T, r *R2) (a, b int64) {
	t.Helper()
	if err := r.DB.R.QueryRow(`SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, r.Now().AddDate(0, 0, -31).Format("2006-01-02")).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	return a, b
}

var testUpload = PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}

const testKey = "files/00000000-0000-4000-8000-000000000000"

// D-20/D-21: after a restore, Cloudflare's GraphQL analytics re-seed the ledger.
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
	paused, err := r.Reseed(ctx)
	if err != nil || paused {
		t.Fatal("measured re-seed incomplete", paused, err)
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
		t.Fatal("re-seeded ledger still pauses downloads", err)
	}
}

// D-21: when analytics is unset or fails, nothing is assumed. Uploads and
// downloads answer "storage unavailable", no R2 request is sent, the re-seed
// is retried hourly, and the alert goes out once and then about daily.
func TestR2AnalyticsFailureKeepsR2Paused(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"unconfigured": nil,
		"graphql-error": func(w http.ResponseWriter) {
			w.Write([]byte(`{"data":null,"errors":[{"message":"not authorized"}]}`))
		},
		"http-error": func(w http.ResponseWriter) { w.WriteHeader(500) },
		"truncated":  func(w http.ResponseWriter) { w.Write([]byte(`{"data":{"viewer":{"accounts":[{"month":[`)) },
	} {
		t.Run(name, func(t *testing.T) {
			r := testR2(t)
			ctx := context.Background()
			clock := r.Now()
			r.Now = func() time.Time { return clock }
			r.DB.ReconcileR2(ctx, clock, 0, nil)
			r.DB.RequestR2Reseed(ctx, clock)
			bucket := newFakeBucket(t, r, nil)
			var healthy atomic.Bool
			if reply != nil {
				fakeAnalytics(t, r, func(w http.ResponseWriter, _ *http.Request) {
					if healthy.Load() {
						w.Write([]byte(`{"data":{"viewer":{"accounts":[{"month":[{"sum":{"requests":700000},"dimensions":{"actionType":"PutObject"}}],"rolling":[],"storage":[]}]}}}`))
						return
					}
					reply(w)
				})
			}
			var reasons []string
			delivered := false
			r.Alert = func(_ context.Context, reason string) bool { reasons = append(reasons, reason); return delivered }
			var errs []error
			step := func() time.Duration {
				alerted := false
				return r.dailyStep(ctx, func(err error) { errs = append(errs, err) }, &alerted)
			}
			if next := step(); next != time.Hour {
				t.Fatal("paused R2 not retried hourly", next)
			}
			if len(errs) == 0 || strings.Contains(errs[0].Error(), fakeAnalyticsToken) {
				t.Fatal("failure not reported, or reported with the token", errs)
			}
			if _, err := r.Put(ctx, testUpload); !errors.Is(err, ErrBudget) || !errors.Is(err, store.ErrR2Reseeding) {
				t.Fatal("paused R2 accepted an upload", err)
			}
			if _, err := r.Get(ctx, testKey); !errors.Is(err, ErrBudget) || !errors.Is(err, store.ErrR2Reseeding) {
				t.Fatal("paused R2 served a download", err)
			}
			if n := bucket.requests.Load(); n != 0 {
				t.Fatal("paused R2 sent requests", n)
			}
			if u, _ := r.DB.R2Usage(ctx, clock); u.ClassA != 0 || u.ClassB != 0 || u.Bytes != 0 {
				t.Fatal("pause assumed usage", u)
			}
			// Undelivered: re-sent at the next hourly retry; then about daily.
			clock = clock.Add(time.Hour)
			delivered = true
			step()
			clock = clock.Add(time.Hour)
			step()
			if len(reasons) != 2 || !strings.Contains(reasons[1], "paused") || !strings.Contains(reasons[1], "R2_ANALYTICS_TOKEN_FILE") || !strings.Contains(reasons[1], "2026-12-17 00:00 UTC") {
				t.Fatal("pause alert not re-sent until delivered", reasons)
			}
			clock = clock.Add(22 * time.Hour)
			step()
			if len(reasons) != 3 {
				t.Fatal("pause alert not repeated about daily", len(reasons))
			}
			if reply == nil {
				return
			}
			healthy.Store(true)
			if next := step(); next != 24*time.Hour {
				t.Fatal("recovered analytics did not end the pause and scan", next)
			}
			if u, _ := r.DB.R2Usage(ctx, clock); u.ClassA < 700000 {
				t.Fatal("measurement not merged", u)
			}
			if _, err := r.Put(ctx, testUpload); err != nil {
				t.Fatal("R2 stayed paused after analytics and inventory", err)
			}
		})
	}
}

// Round 5: a restored snapshot keeps a fresh-looking reconciled_at. Ending
// the pause must not reopen uploads on that inventory: an upload during the
// pause alert, or between the re-seed and the next scan, is refused, and so
// is one after a failed scan. Downloads resume after the re-seed.
func TestR2RestoredInventoryNeverReopensUploads(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	if err := r.DB.ReconcileR2(ctx, r.Now(), 0, nil); err != nil { // recent restored inventory
		t.Fatal(err)
	}
	r.DB.RequestR2Reseed(ctx, r.Now())
	var listOK atomic.Bool
	bucket := newFakeBucket(t, r, &listOK)
	var healthy atomic.Bool
	fakeAnalytics(t, r, func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(emptyAnalytics))
	})
	var overlapping []error
	r.Alert = func(context.Context, string) bool {
		// A slow notification: an upload overlaps it.
		_, err := r.Put(ctx, testUpload)
		overlapping = append(overlapping, err)
		return true
	}
	alerted := false
	if next := r.dailyStep(ctx, func(error) {}, &alerted); next != time.Hour {
		t.Fatal("failed analytics did not keep R2 paused", next)
	}
	if len(overlapping) != 1 || !errors.Is(overlapping[0], ErrBudget) {
		t.Fatal("upload during the pause alert accepted", overlapping)
	}
	healthy.Store(true)
	if paused, err := r.Reseed(ctx); paused || err != nil {
		t.Fatal(paused, err)
	}
	if _, err := r.Put(ctx, testUpload); !errors.Is(err, ErrBudget) {
		t.Fatal("upload accepted on the restored inventory", err)
	}
	if f, err := r.Get(ctx, testKey); err != nil || f == nil {
		t.Fatal("downloads did not resume after the re-seed", err)
	}
	if next := r.dailyStep(ctx, func(error) {}, &alerted); next != 5*time.Minute {
		t.Fatal("failed scan not retried", next)
	}
	if _, err := r.Put(ctx, testUpload); !errors.Is(err, ErrBudget) {
		t.Fatal("upload accepted after a failed scan", err)
	}
	if n := bucket.puts.Load(); n != 0 {
		t.Fatal("PUT requests sent before a post-restore inventory", n)
	}
	listOK.Store(true)
	if next := r.dailyStep(ctx, func(error) {}, &alerted); next != 24*time.Hour {
		t.Fatal("scan failed", next)
	}
	if _, err := r.Put(ctx, testUpload); err != nil {
		t.Fatal("fresh inventory did not reopen uploads", err)
	}
}

// D-21 safety cap: without analytics, R2 unlocks on the 32nd UTC day after
// the restore day, once the billing window has fully rolled, and then scans.
func TestR2RestoreUnlocksAfter32Days(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	clock := r.Now()
	r.Now = func() time.Time { return clock }
	r.DB.ReconcileR2(ctx, clock, 0, nil)
	r.DB.RequestR2Reseed(ctx, clock)
	bucket := newFakeBucket(t, r, nil)
	alerted := false
	clock = time.Date(2026, 12, 17, 0, 0, 0, 0, time.UTC).Add(-time.Second)
	if next := r.dailyStep(ctx, func(error) {}, &alerted); next != time.Hour || bucket.requests.Load() != 0 {
		t.Fatal("pause ended before the billing window rolled", next)
	}
	clock = clock.Add(time.Second)
	if next := r.dailyStep(ctx, func(error) {}, &alerted); next != 24*time.Hour {
		t.Fatal("pause did not end at the safety cap", next)
	}
	if s, _ := r.DB.R2Reseed(ctx); s.Pending {
		t.Fatal("expired marker kept", s)
	}
	if _, err := r.Put(ctx, testUpload); err != nil {
		t.Fatal("uploads stayed closed after the cap and a fresh inventory", err)
	}
}

// D-21 end to end: Daily re-seeds before its first inventory, without
// waiting on analytics past the timeout, and uploads then open normally.
func TestR2DailyReseedsThenOpensUploads(t *testing.T) {
	r := testR2(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.DB.RequestR2Reseed(ctx, r.Now())
	newFakeBucket(t, r, nil)
	fakeAnalytics(t, r, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(emptyAnalytics)) })
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
	if _, err := r.Put(context.Background(), testUpload); err != nil {
		t.Fatal("uploads stayed closed after the restore re-seed", err)
	}
}
