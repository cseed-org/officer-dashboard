package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
)

type countQueries struct {
	store.Q
	calls int
}

func (q *countQueries) QueryContext(ctx context.Context, s string, args ...any) (*sql.Rows, error) {
	q.calls++
	return q.Q.QueryContext(ctx, s, args...)
}
func (q *countQueries) QueryRowContext(ctx context.Context, s string, args ...any) *sql.Row {
	q.calls++
	return q.Q.QueryRowContext(ctx, s, args...)
}

func TestJobDetailsConstantQueriesAndParity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := e.db.Read()
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		t.Fatal(err)
	}
	c := &Ctx{S: e.srv, R: httptest.NewRequest("GET", "http://localhost:8090", nil)}
	// Existing seed covers receipts, approvals, posted entries and held settlements.
	requests, err := q.AllRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var jobs []store.Job
	for _, r := range requests {
		jobs = append(jobs, store.Job{ID: r.ID, RequestID: r.ID, Status: "held"})
	}
	got, err := c.jobViews(cat, q, jobs, "")
	if err != nil {
		t.Fatal(err)
	}
	for i, j := range jobs {
		want, err := c.jobView(cat, q, &j)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got[i])
		if string(a) != string(b) {
			t.Fatalf("%s\n%s\n%s", j.ID, a, b)
		}
	}
	// Grow actual independent requests, rather than repeating a single ID in the batch.
	base := requests[0]
	for i := 0; i < 200; i++ {
		r := base
		r.ID = fmt.Sprintf("perf-request-%03d", i)
		r.Status = "approved"
		r.RequesterID = "user-priya"
		if err := e.db.Write(ctx, func(q *store.Queries) error { _, err := q.InsertRequest(ctx, r, 2027); return err }); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, store.Job{ID: r.ID, RequestID: r.ID, Status: "queued"})
	}
	jobs = jobs[len(jobs)-200:]
	for _, n := range []int{1, 50, 200} {
		counted := &countQueries{Q: e.db.R}
		out, err := c.jobViews(cat, store.On(counted), jobs[:n], "user-director")
		if err != nil || len(out) != n || counted.calls != 5 {
			t.Fatalf("n=%d calls=%d rows=%d err=%v", n, counted.calls, len(out), err)
		}
		own, err := c.jobViews(cat, q, jobs[:n], "user-priya")
		if err != nil || len(own) != 0 {
			t.Fatalf("creator filter: %d %v", len(own), err)
		}
	}
}

func TestDesignDeliveredHistoryBoundedAndComplete(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := e.officer()
	before := e.mustOK(e.get("/api/design-requests", who))
	active := map[string]bool{}
	for _, v := range before.list("items") {
		r := v.(map[string]any)
		if r["stage"] != "delivered" {
			active[r["id"].(string)] = true
		}
	}
	err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 125; i++ {
			if err := q.InsertDesignRequest(ctx, store.DesignRequest{ID: fmt.Sprintf("perf-design-%03d", i), Title: "Past work", Brief: "Past request", Type: "poster", Stage: "delivered", Urgency: "normal", RequesterID: "user-priya", EventID: strp("event-hack-night"), UpdatedAt: 1, CreatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	total := e.scalar(`SELECT count(*) FROM design_requests WHERE stage='delivered'`).(int64)
	seen := map[string]bool{}
	cursor := ""
	first := true
	for {
		path := "/api/design-requests"
		if !first {
			path += "?deliveredOnly=true&deliveredCursor=" + cursor
		}
		r := e.mustOK(e.get(path, who))
		if got := r.num("deliveredTotal"); got != total {
			t.Fatalf("delivered total = %d, want %d", got, total)
		}
		for _, v := range r.list("upcomingEvents") {
			event := v.(map[string]any)
			deliveredLinks := 0
			for _, v := range event["requests"].([]any) {
				if v.(map[string]any)["stage"] == "delivered" {
					deliveredLinks++
				}
			}
			if deliveredLinks > 6 {
				t.Fatal("delivered history escaped through rail", deliveredLinks)
			}
		}
		delivered := 0
		for _, v := range r.list("items") {
			item := v.(map[string]any)
			id := item["id"].(string)
			if item["stage"] != "delivered" {
				if !first || !active[id] {
					t.Fatal(item)
				}
				delete(active, id)
				continue
			}
			delivered++
			if seen[id] {
				t.Fatalf("repeated %s", id)
			}
			seen[id] = true
		}
		if delivered > 6 {
			t.Fatalf("unbounded delivered page: %d", delivered)
		}
		if first && len(active) > 0 {
			t.Fatal("active cards lost", active)
		}
		cursor = r.str("deliveredNextCursor")
		if cursor == "" {
			break
		}
		first = false
	}
	if len(seen) < 125 {
		t.Fatalf("history lost: %d", len(seen))
	}
}

func TestMemberOfficersAllEmailKinds(t *testing.T) {
	for _, kind := range []string{"uw", "cs", "personal"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			email := "PRIYA@uw.example"
			if _, err := e.db.W.Exec(`UPDATE users SET email='priya@uw.example' WHERE id='user-priya'`); err != nil {
				t.Fatal(err)
			}
			p := cloudflare.Person{ID: "perf-member", FirstName: "Priya", JoinDate: "2026-01-01"}
			switch kind {
			case "uw":
				p.UWEmail = &email
			case "cs":
				p.CSEmail = &email
			case "personal":
				p.PersonalEmail = &email
			}
			if err := e.db.Write(ctx, func(q *store.Queries) error { return q.SaveMember(ctx, p) }); err != nil {
				t.Fatal(err)
			}
			c := &Ctx{S: e.srv}
			officers, err := c.memberOfficers(ctx, e.db.Read())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, o := range officers {
				if o.ID == "user-priya" {
					found = o.MemberID != nil && *o.MemberID == p.ID
				}
			}
			if !found {
				t.Fatal(officers)
			}
		})
	}
}

func BenchmarkMemberOfficers(b *testing.B) {
	e := newEnv(b)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 5000; i++ {
			email := fmt.Sprintf("member-%d@uw.example", i)
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

func TestScopedBuildspaceActivityParity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := e.db.Read()
	full, err := q.Buildspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, co := range full.Cohorts {
		scoped, err := q.BuildspaceForCohort(ctx, co.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := launchpadView(scoped, co.ID, true, e.srv.nowMs())
		if err != nil {
			t.Fatal(err)
		}
		want, err := launchpadView(full, co.ID, true, e.srv.nowMs())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cohort %s changed", co.ID)
		}
		for _, a := range scoped.Activity {
			if scoped.Project(a.ProjectID).CohortID != co.ID {
				t.Fatal("unrelated history read")
			}
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
