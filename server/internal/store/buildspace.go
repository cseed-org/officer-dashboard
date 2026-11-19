package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

type BuildspaceProject struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Track       string   `json:"track"`
	Stage       string   `json:"stage"`
	Links       []string `json:"links"`
	MentorIDs   []string `json:"mentorIds"`
	UpdatedAt   int64    `json:"updatedAt"`
	CohortID    string   `json:"cohortId"`
	ArchivedAt  *int64   `json:"archivedAt"`
	// Latest visible activity, independent of the bounded history page.
	LastActivityDate *string `json:"lastActivityDate"`
}
type BuildspacePerson struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	MemberID    *string `json:"memberId"`
	DiscordID   *string `json:"discordId"`
	ProjectID   string  `json:"projectId"`
	JoinedOn    string  `json:"joinedOn"`
	CohortID    string  `json:"cohortId"`
	ArchivedAt  *int64  `json:"archivedAt"`
	Designation string  `json:"designation"`
	Pronouns    string  `json:"pronouns"`
	Bio         string  `json:"bio"`
	Email       *string `json:"email"`
	// The builder's portal account, when one exists (read-only; never saved from here).
	AccountID     *string `json:"accountId"`
	AccountStatus *string `json:"accountStatus"`
	// Latest visible project-wide or own activity, independent of history paging.
	LastActivityDate *string `json:"lastActivityDate"`
}

// BuildspaceMentor is a roster row: a mentor contact with availability, cap, current load
// (non-archived projects in active cohorts) and portal account state.
type BuildspaceMentor struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Org         string            `json:"org"`
	Topics      []string          `json:"topics"`
	Email       string            `json:"email"`
	Type        string            `json:"-"`
	Available   bool              `json:"available"`
	MaxProjects *int              `json:"maxProjects"`
	Load        int               `json:"load"`
	Account     *MentorAccountRef `json:"account"`
}
type MentorAccountRef struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	InviteExpired   bool   `json:"inviteExpired"`
	LastLoginAt     *int64 `json:"lastLoginAt"`
	InviteExpiresAt *int64 `json:"-"`
}

// IsMentorType reports whether the contact is a mentor by type (assigned non-mentor
// contacts still appear in the roster so their assignments stay visible).
func (m BuildspaceMentor) IsMentorType() bool { return m.Type == "mentor" || m.Type == "both" }

