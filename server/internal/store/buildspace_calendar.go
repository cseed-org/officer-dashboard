package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
)

// Calendar titles deliberately omit note bodies and absence reasons. All dashboard
// officers can read the Buildspace cohort; these sources share that access boundary.
func loadBuildspaceAbsences(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	return loadBuildspaceCalendarActivity(ctx, q, window, "absence", "Absence")
}
func loadBuildspaceMentorCheckIns(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	return loadBuildspaceCalendarActivity(ctx, q, window, "mentor", "Mentor check-in")
}
func loadBuildspaceCalendarActivity(ctx context.Context, q *Queries, window CalendarRange, kind, label string) ([]CalendarItem, error) {
	rows, err := q.q.QueryContext(ctx, `WITH entries AS (
 SELECT a.id,a.date,p.id project_id,p.name project_name,coalesce(b.name,'') person_name,p.cohort_id
 FROM buildspace_launchpad_activity a JOIN buildspace_launchpad_projects p ON p.id=a.project_id
 LEFT JOIN buildspace_launchpad_people b ON b.id=a.person_id JOIN buildspace_launchpad_cohorts c ON c.id=p.cohort_id
 WHERE a.kind=? AND a.date>=? AND a.date<? AND p.archived_at IS NULL AND c.status='active' AND (b.id IS NULL OR b.archived_at IS NULL)
 ), totals AS (SELECT cohort_id,date,count(*) n FROM entries GROUP BY cohort_id,date)
 SELECT e.*,t.n FROM entries e JOIN totals t USING(cohort_id,date) WHERE t.n<=20
 UNION ALL
 SELECT min(e.id),e.date,'','','',e.cohort_id,t.n FROM entries e JOIN totals t USING(cohort_id,date) WHERE t.n>20 GROUP BY e.cohort_id,e.date
 ORDER BY date,id`, kind, window.FirstDate, window.EndDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarItem{}
	for rows.Next() {
		var v CalendarItem
		var projectID, projectName, personName, cohortID string
		var count int
		if err := rows.Scan(&v.ID, &v.Date, &projectID, &projectName, &personName, &cohortID, &count); err != nil {
			return nil, err
		}
		v.ID = "buildspace-" + kind + "-" + v.ID
		v.Type = kind
		v.Title = label + " · " + projectName
		if personName != "" {
			v.Title = label + " · " + personName + " · " + projectName
		}
		v.Href = "/buildspace-launchpad?project=" + url.QueryEscape(projectID)
		if count > 20 {
			v.ID = calendarGroupID(kind, cohortID, v.Date)
			v.Title = fmt.Sprintf("%d %s entries", count, label)
			v.Href = "/buildspace-launchpad?cohort=" + url.QueryEscape(cohortID)
			detail := fmt.Sprintf("Open this cohort to view all %d entries.", count)
			v.Detail = &detail
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Weekly due items follow the configurable due-day rules (buildspace_schedule.go): one
// item per builder for each week whose due date falls in the window. A dated weekly entry
// in that week suppresses it; builders not yet joined, shipped or archived projects, archived
// builders and archived cohorts are excluded.
// Titles carry the due time when one is set. This is independent of the People
// directory's in-progress “awaiting” signal.
func loadBuildspaceWeeklyDue(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	out := []CalendarItem{}
	rules, err := q.BuildspaceWeekRules(ctx)
	if err != nil {
		return nil, err
	}
	weeks := BuildspaceWeeksDue(rules, window.FirstDate, window.EndDate)
	if len(weeks) == 0 {
		return out, nil
	}
	type builder struct{ id, name, project, joined, cohort string }
	rows, err := q.q.QueryContext(ctx, `SELECT b.id,b.name,p.name,b.joined_on,p.cohort_id FROM buildspace_launchpad_people b
 JOIN buildspace_launchpad_projects p ON p.id=b.project_id JOIN buildspace_launchpad_cohorts c ON c.id=p.cohort_id
 WHERE p.stage<>'Shipped' AND p.archived_at IS NULL AND b.archived_at IS NULL AND c.status='active' ORDER BY b.id`)
	if err != nil {
		return nil, err
	}
	builders := []builder{}
	for rows.Next() {
		var v builder
		if err := rows.Scan(&v.id, &v.name, &v.project, &v.joined, &v.cohort); err != nil {
			rows.Close()
			return nil, err
		}
		builders = append(builders, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT DISTINCT a.person_id,a.date FROM buildspace_launchpad_activity a JOIN buildspace_launchpad_people b ON b.id=a.person_id JOIN buildspace_launchpad_projects p ON p.id=b.project_id JOIN buildspace_launchpad_cohorts c ON c.id=p.cohort_id WHERE a.kind='weekly' AND a.date>=? AND a.date<=? AND b.archived_at IS NULL AND p.archived_at IS NULL AND p.stage<>'Shipped' AND c.status='active'`, weeks[0].Start, weeks[len(weeks)-1].Due)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for rows.Next() {
		var person, date string
		if err := rows.Scan(&person, &date); err != nil {
			rows.Close()
			return nil, err
		}
		done[person+"|"+BuildspaceWeekOf(rules, date).Due] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, w := range weeks {
		type group struct {
			count int
			items []CalendarItem
		}
		groups := map[string]*group{}

		label := "Weekly check-in due"
		if t := BuildspaceRuleFor(rules, w.Due).DueTime; t != nil {
			label += " " + *t
		}
		for _, b := range builders {
			if b.joined > w.Due || done[b.id+"|"+w.Due] {
				continue
			}
			g := groups[b.cohort]
			if g == nil {
				g = &group{}
				groups[b.cohort] = g
			}
			g.count++
			if g.count <= 20 {
				g.items = append(g.items, CalendarItem{ID: weeklyItemID(b.id, w.Due), Date: w.Due, Type: "weekly-check-in", Title: label + " · " + b.name + " · " + b.project, Href: "/buildspace-launchpad?person=" + url.QueryEscape(b.id)})
			}
		}
		cohorts := make([]string, 0, len(groups))
		for id := range groups {
			cohorts = append(cohorts, id)
		}
		sort.Strings(cohorts)
		for _, id := range cohorts {
			g := groups[id]
			if g.count <= 20 {
				out = append(out, g.items...)
				continue
			}
			detail := fmt.Sprintf("Open this cohort to view all %d builders' check-ins.", g.count)
			out = append(out, CalendarItem{ID: calendarGroupID("weekly", id, w.Due), Date: w.Due, Type: "weekly-check-in", Title: fmt.Sprintf("%d weekly check-ins due", g.count), Href: "/buildspace-launchpad?cohort=" + url.QueryEscape(id), Detail: &detail})
		}
	}

	return out, nil
}

func calendarGroupID(kind, cohort, date string) string {
	sum := sha256.Sum256([]byte(cohort + "|" + date))
	return fmt.Sprintf("buildspace-%s-summary-%x", kind, sum[:8])
}

// calendarItemIDMax is CalendarFeedItem.id's length limit in the API contract.
const calendarItemIDMax = 64

// weeklyItemID is "buildspace-weekly-<builder>-<due date>". A builder ID long enough to
// break the 64-character limit (officer-created builders use 36-character UUIDs) is
// replaced by a stable digest of it; the item's href keeps the real ID.
func weeklyItemID(builderID, due string) string {
	const prefix = "buildspace-weekly-"
	if id := prefix + builderID + "-" + due; len(id) <= calendarItemIDMax {
		return id
	}
	sum := sha256.Sum256([]byte(builderID))
	return prefix + "h" + hex.EncodeToString(sum[:8]) + "-" + due
}
