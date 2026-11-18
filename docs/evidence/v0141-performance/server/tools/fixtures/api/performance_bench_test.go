package api
import ("context";"fmt";"net/http";"net/http/httptest";"testing";"time";"officerdashboard/internal/auth";"officerdashboard/internal/cloudflare";"officerdashboard/internal/store")
func BenchmarkMemberOfficers(b *testing.B) {
	e := newEnv(b)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 5000; i++ {
			email := fmt.Sprintf("member-%d@uw.edu", i)
			if err := q.SaveMember(ctx, cloudflare.Person{ID: fmt.Sprint(i), FirstName: "Member", UWEmail: &email, JoinDate: "2026-01-01"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	c := &Ctx{S: e.srv}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.memberOfficers(ctx, e.db.Read()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPortalBuilder(b *testing.B) {
	e := newEnv(b)
	ctx := context.Background()
	// Activity in three cohorts, with full bodies, exercises the history scope.
	if _, err := e.db.W.Exec(`INSERT INTO buildspace_launchpad_cohorts(id,name,starts_on,ends_on,status,created_at,updated_at) VALUES('perf-cohort','Past cohort','2025-01-01','2025-03-01','archived',1,1)`); err != nil {
		b.Fatal(err)
	}
	if _, err := e.db.W.Exec(`INSERT INTO buildspace_launchpad_projects(id,name,cohort_id,track,stage,updated_at) VALUES('perf-project','Past project','perf-cohort','Software','Building',1)`); err != nil {
		b.Fatal(err)
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
		b.Fatal(err)
	}
	// Portal cookie creation does not require a *testing.T.
	token := "portal-perf"
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.InsertLaunchpadSession(ctx, auth.SHA256Hex(token), "lp-account-alex", e.clock.Add(24*time.Hour).UnixMilli(), "perf", e.clock.UnixMilli())
	}); err != nil {
		b.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://localhost:8090/api/portal/builder", nil)
	req.AddCookie(&http.Cookie{Name: e.srv.portalCookieName(), Value: token})
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
