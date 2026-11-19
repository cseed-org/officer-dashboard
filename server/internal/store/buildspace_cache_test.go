package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func cachedStatementCount(d *DB) int {
	d.cache.mu.Lock()
	defer d.cache.mu.Unlock()
	return len(d.cache.stmts)
}

// Roster-derived scopes must have fixed SQL text: the cache is keyed by that text and
// retains every distinct statement until the database closes.
func TestBuildspaceStatementCacheConstantAcrossRosterGrowth(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Write(ctx, func(q *Queries) error {
		return q.SaveLaunchpadCohort(ctx, LaunchpadCohort{ID: "c1", Name: "C", StartsOn: "2026-09-01", Status: "active", CreatedAt: 1, UpdatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	var initial int
	for i := 0; i < 30; i++ {
		if err := d.Write(ctx, func(q *Queries) error {
			pid := fmt.Sprintf("p%02d", i)
			if err := q.SaveBuildspaceProject(ctx, BuildspaceProject{ID: pid, Name: pid, Track: "Software", Stage: "Building", Links: []string{}, MentorIDs: []string{}, CohortID: "c1", UpdatedAt: 1}); err != nil {
				return err
			}
			return q.SaveBuildspacePerson(ctx, BuildspacePerson{ID: fmt.Sprintf("b%02d", i), Name: "B", ProjectID: pid, JoinedOn: "2026-09-01", CohortID: "c1"})
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Read().BuildspaceCohortPage(ctx, "c1", BuildspaceActivityPage{WeekStart: "2026-11-16", WeekDue: "2026-11-22", FilterArchived: true}); err != nil {
			t.Fatal(err)
		}
		size := cachedStatementCount(d)
		if i == 0 {
			initial = size
		} else if size != initial {
			t.Fatalf("statement cache grew from %d to %d at roster step %d", initial, size, i+1)
		}
	}
}

// The delivered-page limit is a bound argument too, so alternate page sizes share one
// statement for each cursor shape instead of adding another permanent cache entry.
func TestDesignDeliveredStatementCacheConstantAcrossPageSizes(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	initial := -1
	for i, limit := range []int{1, 2, 3, 4, 5, 6} {
		if _, _, err := d.Read().DesignDeliveredPage(ctx, false, 0, "", limit); err != nil {
			t.Fatal(err)
		}
		size := cachedStatementCount(d)
		if i == 0 {
			initial = size
		} else if size != initial {
			t.Fatalf("statement cache grew from %d to %d at page size %d", initial, size, limit)
		}
	}
}

func TestActivityIDScopeQueryPlansUseIndexes(t *testing.T) {
	x := newTDB(t)
	for _, tc := range []struct {
		column string
		index  string
	}{
		{"project_id", "buildspace_launchpad_activity_project"},
		{"person_id", "buildspace_launchpad_activity_person"},
	} {
		scope, args := activityIDScope(tc.column, []string{"one", "two"})
		query := `SELECT a.id FROM buildspace_launchpad_activity a WHERE 1=1` + scope
		plan := explain(t, x.db, query, args...)
		if !strings.Contains(plan, "SEARCH a USING INDEX "+tc.index+" ("+tc.column+"=?)") {
			t.Errorf("%s scope did not use %s: %s", tc.column, tc.index, plan)
		}
	}
}
