package api

import (
	"context"
	"fmt"
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

func TestBuildspaceCalendarAggregatesBusyDaysWithCohortNavigation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 150; i++ {
			if err := q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: fmt.Sprintf("calendar-perf-%d", i), ProjectID: "bs-moss", Kind: "absence", Date: "2027-02-14", Body: "Private absence reason", Status: "on-track", Source: "manual", Visibility: "officers", CreatedAt: 1}); err != nil {
				return err
			}
			if err := q.Exec(ctx, `INSERT INTO buildspace_launchpad_people(id,name,project_id,cohort_id,joined_on) VALUES(?,?,?,?,?)`, fmt.Sprintf("calendar-builder-%d", i), fmt.Sprintf("Builder %d", i), "bs-moss", "cohort-fall-2026", "2026-10-01"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	who := e.officer()
	r := e.mustOK(e.get("/api/calendar/feed?start=2027-02-14&end=2027-02-15&sources=buildspace-absences,buildspace-weekly", who))
	if len(r.list("items")) > 4 || strings.Contains(string(r.Raw), "Private absence reason") {
		t.Fatal(string(r.Raw))
	}
	absence, weekly := false, false
	for _, v := range r.list("items") {
		it := v.(map[string]any)
		if it["source"] != "buildspace-absences" && it["source"] != "buildspace-weekly" {
			t.Fatal(it)
		}
		if !strings.Contains(it["href"].(string), "cohort=cohort-fall-2026") || it["date"] != "2027-02-14" || it["detail"] == nil {
			t.Fatal(it)
		}
		if it["source"] == "buildspace-absences" {
			absence = strings.Contains(it["title"].(string), "150")
		}
		if it["source"] == "buildspace-weekly" {
			weekly = true
		}
	}
	if !absence || !weekly {
		t.Fatal(string(r.Raw))
	}
}
