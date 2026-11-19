package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// BuildspaceActivityPage always retains current-week activity and open requests so
// dashboard signals remain complete. Only completed history is paged.
type BuildspaceActivityPage struct {
	WeekStart, WeekDue string
	Cursor             string
	PersonID, MentorID string
	Visibility         []string
	FilterArchived     bool
	SkipActivity       bool
}

func (q *Queries) BuildspaceRoster(ctx context.Context) (Buildspace, error) {
	return q.buildspace(ctx, nil, &BuildspaceActivityPage{SkipActivity: true})
}

func (q *Queries) BuildspaceCohortPage(ctx context.Context, cohort string, page BuildspaceActivityPage) (Buildspace, error) {
	return q.buildspace(ctx, &cohort, &page)
}
func (q *Queries) BuildspaceMentorPage(ctx context.Context, page BuildspaceActivityPage) (Buildspace, error) {
	return q.buildspace(ctx, nil, &page)
}

// Check-in completion is visible independently of a note's body visibility. Read
// only person IDs, keeping hidden notes out of both the payload and history cursor.
func weeklyDoneScope(scope string, args []any, p BuildspaceActivityPage) (string, []any) {
	args = append(append([]any{}, args...), p.WeekStart, p.WeekDue)
	scope += ` AND a.kind='weekly' AND a.person_id IS NOT NULL AND a.date>=? AND a.date<=?`
	if p.PersonID != "" {
		scope += ` AND a.person_id=?`
		args = append(args, p.PersonID)
	}
	if p.MentorID != "" {
		scope += ` AND a.project_id IN (SELECT project_id FROM buildspace_launchpad_mentors WHERE contact_id=?)`
		args = append(args, p.MentorID)
	}
	return scope, args
}

func activityScope(scope string, args []any, p BuildspaceActivityPage) (string, []any) {
	visibility, visibilityArgs := activityVisibility(p)
	scope += visibility
	args = append(args, visibilityArgs...)
	if p.FilterArchived {
		scope += ` AND a.project_id IN (SELECT id FROM buildspace_launchpad_projects WHERE archived_at IS NULL) AND (a.person_id IS NULL OR a.person_id IN (SELECT id FROM buildspace_launchpad_people WHERE archived_at IS NULL))`
	}
	if p.PersonID != "" {
		scope += ` AND a.project_id=(SELECT project_id FROM buildspace_launchpad_people WHERE id=?)`
		args = append(args, p.PersonID)
	}
	if p.MentorID != "" {
		scope += ` AND a.project_id IN (SELECT m.project_id FROM buildspace_launchpad_mentors m JOIN buildspace_launchpad_projects pr ON pr.id=m.project_id JOIN buildspace_launchpad_cohorts co ON co.id=pr.cohort_id WHERE m.contact_id=? AND pr.archived_at IS NULL AND co.status='active')`
		args = append(args, p.MentorID)
	}
	return scope, args
}

func activityVisibility(p BuildspaceActivityPage) (string, []any) {
	if len(p.Visibility) == 0 {
		return "", nil
	}
	literals := make([]string, 0, len(p.Visibility))
	for _, visibility := range p.Visibility {
		switch visibility {
		case "officers", "mentors", "team":
			literals = append(literals, "'"+visibility+"'")
		default:
			raw, _ := json.Marshal(p.Visibility)
			return ` AND a.visibility IN (SELECT value FROM json_each(?))`, []any{string(raw)}
		}
	}
	return ` AND a.visibility IN (` + strings.Join(literals, ",") + `)`, nil
}

// lastActivityFromRows avoids another query when the response already contains
// every visible activity row (the normal small-cohort case).
func lastActivityFromRows(b *Buildspace) {
	project, projectWide, own := map[string]string{}, map[string]string{}, map[string]string{}
	for _, a := range b.Activity {
		if a.Date > project[a.ProjectID] {
			project[a.ProjectID] = a.Date
		}
		if a.PersonID == nil {
			if a.Date > projectWide[a.ProjectID] {
				projectWide[a.ProjectID] = a.Date
			}
		} else if a.Date > own[*a.PersonID] {
			own[*a.PersonID] = a.Date
		}
	}
	for i := range b.Projects {
		if date := project[b.Projects[i].ID]; date != "" {
			b.Projects[i].LastActivityDate = &date
		}
	}
	for i := range b.People {
		date := projectWide[b.People[i].ProjectID]
		if own[b.People[i].ID] > date {
			date = own[b.People[i].ID]
		}
		if date != "" {
			b.People[i].LastActivityDate = &date
		}
	}
}

