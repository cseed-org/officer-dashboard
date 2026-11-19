package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"testing"
)

func BenchmarkAgentJobs(b *testing.B) {
	e := newEnv(b)
	ctx := context.Background()
	token := "perf-service-token"
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		r, err := q.RequestByID(ctx, "req-0002")
		if err != nil {
			return err
		}
		for i := 0; i < 200; i++ {
			copy := *r
			copy.ID = fmt.Sprintf("perf-job-request-%d", i)
			copy.Status = "approved"
			copy.RequesterID = "user-priya"
			if _, err := q.InsertRequest(ctx, copy, 2027); err != nil {
				return err
			}
			if err := q.InsertJob(ctx, store.Job{ID: fmt.Sprintf("perf-job-%d", i), RequestID: copy.ID, Status: "queued", CreatedAt: 1, UpdatedAt: 1}); err != nil {
				return err
			}
		}
		return q.InsertToken(ctx, store.ServiceToken{ID: "perf-service", Name: "Performance", TokenHash: auth.SHA256Hex(token), Prefix: "perf", Scopes: []string{"agent:jobs"}, CreatedBy: "user-director", CreatedAt: 1})
	}); err != nil {
		b.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://localhost:8090/api/agent/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
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
