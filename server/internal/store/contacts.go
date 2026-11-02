package store

import (
	"context"
	"encoding/json"
)

type Contact struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Org          string            `json:"org"`
	Role         string            `json:"role"`
	Type         string            `json:"type"`
	Topics       []string          `json:"topics"`
	Email        string            `json:"email"`
	LinkedIn     string            `json:"linkedin"`
	Notes        string            `json:"notes"`
	CreatedAt    int64             `json:"createdAt"`
	UpdatedAt    int64             `json:"updatedAt"`
	Events       []ContactEvent    `json:"events"`
	Outreach     []ContactOutreach `json:"outreach"`
	LastOutreach *ContactOutreach  `json:"lastOutreach"`
}
type ContactEvent struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	StartsAt string `json:"startsAt"`
}
type ContactOutreach struct {
	ID          string `json:"id"`
	OfficerID   string `json:"officerId"`
	OfficerName string `json:"officerName"`
	Date        string `json:"date"`
	Channel     string `json:"channel"`
	Outcome     string `json:"outcome"`
	Note        string `json:"note"`
	CreatedAt   int64  `json:"createdAt"`
}

func (q *Queries) Contacts(ctx context.Context) ([]Contact, error) { return q.contacts(ctx, "") }
func (q *Queries) contacts(ctx context.Context, id string) ([]Contact, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,name,org,role,type,topics,email,linkedin,notes,created_at,updated_at FROM contacts WHERE (?1='' OR id=?1) ORDER BY name COLLATE NOCASE,id`, id)
	if err != nil {
		return nil, err
	}
	out := []Contact{}
	for rows.Next() {
		var c Contact
		var topics string
		if err := rows.Scan(&c.ID, &c.Name, &c.Org, &c.Role, &c.Type, &topics, &c.Email, &c.LinkedIn, &c.Notes, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(topics), &c.Topics); err != nil {
			rows.Close()
			return nil, err
		}
		c.Events = []ContactEvent{}
		c.Outreach = []ContactOutreach{}
		out = append(out, c)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	// Load relations in two queries rather than issuing queries per contact.
	byID := map[string]*Contact{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	rows, err = q.q.QueryContext(ctx, `SELECT ce.contact_id,e.id,e.name,e.starts_at FROM contact_events ce JOIN events e ON e.id=ce.event_id WHERE (?1='' OR ce.contact_id=?1) ORDER BY e.starts_at DESC,e.id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var contactID string
		var e ContactEvent
		if err := rows.Scan(&contactID, &e.ID, &e.Name, &e.StartsAt); err != nil {
			rows.Close()
			return nil, err
		}
		if c := byID[contactID]; c != nil {
			c.Events = append(c.Events, e)
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT o.contact_id,o.id,o.officer_id,u.name,o.date,o.channel,o.outcome,o.note,o.created_at FROM contact_outreach o JOIN users u ON u.id=o.officer_id WHERE (?1='' OR o.contact_id=?1) ORDER BY o.date DESC,o.created_at DESC,o.id DESC`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var contactID string
		var o ContactOutreach
		if err := rows.Scan(&contactID, &o.ID, &o.OfficerID, &o.OfficerName, &o.Date, &o.Channel, &o.Outcome, &o.Note, &o.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if c := byID[contactID]; c != nil {
			c.Outreach = append(c.Outreach, o)
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	for i := range out {
		if len(out[i].Outreach) > 0 {
			out[i].LastOutreach = &out[i].Outreach[0]
		}
	}

	return out, nil
}
func (q *Queries) ContactByID(ctx context.Context, id string) (*Contact, error) {
	all, err := q.contacts(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}
func (q *Queries) SaveContact(ctx context.Context, c Contact) error {
	topics, err := json.Marshal(c.Topics)
	if err != nil {
		return err
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO contacts(id,name,org,role,type,topics,email,linkedin,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,org=excluded.org,role=excluded.role,type=excluded.type,topics=excluded.topics,email=excluded.email,linkedin=excluded.linkedin,notes=excluded.notes,updated_at=excluded.updated_at`, c.ID, c.Name, c.Org, c.Role, c.Type, string(topics), c.Email, c.LinkedIn, c.Notes, c.CreatedAt, c.UpdatedAt)
	return err
}
func (q *Queries) SetContactEvents(ctx context.Context, id string, ids []string) error {
	if _, err := q.q.ExecContext(ctx, `DELETE FROM contact_events WHERE contact_id=?`, id); err != nil {
		return err
	}
	for _, e := range ids {
		if _, err := q.q.ExecContext(ctx, `INSERT INTO contact_events(contact_id,event_id) VALUES(?,?)`, id, e); err != nil {
			return err
		}
	}
	return nil
}
func (q *Queries) DeleteContact(ctx context.Context, id string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM contacts WHERE id=?`, id)
	return err
}
func (q *Queries) InsertContactOutreach(ctx context.Context, contactID string, o ContactOutreach) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO contact_outreach(id,contact_id,officer_id,date,channel,outcome,note,created_at) VALUES(?,?,?,?,?,?,?,?)`, o.ID, contactID, o.OfficerID, o.Date, o.Channel, o.Outcome, o.Note, o.CreatedAt)
	return err
}
