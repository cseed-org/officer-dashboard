package api

import (
	"fmt"
	"net/http/httptest"
	"os"
	"sort"
	"testing"
	"time"
)

// Read and list endpoints on the seeded database, through the whole handler stack
// (middleware, session lookup, SQL, JSON). `go test -bench . ./internal/api`
var benchPaths = []string{
	"/api/me", "/api/lookups", "/api/overview", "/api/events", "/api/events/event-kickoff", "/api/events/event-kickoff/metrics",
	"/api/requests?scope=all", "/api/requests/req-0002", "/api/finance/summary", "/api/finance/budgets",
	"/api/finance/reports/budget-vs-actual", "/api/finance/reports/statement-of-activities", "/api/search?q=demo",
}

func BenchmarkReadEndpoints(b *testing.B) {
	e := newEnv(b)
	ck := e.login("user-director")
	for _, p := range benchPaths {
		b.Run(p, func(b *testing.B) {
			req := httptest.NewRequest("GET", "http://localhost:8090"+p, nil)
			req.AddCookie(ck)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				e.h.ServeHTTP(rec, req)
				if rec.Code != 200 {
					b.Fatalf("%s → %d %s", p, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

// TestLatencyReport prints p50/p95/p99 per endpoint over 2,000 requests each.
// BENCH_LATENCY=1 go test -run LatencyReport -v ./internal/api
func TestLatencyReport(t *testing.T) {
	if os.Getenv("BENCH_LATENCY") == "" {
		t.Skip("set BENCH_LATENCY=1")
	}
	e := newEnv(t)
	ck := e.login("user-director")
	fmt.Printf("%-52s %8s %8s %8s\n", "endpoint (2000 requests, seeded data)", "p50", "p95", "p99")
	for _, p := range benchPaths {
		req := httptest.NewRequest("GET", "http://localhost:8090"+p, nil)
		req.AddCookie(ck)
		var d []time.Duration
		for i := 0; i < 2000; i++ {
			rec := httptest.NewRecorder()
			start := time.Now()
			e.h.ServeHTTP(rec, req)
			d = append(d, time.Since(start))
			if rec.Code != 200 {
				t.Fatalf("%s → %d", p, rec.Code)
			}
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		q := func(f float64) string {
			return fmt.Sprintf("%.2fms", float64(d[int(float64(len(d)-1)*f)].Microseconds())/1000)
		}
		fmt.Printf("%-52s %8s %8s %8s\n", p, q(.5), q(.95), q(.99))
	}
}