type BuildspaceActivity struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"projectId"`
	PersonID       *string `json:"personId"`
	MentorID       *string `json:"mentorId"`
	Kind           string  `json:"kind"`
	Date           string  `json:"date"`
	Body           string  `json:"body"`
	Status         string  `json:"status"`
	PurchaseID     *string `json:"purchaseId"`
	Source         string  `json:"source"`
	ExternalID     *string `json:"externalId"`
	CreatedAt      int64   `json:"createdAt"`
	PurchaseTitle  *string `json:"purchaseTitle"`
	PurchaseStatus *string `json:"purchaseStatus"`
	// PurchaseNeededBy is the linked finance request's date-only deadline.
	PurchaseNeededBy *string `json:"purchaseNeededBy"`
	// Visibility: team | mentors | officers (docs/launchpad-cohorts.md).
	Visibility      string  `json:"visibility"`
	AuthorAccountID *string `json:"authorAccountId"`
	AuthorName      *string `json:"authorName"`
}
type Buildspace struct {
	WeeklyDone         map[string]bool      `json:"-"`
	ActivityNextCursor *string              `json:"activityNextCursor,omitempty"`
	Projects           []BuildspaceProject  `json:"projects"`
	People             []BuildspacePerson   `json:"people"`
	Mentors            []BuildspaceMentor   `json:"mentors"`
	Activity           []BuildspaceActivity `json:"activity"`
	WeekStart          string               `json:"weekStart"`
	WeekDue            string               `json:"weekDue"`
	Today              string               `json:"today"`
	Schedule           BuildspaceSchedule   `json:"schedule"`
	Cohorts            []LaunchpadCohort    `json:"cohorts"`
	CohortID           *string              `json:"cohortId"`
	Accounts           []LaunchpadAccount   `json:"accounts"`
}

// Project finds a project by ID.
func (b *Buildspace) Project(id string) *BuildspaceProject {
	for i := range b.Projects {
		if b.Projects[i].ID == id {
			return &b.Projects[i]
		}
	}
	return nil
}

// Person finds a builder by ID.
func (b *Buildspace) Person(id string) *BuildspacePerson {
	for i := range b.People {
		if b.People[i].ID == id {
			return &b.People[i]
		}
	}
	return nil
}

// Cohort finds a cohort by ID.
func (b *Buildspace) Cohort(id string) *LaunchpadCohort {
	for i := range b.Cohorts {
		if b.Cohorts[i].ID == id {
			return &b.Cohorts[i]
		}
	}
	return nil
}

// Mentor finds a roster row by contact ID.
func (b *Buildspace) Mentor(id string) *BuildspaceMentor {
	for i := range b.Mentors {
		if b.Mentors[i].ID == id {
			return &b.Mentors[i]
		}
	}
	return nil
}

// DefaultCohort is the newest active cohort (cohorts are sorted newest first), else the newest.
func (b *Buildspace) DefaultCohort() *LaunchpadCohort {
	for i := range b.Cohorts {
		if b.Cohorts[i].Status == "active" {
			return &b.Cohorts[i]
		}
	}
	if len(b.Cohorts) > 0 {
		return &b.Cohorts[0]
	}
	return nil
}

// ProjectLive reports whether a project counts for load, calendars and portals: not
// archived, in an active cohort.
func (b *Buildspace) ProjectLive(p *BuildspaceProject) bool {
	if p == nil || p.ArchivedAt != nil {
		return false
	}
	c := b.Cohort(p.CohortID)
	return c != nil && c.Status == "active"
}

type rowIter struct {
	rows *sql.Rows
	err  error
}

func iter(rows *sql.Rows, err error) rowIter { return rowIter{rows, err} }

// each calls fn per row and closes the rows.
func (r rowIter) each(fn func(*sql.Rows) error) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()
	for r.rows.Next() {
		if err := fn(r.rows); err != nil {
			return err
		}
	}
	return r.rows.Err()
}

// Buildspace reads the whole register: every cohort, project, builder, mentor, account and
// activity entry. The cohort is small; the API filters by cohort and archive state.
func (q *Queries) Buildspace(ctx context.Context) (Buildspace, error) {
	return q.buildspace(ctx, nil, nil)
}

// BuildspaceForCohort reads activity only for the selected cohort. An empty ID selects
// the default cohort; all cohort metadata remains available for navigation and mentor loads.
func (q *Queries) BuildspaceForCohort(ctx context.Context, cohortID string) (Buildspace, error) {
	return q.buildspace(ctx, &cohortID, nil)
}

func (q *Queries) LaunchpadPersonArchived(ctx context.Context, id string) (bool, error) {
	var archived bool
	err := q.q.QueryRowContext(ctx, `SELECT archived_at IS NOT NULL FROM buildspace_launchpad_people WHERE id=?`, id).Scan(&archived)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return archived, err
}

const buildspaceActivityCols = `a.id,a.project_id,a.person_id,a.mentor_id,a.kind,a.date,a.body,a.status,a.purchase_id,a.source,a.external_id,a.created_at,p.title,p.status,p.needed_by,a.visibility,a.author_account_id,
 CASE WHEN acc.id IS NULL THEN NULL ELSE COALESCE((SELECT name FROM buildspace_launchpad_people WHERE id=acc.person_id),(SELECT name FROM contacts WHERE id=acc.contact_id),acc.name) END`

func scanBuildspaceActivity(rows *sql.Rows) (BuildspaceActivity, error) {
	var a BuildspaceActivity
	err := rows.Scan(&a.ID, &a.ProjectID, &a.PersonID, &a.MentorID, &a.Kind, &a.Date, &a.Body, &a.Status, &a.PurchaseID, &a.Source, &a.ExternalID, &a.CreatedAt, &a.PurchaseTitle, &a.PurchaseStatus, &a.PurchaseNeededBy, &a.Visibility, &a.AuthorAccountID, &a.AuthorName)
	return a, err
}

func activityIDScope(column string, ids []string) (string, []any) {
	if len(ids) == 0 {
		return " AND 0", nil
	}
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = ids[i]
	}
	return ` AND a.` + column + ` IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`, args
}

func (q *Queries) buildspace(ctx context.Context, cohortID *string, page *BuildspaceActivityPage) (Buildspace, error) {
	b := Buildspace{Projects: []BuildspaceProject{}, People: []BuildspacePerson{}, Mentors: []BuildspaceMentor{}, Activity: []BuildspaceActivity{}, Cohorts: []LaunchpadCohort{}, Accounts: []LaunchpadAccount{}}
	var err error
	if b.Cohorts, err = q.LaunchpadCohorts(ctx); err != nil {
		return b, err
	}
	err = iter(q.query(ctx, `SELECT id,name,description,track,stage,links,updated_at,cohort_id,archived_at FROM buildspace_launchpad_projects ORDER BY name`)).each(func(rows *sql.Rows) error {
		var p BuildspaceProject
		var links string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Track, &p.Stage, &links, &p.UpdatedAt, &p.CohortID, &p.ArchivedAt); err != nil {
			return err
		}
		p.MentorIDs = []string{}
		b.Projects = append(b.Projects, p)
		return json.Unmarshal([]byte(links), &b.Projects[len(b.Projects)-1].Links)
	})
	if err != nil {
		return b, err
	}
	err = iter(q.query(ctx, `SELECT project_id,contact_id FROM buildspace_launchpad_mentors ORDER BY contact_id`)).each(func(rows *sql.Rows) error {
		var p, m string
		if err := rows.Scan(&p, &m); err != nil {
			return err
		}
		if pr := b.Project(p); pr != nil {
			pr.MentorIDs = append(pr.MentorIDs, m)
		}
		return nil
	})
	if err != nil {
		return b, err
	}
	err = iter(q.query(ctx, `SELECT b.id,b.name,b.member_id,b.discord_id,b.project_id,b.joined_on,b.cohort_id,b.archived_at,b.designation,b.pronouns,b.bio,b.email,a.id,a.status
 FROM buildspace_launchpad_people b LEFT JOIN buildspace_launchpad_accounts a ON a.person_id=b.id ORDER BY b.name`)).each(func(rows *sql.Rows) error {
		var p BuildspacePerson
		if err := rows.Scan(&p.ID, &p.Name, &p.MemberID, &p.DiscordID, &p.ProjectID, &p.JoinedOn, &p.CohortID, &p.ArchivedAt, &p.Designation, &p.Pronouns, &p.Bio, &p.Email, &p.AccountID, &p.AccountStatus); err != nil {
			return err
		}
		b.People = append(b.People, p)
		return nil
	})
	if err != nil {
		return b, err
	}
	if b.Accounts, err = q.LaunchpadAccounts(ctx); err != nil {
		return b, err
	}
	mentorAccounts := map[string]*LaunchpadAccount{}
	for i := range b.Accounts {
		if a := &b.Accounts[i]; a.ContactID != nil {
			mentorAccounts[*a.ContactID] = a
		}
	}
	err = iter(q.query(ctx, `SELECT c.id,c.name,c.org,c.topics,c.email,c.type,COALESCE(s.available,1),s.max_projects FROM contacts c
 LEFT JOIN buildspace_launchpad_mentor_settings s ON s.contact_id=c.id
 WHERE c.type IN ('mentor','both') OR c.id IN (SELECT contact_id FROM buildspace_launchpad_mentors) ORDER BY c.name`)).each(func(rows *sql.Rows) error {
		var m BuildspaceMentor
		var topics string
		if err := rows.Scan(&m.ID, &m.Name, &m.Org, &topics, &m.Email, &m.Type, &m.Available, &m.MaxProjects); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(topics), &m.Topics); err != nil {
			return err
		}
		if a := mentorAccounts[m.ID]; a != nil {
			m.Account = &MentorAccountRef{ID: a.ID, Status: a.Status, LastLoginAt: a.LastLoginAt, InviteExpiresAt: a.InviteExpiresAt}
		}
		b.Mentors = append(b.Mentors, m)
		return nil
	})
	if err != nil {
		return b, err
	}
	for i := range b.Projects {
		if !b.ProjectLive(&b.Projects[i]) {
			continue
		}
		for _, id := range b.Projects[i].MentorIDs {
			if m := b.Mentor(id); m != nil {
				m.Load++
			}
		}
	}
	activityWhere := " WHERE 1=1"
	if page != nil && page.SkipActivity {
		return b, nil
	}
	var activityArgs []any
	var lastActivityWhere string
	var lastActivityArgs []any
	if cohortID != nil {
		id := *cohortID
		if id == "" {
			if cohort := b.DefaultCohort(); cohort != nil {
				id = cohort.ID
			}
		}
		projectIDs := []string{}
		for i := range b.Projects {
			project := &b.Projects[i]
			if project.CohortID == id && (page == nil || !page.FilterArchived || project.ArchivedAt == nil) {
				projectIDs = append(projectIDs, project.ID)
			}
		}
		condition, ids := activityIDScope("project_id", projectIDs)
		activityWhere += condition
		activityArgs = append(activityArgs, ids...)
	}
	if page != nil {
		weeklyWhere := activityWhere
		weeklyArgs := append([]any(nil), activityArgs...)
		scopePage := *page
		if scopePage.FilterArchived {
			peopleIDs := []string{}
			for i := range b.People {
				if b.People[i].ArchivedAt == nil {
					peopleIDs = append(peopleIDs, b.People[i].ID)
				}
			}
			condition, ids := activityIDScope("person_id", peopleIDs)
			condition = ` AND (a.person_id IS NULL OR ` + strings.TrimPrefix(condition, " AND ") + `)`
			activityWhere += condition
			activityArgs = append(activityArgs, ids...)
			weeklyWhere += condition
			weeklyArgs = append(weeklyArgs, ids...)
			scopePage.FilterArchived = false
		}
		if scopePage.PersonID != "" {
			person := b.Person(scopePage.PersonID)
			condition, ids := "", []any(nil)
			if person != nil {
				condition, ids = activityIDScope("project_id", []string{person.ProjectID})
			} else {
				condition = " AND 0"
			}
			activityWhere += condition
			activityArgs = append(activityArgs, ids...)
			weeklyWhere += ` AND a.person_id=?`
			weeklyArgs = append(weeklyArgs, scopePage.PersonID)
			scopePage.PersonID = ""
		}
		if scopePage.MentorID != "" {
			projectIDs := []string{}
			for i := range b.Projects {
				project := &b.Projects[i]
				if !b.ProjectLive(project) {
					continue
				}
				for _, mentorID := range project.MentorIDs {
					if mentorID == scopePage.MentorID {
						projectIDs = append(projectIDs, project.ID)
						break
					}
				}
			}
			condition, ids := activityIDScope("project_id", projectIDs)
			activityWhere += condition
			activityArgs = append(activityArgs, ids...)
			weeklyWhere += condition
			weeklyArgs = append(weeklyArgs, ids...)
			scopePage.MentorID = ""
		}
		activityWhere, activityArgs = activityScope(activityWhere, activityArgs, scopePage)
		lastActivityWhere = activityWhere
		lastActivityArgs = append([]any(nil), activityArgs...)
		b.Activity, b.ActivityNextCursor, b.WeeklyDone, err = q.activityPageWhere(ctx, activityWhere, activityArgs, weeklyWhere, weeklyArgs, scopePage)
		if err != nil {
			return b, err
		}
	} else {
		err = iter(q.query(ctx, `SELECT `+buildspaceActivityCols+`
 FROM buildspace_launchpad_activity a LEFT JOIN purchase_requests p ON p.id=a.purchase_id LEFT JOIN buildspace_launchpad_accounts acc ON acc.id=a.author_account_id
