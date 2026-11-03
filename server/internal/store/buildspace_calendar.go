package store

import (
	"context"
	"net/url"
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
	rows, err := q.q.QueryContext(ctx, `SELECT a.id,a.date,p.id,p.name,coalesce(b.name,'') FROM buildspace_activity a
 JOIN buildspace_projects p ON p.id=a.project_id LEFT JOIN buildspace_people b ON b.id=a.person_id
 WHERE a.kind=? AND a.date>=? AND a.date<? ORDER BY a.date,a.id`, kind, window.FirstDate, window.EndDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarItem{}
	for rows.Next() {
		var v CalendarItem
		var projectID, projectName, personName string
		if err := rows.Scan(&v.ID, &v.Date, &projectID, &projectName, &personName); err != nil {
			return nil, err
		}
		v.ID = "buildspace-" + kind + "-" + v.ID
		v.Type = kind
		v.Title = label + " · " + projectName
		if personName != "" {
			v.Title = label + " · " + personName + " · " + projectName
		}
		v.Href = "/buildspace?project=" + url.QueryEscape(projectID)
		out = append(out, v)
	}
	return out, rows.Err()
}

// The optional calendar convention is Sunday due dates for Monday–Sunday weeks.
// Existing check-ins suppress that week's due item, and shipped projects are excluded.
// This is independent of the People directory's in-progress “awaiting” signal.
func loadBuildspaceWeeklyDue(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	rows, err := q.q.QueryContext(ctx, `WITH RECURSIVE dates(day) AS (
 SELECT ? WHERE ? < ? UNION ALL SELECT date(day,'+1 day') FROM dates WHERE date(day,'+1 day') < ?
 ) SELECT b.id,b.name,p.name,d.day FROM dates d CROSS JOIN buildspace_people b
 JOIN buildspace_projects p ON p.id=b.project_id
 WHERE strftime('%w',d.day)='0' AND b.joined_on<=d.day AND p.stage<>'Shipped'
 AND NOT EXISTS (SELECT 1 FROM buildspace_activity a WHERE a.person_id=b.id AND a.kind='weekly'
 AND a.date>=date(d.day,'-6 days') AND a.date<=d.day) ORDER BY d.day,b.id`, window.FirstDate, window.FirstDate, window.EndDate, window.EndDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarItem{}
	for rows.Next() {
		var v CalendarItem
		var personID, personName, projectName string
		if err := rows.Scan(&personID, &personName, &projectName, &v.Date); err != nil {
			return nil, err
		}
		v.ID = "buildspace-weekly-" + personID + "-" + v.Date
		v.Type = "weekly-check-in"
		v.Title = "Weekly check-in due · " + personName + " · " + projectName
		v.Href = "/buildspace?person=" + url.QueryEscape(personID)
		out = append(out, v)
	}
	return out, rows.Err()
}
