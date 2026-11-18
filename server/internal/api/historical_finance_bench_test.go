package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"officerdashboard/internal/store"
)

func seedHistoricalFinance(e *testEnv, requests, history int) {
	e.t.Helper()
	ctx := context.Background()
	err := e.db.Write(ctx, func(q *store.Queries) error {
		base, err := q.RequestByID(ctx, "req-0002")
		if err != nil {
			return err
		}
		for i := 0; i < requests; i++ {
			r := *base
			r.ID = fmt.Sprintf("finance-perf-%d", i)
			if _, err := q.InsertRequest(ctx, r, 2027); err != nil {
				return err
			}
		}
		for i := 0; i < history; i++ {
			id := fmt.Sprintf("finance-perf-%d", i%requests)
			before := `{"status":"approved"}`
			if err := q.InsertAudit(ctx, store.AuditEntry{ID: fmt.Sprintf("finance-audit-%d", i), Action: "request.changed", EntityType: "purchase_request", EntityID: &id, BeforeJSON: &before, AfterJSON: &before, CreatedAt: store.Ms(e.clock.UnixMilli())}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func BenchmarkHistoricalFinanceSummary(b *testing.B) {
	e := newEnv(b)
	seedHistoricalFinance(e, 300, 6000)
	req := httptest.NewRequest("GET", "http://localhost:8090/api/finance/summary?asOf=2026-09-30", nil)
	req.AddCookie(e.login("user-director"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			b.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestHistoricalFinanceDecodeBudget(t *testing.T) {
	e := newEnv(t)
	seedHistoricalFinance(e, 30, 600)
	req := httptest.NewRequest("GET", "http://localhost:8090/api/finance/summary?asOf=2026-09-30", nil)
	req.AddCookie(e.login("user-director"))
	allocations := testing.AllocsPerRun(2, func() {
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
	})
	if allocations > 25000 {
		t.Fatalf("historical finance repeatedly decoded before images: %.0f allocations", allocations)
	}
}