`+activityWhere+` ORDER BY a.date DESC,a.created_at DESC,a.id`, activityArgs...)).each(func(rows *sql.Rows) error {
			a, err := scanBuildspaceActivity(rows)
			if err != nil {
				return err
			}
			b.Activity = append(b.Activity, a)
			return nil
		})
		if err != nil {
			return b, err
		}
	}
	if page != nil {
		sort.Slice(b.Activity, func(i, j int) bool {
			if b.Activity[i].Date != b.Activity[j].Date {
				return b.Activity[i].Date > b.Activity[j].Date
			}
			if b.Activity[i].CreatedAt != b.Activity[j].CreatedAt {
				return b.Activity[i].CreatedAt > b.Activity[j].CreatedAt
			}
			return b.Activity[i].ID < b.Activity[j].ID
		})
	}
	if page == nil || (page.Cursor == "" && b.ActivityNextCursor == nil) {
		lastActivityFromRows(&b)
	} else if err := q.lastActivityDates(ctx, lastActivityWhere, lastActivityArgs, *page, &b); err != nil {
		return b, err
	}
	return b, nil
}

// MarkExpired fills the invite-expired flags against now (milliseconds).
func (b *Buildspace) MarkExpired(now int64) {
	for i := range b.Accounts {
		b.Accounts[i].MarkExpired(now)
	}
	for i := range b.Mentors {
		if a := b.Mentors[i].Account; a != nil {
			a.InviteExpired = a.Status == "invited" && a.InviteExpiresAt != nil && *a.InviteExpiresAt <= now
		}
	}
}

func (q *Queries) SaveBuildspaceProject(ctx context.Context, p BuildspaceProject) error {
	links, err := json.Marshal(p.Links)
	if err != nil {
		return err
	}
	if err = q.Exec(ctx, `INSERT INTO buildspace_launchpad_projects(id,name,description,track,stage,links,updated_at,cohort_id) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,track=excluded.track,stage=excluded.stage,links=excluded.links,updated_at=excluded.updated_at`, p.ID, p.Name, p.Description, p.Track, p.Stage, string(links), p.UpdatedAt, p.CohortID); err != nil {
		return err
	}
	if err = q.Exec(ctx, `DELETE FROM buildspace_launchpad_mentors WHERE project_id=?`, p.ID); err != nil {
		return err
	}
	for _, id := range p.MentorIDs {
		if err = q.Exec(ctx, `INSERT INTO buildspace_launchpad_mentors(project_id,contact_id) VALUES(?,?)`, p.ID, id); err != nil {
			return err
		}
	}
	return nil
}

// AddBuildspaceMentor assigns one more mentor to a project.
func (q *Queries) AddBuildspaceMentor(ctx context.Context, projectID, contactID string, now int64) error {
	if err := q.Exec(ctx, `INSERT INTO buildspace_launchpad_mentors(project_id,contact_id) VALUES(?,?)`, projectID, contactID); err != nil {
		return err
	}
	return q.Exec(ctx, `UPDATE buildspace_launchpad_projects SET updated_at=? WHERE id=?`, now, projectID)
}

func (q *Queries) SaveBuildspacePerson(ctx context.Context, p BuildspacePerson) error {
	return q.Exec(ctx, `INSERT INTO buildspace_launchpad_people(id,name,member_id,discord_id,project_id,joined_on,cohort_id,designation,pronouns,bio,email) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,member_id=excluded.member_id,discord_id=excluded.discord_id,project_id=excluded.project_id,joined_on=excluded.joined_on,cohort_id=excluded.cohort_id,
 designation=excluded.designation,pronouns=excluded.pronouns,bio=excluded.bio,email=excluded.email`,
		p.ID, p.Name, p.MemberID, p.DiscordID, p.ProjectID, p.JoinedOn, p.CohortID, p.Designation, p.Pronouns, p.Bio, p.Email)
}
func (q *Queries) InsertBuildspaceActivity(ctx context.Context, a BuildspaceActivity) error {
	if a.Visibility == "" {
		a.Visibility = "officers"
	}
	return q.Exec(ctx, `INSERT INTO buildspace_launchpad_activity(id,project_id,person_id,mentor_id,kind,date,body,status,purchase_id,source,external_id,created_at,visibility,author_account_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ProjectID, a.PersonID, a.MentorID, a.Kind, a.Date, a.Body, a.Status, a.PurchaseID, a.Source, a.ExternalID, a.CreatedAt, a.Visibility, a.AuthorAccountID)
}
func (q *Queries) ResolveBuildspaceActivity(ctx context.Context, id string) error {
	return q.Exec(ctx, `UPDATE buildspace_launchpad_activity SET status='resolved' WHERE id=?`, id)
}

