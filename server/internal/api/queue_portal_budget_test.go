package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"testing"
	"time"
)

func budgetAgentRequest(e *testEnv) *http.Request {
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
		e.t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://localhost:8090/api/agent/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
func budgetPortalRequest(e *testEnv) *http.Request {
	ctx := context.Background()
	// Activity in three cohorts, with full bodies, exercises the history scope.
	if _, err := e.db.W.Exec(`INSERT INTO buildspace_launchpad_cohorts(id,name,starts_on,ends_on,status,created_at,updated_at) VALUES('perf-cohort','Past cohort','2025-01-01','2025-03-01','archived',1,1)`); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.db.W.Exec(`INSERT INTO buildspace_launchpad_projects(id,name,cohort_id,track,stage,updated_at) VALUES('perf-project','Past project','perf-cohort','Software','Building',1)`); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 5000; i++ {
			project := []string{"perf-project", "bs-lumen", "bs-sprout"}[i%3]
			if err := q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: fmt.Sprintf("perf-activity-%d", i), ProjectID: project, Kind: "absence", Date: "2026-10-20", Body: "Synthetic history", Status: "on-track", Source: "manual", Visibility: "team", CreatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	// Portal cookie creation does not require a *testing.T.
	token := "portal-perf"
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.InsertLaunchpadSession(ctx, auth.SHA256Hex(token), "lp-account-alex", e.clock.Add(24*time.Hour).UnixMilli(), "perf", e.clock.UnixMilli())
	}); err != nil {
		e.t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://localhost:8090/api/portal/builder", nil)
	req.AddCookie(&http.Cookie{Name: e.srv.portalCookieName(), Value: token})
	return req
}

func TestAgentJobsAllocationBudget(t *testing.T) {
	e := newEnv(t)
	req := budgetAgentRequest(e)
	allocations := testing.AllocsPerRun(2, func() {
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})
	if allocations > 40000 {
		t.Fatalf("AgentJobs retained growing per-request work: %.0f allocations", allocations)
	}
}

func TestPortalBuilderAllocationBudget(t *testing.T) {
	e := newEnv(t)
	req := budgetPortalRequest(e)
	allocations := testing.AllocsPerRun(2, func() {
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})
	if allocations > 15000 {
		t.Fatalf("PortalBuilder retained growing per-request work: %.0f allocations", allocations)
	}
}