// lastActivityDates uses the existing single-column indexes without SQLite's
// temporary composite GROUP BY table. Mentor cards do not need person maxima.
func (q *Queries) lastActivityDates(ctx context.Context, scope string, args []any, p BuildspaceActivityPage, b *Buildspace) error {
	rows, err := q.query(ctx, `SELECT a.project_id,max(a.date),max(CASE WHEN a.person_id IS NULL THEN a.date END) FROM buildspace_launchpad_activity a`+scope+` GROUP BY a.project_id`, args...)
	if err != nil {
		return err
	}
	projectWide := map[string]string{}
	for rows.Next() {
		var projectID, date string
		var wide *string
		if err := rows.Scan(&projectID, &date, &wide); err != nil {
			rows.Close()
			return err
		}
		for i := range b.Projects {
			if b.Projects[i].ID == projectID {
				b.Projects[i].LastActivityDate = &date
				break
			}
		}
		if wide != nil {
			projectWide[projectID] = *wide
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for i := range b.People {
		if date := projectWide[b.People[i].ProjectID]; date != "" {
			b.People[i].LastActivityDate = &date
		}
	}
	if p.MentorID != "" {
		return nil
	}
	rows, err = q.query(ctx, `SELECT a.person_id,max(a.date) FROM buildspace_launchpad_activity a`+scope+` AND a.person_id IS NOT NULL GROUP BY a.person_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var personID, date string
		if err := rows.Scan(&personID, &date); err != nil {
			return err
		}
		for i := range b.People {
			if b.People[i].ID == personID {
				if projectWide[b.People[i].ProjectID] > date {
					date = projectWide[b.People[i].ProjectID]
				}
				b.People[i].LastActivityDate = &date
				break
			}
		}
	}
	return rows.Err()
}

func (q *Queries) activityPageWhere(ctx context.Context, scope string, args []any, weeklyScope string, weeklyArgs []any, p BuildspaceActivityPage) ([]BuildspaceActivity, *string, map[string]bool, error) {
	// The second open condition retains mentor signals that use activity.status even
	// after a linked purchase is resolved. It is intentionally a superset of the UI flags.
	live := `((a.date>=? AND a.date<=?) OR (a.kind IN ('request','purchase') AND (a.status='open' OR (p.status IS NOT NULL AND p.status NOT IN ('denied','cancelled','fulfilled','reimbursed','closed')))))`
	historyArgs := append(append([]any{}, args...), p.WeekStart, p.WeekDue)
	cursorWhere := ""
	if p.Cursor != "" {
		parts := strings.SplitN(p.Cursor, "|", 3)
		if len(parts) != 3 {
			return nil, nil, nil, fmt.Errorf("invalid activity cursor")
		}
		at, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid activity cursor")
		}
		cursorWhere = ` AND (a.date<? OR (a.date=? AND a.created_at<?) OR (a.date=? AND a.created_at=? AND a.id>?))`
		historyArgs = append(historyArgs, parts[0], parts[0], at, parts[0], at, parts[2])
	}
	from := ` FROM buildspace_launchpad_activity a LEFT JOIN purchase_requests p ON p.id=a.purchase_id LEFT JOIN buildspace_launchpad_accounts acc ON acc.id=a.author_account_id`
	liveArgs := append(append([]any{}, args...), p.WeekStart, p.WeekDue)
	liveQuery := `SELECT ` + buildspaceActivityCols + `,0` + from + scope + ` AND ` + live
	historyQuery := `SELECT ` + buildspaceActivityCols + `,1` + from + scope + ` AND NOT ` + live + cursorWhere + ` ORDER BY a.date DESC,a.created_at DESC,a.id LIMIT 201`
	query := `SELECT * FROM (` + liveQuery + `) live UNION ALL SELECT * FROM (` + historyQuery + `) history`
	queryArgs := append(liveArgs, historyArgs...)
	var done map[string]bool
	if len(p.Visibility) > 0 {
		// Weekly completion is independent of note visibility. Return only person IDs
		// in the same round trip, never the hidden note bodies.
		weeklyScope, weeklyArgs = weeklyDoneScope(weeklyScope, weeklyArgs, p)
		query += ` UNION ALL SELECT a.person_id,'',a.person_id,NULL,'weekly','','','',NULL,'','',0,NULL,NULL,NULL,'officers',NULL,NULL,2 FROM buildspace_launchpad_activity a` + weeklyScope
		queryArgs = append(queryArgs, weeklyArgs...)
		done = map[string]bool{}
	}
	query += ` ORDER BY date DESC,created_at DESC,id`
	rows, err := q.query(ctx, query, queryArgs...)
	if err != nil {
		return nil, nil, nil, err
	}
	activities := []BuildspaceActivity{}
	historyRows := 0
	var next *string
	var last string
	for rows.Next() {
		var a BuildspaceActivity
		var rowKind int
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.PersonID, &a.MentorID, &a.Kind, &a.Date, &a.Body, &a.Status, &a.PurchaseID, &a.Source, &a.ExternalID, &a.CreatedAt, &a.PurchaseTitle, &a.PurchaseStatus, &a.PurchaseNeededBy, &a.Visibility, &a.AuthorAccountID, &a.AuthorName, &rowKind); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		if rowKind == 2 {
			done[a.ID] = true
			continue
		}
		if rowKind == 1 {
			historyRows++
			if historyRows > 200 {
				next = &last
				continue
			}
			if historyRows == 200 {
				last = fmt.Sprintf("%s|%d|%s", a.Date, a.CreatedAt, a.ID)
			}
		}
		activities = append(activities, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	return activities, next, done, nil
}