// SetBuildspaceProjectArchived archives (at != nil) or restores a project.
func (q *Queries) SetBuildspaceProjectArchived(ctx context.Context, id string, at *int64) error {
	return q.Exec(ctx, `UPDATE buildspace_launchpad_projects SET archived_at=? WHERE id=?`, at, id)
}

// SetBuildspacePersonArchived archives (at != nil) or restores a builder.
func (q *Queries) SetBuildspacePersonArchived(ctx context.Context, id string, at *int64) error {
	return q.Exec(ctx, `UPDATE buildspace_launchpad_people SET archived_at=? WHERE id=?`, at, id)
}

func (q *Queries) DeleteBuildspaceProject(ctx context.Context, id string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_projects WHERE id=?`, id)
}
func (q *Queries) DeleteBuildspacePerson(ctx context.Context, id string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_people WHERE id=?`, id)
}

// MemberIDsByUWEmail returns members whose UW email matches (case-insensitive), sorted.
func (q *Queries) MemberIDsByUWEmail(ctx context.Context, email string) ([]string, error) {
	out := []string{}
	err := iter(q.query(ctx, `SELECT person_id FROM members_mirror WHERE lower(trim(uw_email))=lower(trim(?)) AND trim(?)<>''`, email, email)).each(func(rows *sql.Rows) error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		out = append(out, id)
		return nil
	})
	sort.Strings(out)
	return out, err
}
