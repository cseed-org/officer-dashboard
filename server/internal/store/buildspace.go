package store

import (
	"context"
	"encoding/json"
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
}
type BuildspacePerson struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	MemberID  *string `json:"memberId"`
	DiscordID *string `json:"discordId"`
	ProjectID string  `json:"projectId"`
	JoinedOn  string  `json:"joinedOn"`
}
type BuildspaceMentor struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Org    string   `json:"org"`
	Topics []string `json:"topics"`
}
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
}
type Buildspace struct {
	Projects  []BuildspaceProject  `json:"projects"`
	People    []BuildspacePerson   `json:"people"`
	Mentors   []BuildspaceMentor   `json:"mentors"`
	Activity  []BuildspaceActivity `json:"activity"`
	WeekStart string               `json:"weekStart"`
	WeekDue   string               `json:"weekDue"`
	Today     string               `json:"today"`
	Schedule  BuildspaceSchedule   `json:"schedule"`
}

func (q *Queries) Buildspace(ctx context.Context) (Buildspace, error) {
	b := Buildspace{Projects: []BuildspaceProject{}, People: []BuildspacePerson{}, Mentors: []BuildspaceMentor{}, Activity: []BuildspaceActivity{}}
	rows, err := q.q.QueryContext(ctx, `SELECT id,name,description,track,stage,links,updated_at FROM buildspace_projects ORDER BY name`)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var p BuildspaceProject
		var links string
		if err = rows.Scan(&p.ID, &p.Name, &p.Description, &p.Track, &p.Stage, &links, &p.UpdatedAt); err != nil {
			rows.Close()
			return b, err
		}
		if err = json.Unmarshal([]byte(links), &p.Links); err != nil {
			rows.Close()
			return b, err
		}
		p.MentorIDs = []string{}
		b.Projects = append(b.Projects, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT project_id,contact_id FROM buildspace_mentors ORDER BY contact_id`)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var p, m string
		if err = rows.Scan(&p, &m); err != nil {
			rows.Close()
			return b, err
		}
		for i := range b.Projects {
			if b.Projects[i].ID == p {
				b.Projects[i].MentorIDs = append(b.Projects[i].MentorIDs, m)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT id,name,member_id,discord_id,project_id,joined_on FROM buildspace_people ORDER BY name`)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var p BuildspacePerson
		if err = rows.Scan(&p.ID, &p.Name, &p.MemberID, &p.DiscordID, &p.ProjectID, &p.JoinedOn); err != nil {
			rows.Close()
			return b, err
		}
		b.People = append(b.People, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT id,name,org,topics FROM contacts WHERE type IN ('mentor','both') OR id IN (SELECT contact_id FROM buildspace_mentors) ORDER BY name`)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var m BuildspaceMentor
		var topics string
		if err = rows.Scan(&m.ID, &m.Name, &m.Org, &topics); err != nil {
			rows.Close()
			return b, err
		}
		if err = json.Unmarshal([]byte(topics), &m.Topics); err != nil {
			rows.Close()
			return b, err
		}
		b.Mentors = append(b.Mentors, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT a.id,a.project_id,a.person_id,a.mentor_id,a.kind,a.date,a.body,a.status,a.purchase_id,a.source,a.external_id,a.created_at,p.title,p.status,p.needed_by FROM buildspace_activity a LEFT JOIN purchase_requests p ON p.id=a.purchase_id ORDER BY a.date DESC,a.created_at DESC,a.id`)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var a BuildspaceActivity
		if err = rows.Scan(&a.ID, &a.ProjectID, &a.PersonID, &a.MentorID, &a.Kind, &a.Date, &a.Body, &a.Status, &a.PurchaseID, &a.Source, &a.ExternalID, &a.CreatedAt, &a.PurchaseTitle, &a.PurchaseStatus, &a.PurchaseNeededBy); err != nil {
			rows.Close()
			return b, err
		}
		b.Activity = append(b.Activity, a)
	}
	err = rows.Err()
	rows.Close()
	return b, err
}
func (q *Queries) SaveBuildspaceProject(ctx context.Context, p BuildspaceProject) error {
	links, err := json.Marshal(p.Links)
	if err != nil {
		return err
	}
	if err = q.Exec(ctx, `INSERT INTO buildspace_projects(id,name,description,track,stage,links,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,track=excluded.track,stage=excluded.stage,links=excluded.links,updated_at=excluded.updated_at`, p.ID, p.Name, p.Description, p.Track, p.Stage, string(links), p.UpdatedAt); err != nil {
		return err
	}
	if err = q.Exec(ctx, `DELETE FROM buildspace_mentors WHERE project_id=?`, p.ID); err != nil {
		return err
	}
	for _, id := range p.MentorIDs {
		if err = q.Exec(ctx, `INSERT INTO buildspace_mentors(project_id,contact_id) VALUES(?,?)`, p.ID, id); err != nil {
			return err
		}
	}
	return nil
}
func (q *Queries) SaveBuildspacePerson(ctx context.Context, p BuildspacePerson) error {
	return q.Exec(ctx, `INSERT INTO buildspace_people(id,name,member_id,discord_id,project_id,joined_on) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,member_id=excluded.member_id,discord_id=excluded.discord_id,project_id=excluded.project_id,joined_on=excluded.joined_on`, p.ID, p.Name, p.MemberID, p.DiscordID, p.ProjectID, p.JoinedOn)
}
func (q *Queries) InsertBuildspaceActivity(ctx context.Context, a BuildspaceActivity) error {
	return q.Exec(ctx, `INSERT INTO buildspace_activity(id,project_id,person_id,mentor_id,kind,date,body,status,purchase_id,source,external_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ProjectID, a.PersonID, a.MentorID, a.Kind, a.Date, a.Body, a.Status, a.PurchaseID, a.Source, a.ExternalID, a.CreatedAt)
}
func (q *Queries) ResolveBuildspaceActivity(ctx context.Context, id string) error {
	return q.Exec(ctx, `UPDATE buildspace_activity SET status='resolved' WHERE id=?`, id)
}
