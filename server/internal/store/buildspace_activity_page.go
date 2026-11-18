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
func (q *Queries) weeklyDone(ctx context.Context, scope string, args []any, p BuildspaceActivityPage) (map[string]bool, error) {
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
	rows, err := q.q.QueryContext(ctx, `SELECT DISTINCT a.person_id FROM buildspace_launchpad_activity a`+scope, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		done[id] = true
	}
	return done, rows.Err()
}

func (q *Queries) activityPageWhere(ctx context.Context, scope string, args []any, p BuildspaceActivityPage) (string, []any, *string, error) {
	if len(p.Visibility) > 0 {
		raw, _ := json.Marshal(p.Visibility)
		scope += ` AND a.visibility IN (SELECT value FROM json_each(?))`
		args = append(args, string(raw))
	}
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
	// The second open condition retains mentor signals that use activity.status even
	// after a linked purchase is resolved. It is intentionally a superset of the UI flags.
	live := `((a.date>=? AND a.date<=?) OR (a.kind IN ('request','purchase') AND (a.status='open' OR (p.status IS NOT NULL AND p.status NOT IN ('denied','cancelled','fulfilled','reimbursed','closed')))))`
	historyArgs := append(append([]any{}, args...), p.WeekStart, p.WeekDue)
	cursorWhere := ""
	if p.Cursor != "" {
		parts := strings.SplitN(p.Cursor, "|", 3)
		if len(parts) != 3 {
			return "", nil, nil, fmt.Errorf("invalid activity cursor")
		}
		at, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return "", nil, nil, fmt.Errorf("invalid activity cursor")
		}
		cursorWhere = ` AND (a.date<? OR (a.date=? AND a.created_at<?) OR (a.date=? AND a.created_at=? AND a.id>?))`
		historyArgs = append(historyArgs, parts[0], parts[0], at, parts[0], at, parts[2])
	}
	rows, err := q.q.QueryContext(ctx, `SELECT a.id,a.date,a.created_at FROM buildspace_launchpad_activity a LEFT JOIN purchase_requests p ON p.id=a.purchase_id`+scope+` AND NOT `+live+cursorWhere+` ORDER BY a.date DESC,a.created_at DESC,a.id LIMIT 201`, historyArgs...)
	if err != nil {
		return "", nil, nil, err
	}
	ids := []string{}
	var next *string
	var last string
	for rows.Next() {
		var id, date string
		var at int64
		if err := rows.Scan(&id, &date, &at); err != nil {
			rows.Close()
			return "", nil, nil, err
		}
		if len(ids) == 200 {
			next = &last
			break
		}
		ids = append(ids, id)
		last = fmt.Sprintf("%s|%d|%s", date, at, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", nil, nil, err
	}
	raw, _ := json.Marshal(ids)
	args = append(args, p.WeekStart, p.WeekDue, string(raw))
	return scope + ` AND (` + live + ` OR a.id IN (SELECT value FROM json_each(?)))`, args, next, nil
}
