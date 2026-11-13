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
	rows, err := q.q.QueryContext(ctx, `SELECT a.id,a.date,p.id,p.name,coalesce(b.name,'') FROM buildspace_launchpad_activity a
 JOIN buildspace_launchpad_projects p ON p.id=a.project_id LEFT JOIN buildspace_launchpad_people b ON b.id=a.person_id
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
		v.Href = "/buildspace-launchpad?project=" + url.QueryEscape(projectID)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Weekly due items follow the configurable due-day rules (buildspace_launchpad_schedule.go): one
// item per builder for each week whose due date falls in the window. A dated weekly entry
// in that week suppresses it; builders not yet joined and shipped projects are excluded.
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
	type builder struct{ id, name, project, joined string }
	rows, err := q.q.QueryContext(ctx, `SELECT b.id,b.name,p.name,b.joined_on FROM buildspace_launchpad_people b
 JOIN buildspace_launchpad_projects p ON p.id=b.project_id WHERE p.stage<>'Shipped' ORDER BY b.id`)
	if err != nil {
		return nil, err
	}
	builders := []builder{}
	for rows.Next() {
		var v builder
		if err := rows.Scan(&v.id, &v.name, &v.project, &v.joined); err != nil {
			rows.Close()
			return nil, err
		}
		builders = append(builders, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT person_id,date FROM buildspace_launchpad_activity WHERE kind='weekly' AND date>=? AND date<=?`, weeks[0].Start, weeks[len(weeks)-1].Due)
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
		label := "Weekly check-in due"
		if t := BuildspaceRuleFor(rules, w.Due).DueTime; t != nil {
			label += " " + *t
		}
		for _, b := range builders {
			if b.joined > w.Due || done[b.id+"|"+w.Due] {
				continue
			}
			out = append(out, CalendarItem{ID: "buildspace-weekly-" + b.id + "-" + w.Due, Date: w.Due, Type: "weekly-check-in",
				Title: label + " · " + b.name + " · " + b.project, Href: "/buildspace-launchpad?person=" + url.QueryEscape(b.id)})
		}
	}
	return out, nil
}
