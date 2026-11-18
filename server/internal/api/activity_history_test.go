package api

import (
	"context"
	"fmt"
	"net/url"
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

func TestBuildspaceHistoryPagesBoundBodiesAndRetainSignals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 601; i++ {
			if err := q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: fmt.Sprintf("paged-%04d", i), ProjectID: "bs-moss", Kind: "absence", Date: "2026-10-20", Body: strings.Repeat("history ", 125), Status: "resolved", Source: "manual", Visibility: "team", CreatedAt: 1}); err != nil {
				return err
			}
		}
		for _, a := range []store.BuildspaceActivity{
			{ID: "paged-open", ProjectID: "bs-moss", Kind: "request", Date: "2026-01-01", Body: "Still open", Status: "open", Source: "manual", Visibility: "team", CreatedAt: 1},
			{ID: "paged-private", ProjectID: "bs-moss", Kind: "absence", Date: "2026-10-20", Body: "Officers only", Status: "resolved", Source: "manual", Visibility: "officers", CreatedAt: 1},
		} {
			if err := q.InsertBuildspaceActivity(ctx, a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path    string
		who     actor
		private bool
	}{
		{"/api/buildspace-launchpad", e.officer(), true},
		{"/api/portal/builder", e.portalLogin("lp-account-alex"), false},
	} {
		seen := map[string]bool{}
		cursor := ""
		pages := 0
		for {
			path := tc.path
			if cursor != "" {
				path += "?activityCursor=" + url.QueryEscape(cursor)
			}
			r := e.mustOK(e.get(path, tc.who))
			pages++
			if len(r.list("activity")) > 225 || len(r.Raw) > 400000 {
				t.Fatalf("unbounded page: %d rows %d bytes", len(r.list("activity")), len(r.Raw))
			}
			open := false
			for _, v := range r.list("activity") {
				a := v.(map[string]any)
				id := a["id"].(string)
				if id == "paged-open" {
					open = true
					continue
				}
				if !strings.HasPrefix(id, "paged-") {
					continue
				}
				if !tc.private && id == "paged-private" {
					t.Fatal("private history leaked")
				}
				if seen[id] {
					t.Fatalf("duplicate history %s", id)
				}
				seen[id] = true
			}
			if !open {
				t.Fatal("open request disappeared on a history page")
			}
			cursor = r.str("activityNextCursor")
			if cursor == "" {
				break
			}
			if pages > 5 {
				t.Fatal("cursor did not advance")
			}
		}
		want := 601
		if tc.private {
			want++
		}
		if len(seen) != want || pages < 4 {
			t.Fatalf("coverage=%d pages=%d", len(seen), pages)
		}
	}
}

func TestBuilderPreloadedRegisterNeedsNoQueries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := e.db.Read()
	account, err := q.LaunchpadAccount(ctx, "lp-account-alex")
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.BuildspaceForCohort(ctx, *account.CohortID)
	if err != nil {
		t.Fatal(err)
	}
	counted := &countQueries{Q: e.db.R}
	if _, err := loadBuilder(ctx, store.On(counted), &portalSession{account: account}, &b); err != nil {
		t.Fatal(err)
	}
	if counted.calls != 0 {
		t.Fatal("preloaded register re-read", counted.calls)
	}
	counted.calls = 0
	archived, err := store.On(counted).LaunchpadPersonArchived(ctx, *account.PersonID)
	if err != nil || archived || counted.calls != 1 {
		t.Fatalf("archive check: %t %d %v", archived, counted.calls, err)
	}
}

func TestPrivateWeeklyCompletionSurvivesPortalHistoryFilter(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	person := "bs-person-01"
	e.exec(`DELETE FROM buildspace_launchpad_activity WHERE person_id=? AND kind='weekly'`, person)
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: "private-weekly", ProjectID: "bs-moss", PersonID: &person, Kind: "weekly", Date: "2026-10-26", Body: "Private body", Status: "on-track", Source: "manual", Visibility: "officers", CreatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/portal/builder", e.portalLogin("lp-account-alex")))
	if r.str("thisWeek") != "done" || strings.Contains(string(r.Raw), "Private body") || strings.Contains(string(r.Raw), "private-weekly") {
		t.Fatal(string(r.Raw))
	}
}

func TestOnboardingHasNoOtherProjectsHistoryCursor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.exec(`UPDATE buildspace_launchpad_accounts SET person_id=NULL,onboarded_at=NULL WHERE id='lp-account-alex'`)
	e.exec(`INSERT INTO buildspace_launchpad_people(id,name,project_id,cohort_id,joined_on) VALUES('no-person','Another builder','bs-moss','cohort-fall-2026','2026-10-01')`)
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 201; i++ {
			if err := q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: fmt.Sprintf("onboarding-history-%d", i), ProjectID: "bs-moss", Kind: "absence", Date: "2026-10-20", Body: "Team history", Status: "resolved", Source: "manual", Visibility: "team", CreatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/portal/builder", e.portalLogin("lp-account-alex")))
	if r.at("onboarded") != false || len(r.list("activity")) != 0 || r.str("activityNextCursor") != "" {
		t.Fatal(string(r.Raw))
	}
}
