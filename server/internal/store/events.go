package store

import (
	"context"
	"strings"

	"officerdashboard/internal/luma"
)

// Event is an events row.
type Event struct {
	ID            string
	LumaID        *string
	Name          string
	StartsAt      string // ISO with offset, as supplied
	EndsAt        *string
	Location      *string
	URL           *string
	HostUserID    *string
	Status        string // draft | published | completed | cancelled
	Description   *string
	Capacity      *int64
	HostSubteamID *string
	LumaURL       *string
	NotionURL     *string
	CreatedAt     Ms
	UpdatedAt     Ms
	// CoverUpdatedAt is read from event_covers (nil when the event has no cover).
	CoverUpdatedAt *int64
}

// EventStatuses are the values events.status accepts.
var EventStatuses = []string{"draft", "published", "completed", "cancelled"}

const eventCols = `id, luma_id, name, starts_at, ends_at, location, url, host_user_id, status, description, capacity, host_subteam_id, luma_url, notion_url, created_at, updated_at`

const eventSelect = `SELECT e.id, e.luma_id, e.name, e.starts_at, e.ends_at, e.location, e.url, e.host_user_id, e.status, e.description, e.capacity,
	e.host_subteam_id, e.luma_url, e.notion_url, e.created_at, e.updated_at, c.updated_at
	FROM events e LEFT JOIN event_covers c ON c.event_id = e.id`

func scanEvent(s interface{ Scan(...any) error }) (*Event, error) {
	var e Event
	if err := s.Scan(&e.ID, &e.LumaID, &e.Name, &e.StartsAt, &e.EndsAt, &e.Location, &e.URL, &e.HostUserID, &e.Status, &e.Description, &e.Capacity,
		&e.HostSubteamID, &e.LumaURL, &e.NotionURL, &e.CreatedAt, &e.UpdatedAt, &e.CoverUpdatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

// Events lists every event (callers filter and page: there are tens per year).
func (q *Queries) Events(ctx context.Context) ([]Event, error) {
	rows, err := q.q.QueryContext(ctx, eventSelect+` ORDER BY e.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// EventByID returns ErrNotFound for an unknown id.
func (q *Queries) EventByID(ctx context.Context, id string) (*Event, error) {
	e, err := scanEvent(q.q.QueryRowContext(ctx, eventSelect+` WHERE e.id = ?`, id))
	return e, notFound(err)
}

// EventByLumaID returns ErrNotFound when no event has the Luma id.
func (q *Queries) EventByLumaID(ctx context.Context, lumaID string) (*Event, error) {
	e, err := scanEvent(q.q.QueryRowContext(ctx, eventSelect+` WHERE e.luma_id = ?`, lumaID))
	return e, notFound(err)
}

// EventByLumaURL returns ErrNotFound when no event links the Luma page.
func (q *Queries) EventByLumaURL(ctx context.Context, lumaURL string) (*Event, error) {
	e, err := scanEvent(q.q.QueryRowContext(ctx, eventSelect+` WHERE e.luma_url = ?`, lumaURL))
	return e, notFound(err)
}

// InsertEvent adds an event. An empty Status stores "published" (imports come from Luma).
func (q *Queries) InsertEvent(ctx context.Context, e Event) error {
	if e.Status == "" {
		e.Status = "published"
	}
	_, err := q.q.ExecContext(ctx, `INSERT INTO events (`+eventCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.LumaID, e.Name, e.StartsAt, e.EndsAt, e.Location, e.URL, e.HostUserID, e.Status, e.Description, e.Capacity, e.HostSubteamID,
		e.LumaURL, e.NotionURL, int64(e.CreatedAt), int64(e.UpdatedAt))
	return err
}

// EventPatch lists the event fields that can change.
type EventPatch struct {
	Name          Opt[string]
	StartsAt      Opt[string]
	EndsAt        Opt[string]
	Location      Opt[string]
	URL           Opt[string]
	LumaID        Opt[string]
	HostUserID    Opt[string]
	Status        Opt[string]
	Description   Opt[string]
	Capacity      Opt[int64]
	HostSubteamID Opt[string]
	LumaURL       Opt[string]
	NotionURL     Opt[string]
}

// UpdateEvent applies a patch and stamps updated_at.
func (q *Queries) UpdateEvent(ctx context.Context, id string, p EventPatch, now int64) error {
	var sets []string
	var args []any
	add := func(col string, set bool, v any) {
		if set {
			sets = append(sets, col+" = ?")
			args = append(args, v)
		}
	}
	str := func(col string, o Opt[string]) { add(col, o.Set, o.val()) }
	str("name", p.Name)
	str("starts_at", p.StartsAt)
	str("ends_at", p.EndsAt)
	str("location", p.Location)
	str("url", p.URL)
	str("luma_id", p.LumaID)
	str("host_user_id", p.HostUserID)
	str("status", p.Status)
	str("description", p.Description)
	add("capacity", p.Capacity.Set, p.Capacity.val())
	str("host_subteam_id", p.HostSubteamID)
	str("luma_url", p.LumaURL)
	str("notion_url", p.NotionURL)
	sets = append(sets, "updated_at = ?")
	args = append(args, now, id)
	_, err := q.q.ExecContext(ctx, `UPDATE events SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// EventRequestCount counts purchase requests that point at the event (they block a delete).
func (q *Queries) EventRequestCount(ctx context.Context, id string) (int, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM purchase_requests WHERE event_id = ?`, id).Scan(&n)
	return n, err
}

// DeleteEvent removes an event; guests, reflections and the cover cascade.
func (q *Queries) DeleteEvent(ctx context.Context, id string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id)
	return err
}

// Cover is an event's cover image.
type Cover struct {
	EventID   string
	MimeType  string
	Bytes     []byte
	Filename  string
	UpdatedAt int64
}

// EventCover returns ErrNotFound when the event has no cover.
func (q *Queries) EventCover(ctx context.Context, eventID string) (*Cover, error) {
	var c Cover
	err := q.q.QueryRowContext(ctx, `SELECT event_id, mime_type, bytes, filename, updated_at FROM event_covers WHERE event_id = ?`, eventID).
		Scan(&c.EventID, &c.MimeType, &c.Bytes, &c.Filename, &c.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

// PutEventCover inserts or replaces the cover.
func (q *Queries) PutEventCover(ctx context.Context, c Cover) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO event_covers (event_id, mime_type, bytes, filename, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT (event_id) DO UPDATE SET mime_type = excluded.mime_type, bytes = excluded.bytes, filename = excluded.filename, updated_at = excluded.updated_at`,
		c.EventID, c.MimeType, c.Bytes, c.Filename, c.UpdatedAt)
	return err
}

// DeleteEventCover removes the cover and reports whether there was one.
func (q *Queries) DeleteEventCover(ctx context.Context, eventID string) (bool, error) {
	res, err := q.q.ExecContext(ctx, `DELETE FROM event_covers WHERE event_id = ?`, eventID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Subteam is a subteams row.
type Subteam struct {
	ID   string
	Name string
}

// Subteams lists subteams by name.
func (q *Queries) Subteams(ctx context.Context) ([]Subteam, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id, name FROM subteams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subteam
	for rows.Next() {
		var t Subteam
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Counts are the raw numbers behind an event's health ratios.
type Counts struct {
	Registered int
	CheckedIn  int
	Reflect    int
	Matched    int
}

// EventCounts computes counts for one event, or for every event when eventID is "".
// Registered follows Luma's approval states; a reflection is matched when its email
// belongs to a checked-in guest of the same event.
func (q *Queries) EventCounts(ctx context.Context, eventID string) (map[string]*Counts, error) {
	out := map[string]*Counts{}
	get := func(id string) *Counts {
		c := out[id]
		if c == nil {
			c = &Counts{}
			out[id] = c
		}
		return c
	}
	where, args := "", []any{}
	if eventID != "" {
		where, args = " WHERE event_id = ?", []any{eventID}
	}
	rows, err := q.q.QueryContext(ctx, `SELECT event_id, approval_status, checked_in_at IS NOT NULL, count(*) FROM event_guests`+where+` GROUP BY event_id, approval_status, checked_in_at IS NOT NULL`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var status *string
		var checked bool
		var n int
		if err := rows.Scan(&id, &status, &checked, &n); err != nil {
			rows.Close()
			return nil, err
		}
		c := get(id)
		if luma.IsRegistered(status, checked) {
			c.Registered += n
		}
		if checked {
			c.CheckedIn += n
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	rrows, err := q.q.QueryContext(ctx, `SELECT r.event_id, count(*), coalesce(sum(EXISTS (SELECT 1 FROM event_guests g WHERE g.event_id = r.event_id AND g.email = r.email AND g.checked_in_at IS NOT NULL)), 0)
		FROM reflections r`+strings.Replace(where, "event_id", "r.event_id", 1)+` GROUP BY r.event_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var id string
		var n, m int
		if err := rrows.Scan(&id, &n, &m); err != nil {
			return nil, err
		}
		c := get(id)
		c.Reflect, c.Matched = n, m
	}
	return out, rrows.Err()
}

// GuestRow is what the registration chart needs from a guest.
type GuestRow struct {
	ApprovalStatus *string
	RegisteredAt   *int64
	CheckedIn      bool
}

// GuestsForChart lists an event's guests.
func (q *Queries) GuestsForChart(ctx context.Context, eventID string) ([]GuestRow, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT approval_status, registered_at, checked_in_at IS NOT NULL FROM event_guests WHERE event_id = ?`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GuestRow
	for rows.Next() {
		var g GuestRow
		var reg *Ms
		if err := rows.Scan(&g.ApprovalStatus, &reg, &g.CheckedIn); err != nil {
			return nil, err
		}
		if reg != nil {
			v := int64(*reg)
			g.RegisteredAt = &v
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ReflectionTimes lists when each reflection was submitted (ms).
func (q *Queries) ReflectionTimes(ctx context.Context, eventID string) ([]int64, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT submitted_at FROM reflections WHERE event_id = ?`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var t Ms
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, int64(t))
	}
	return out, rows.Err()
}

// GuestIn is one guest to upsert. Nil fields keep the stored value; names in Clear are set
// to NULL (an explicit JSON null from the ingest API).
type GuestIn struct {
	ID             string
	Email          string
	Name           *string
	LumaGuestID    *string
	ApprovalStatus *string
	RegisteredAt   *int64
	CheckedInAt    *int64
	Clear          []string // name | lumaGuestId | approvalStatus | registeredAt | checkedInAt
}

// ExistingGuests maps email → id for an event's guests.
func (q *Queries) ExistingGuests(ctx context.Context, eventID string) (map[string]string, error) {
	return q.emailIDs(ctx, `SELECT email, id FROM event_guests WHERE event_id = ?`, eventID)
}

// ExistingReflections maps email → id for an event's reflections.
func (q *Queries) ExistingReflections(ctx context.Context, eventID string) (map[string]string, error) {
	return q.emailIDs(ctx, `SELECT email, id FROM reflections WHERE event_id = ?`, eventID)
}

func (q *Queries) emailIDs(ctx context.Context, query, eventID string) (map[string]string, error) {
	rows, err := q.q.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var email, id string
		if err := rows.Scan(&email, &id); err != nil {
			return nil, err
		}
		out[email] = id
	}
	return out, rows.Err()
}

var clearCols = map[string]string{"name": "name", "lumaGuestId": "luma_guest_id", "approvalStatus": "approval_status", "registeredAt": "registered_at", "checkedInAt": "checked_in_at"}

// UpsertGuests inserts or merges guests by (event, email): new non-null values win, absent
// ones keep what is stored, explicit clears null the field.
func (q *Queries) UpsertGuests(ctx context.Context, eventID string, guests []GuestIn, now int64) error {
	st, err := q.q.PrepareContext(ctx, `INSERT INTO event_guests (id, event_id, email, name, luma_guest_id, approval_status, registered_at, checked_in_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT (event_id, email) DO UPDATE SET
		  name = coalesce(excluded.name, name),
		  luma_guest_id = coalesce(excluded.luma_guest_id, luma_guest_id),
		  approval_status = coalesce(excluded.approval_status, approval_status),
		  registered_at = coalesce(excluded.registered_at, registered_at),
		  checked_in_at = coalesce(excluded.checked_in_at, checked_in_at)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, g := range guests {
		if _, err := st.ExecContext(ctx, g.ID, eventID, g.Email, g.Name, g.LumaGuestID, g.ApprovalStatus, g.RegisteredAt, g.CheckedInAt, now); err != nil {
			return err
		}
		for _, field := range g.Clear {
			col, ok := clearCols[field]
			if !ok {
				continue
			}
			if _, err := q.q.ExecContext(ctx, `UPDATE event_guests SET `+col+` = NULL WHERE event_id = ? AND email = ?`, eventID, g.Email); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReflectionIn is one survey response to upsert.
type ReflectionIn struct {
	ID          string
	Email       string
	SubmittedAt int64
}

// UpsertReflections inserts or refreshes the submission time by (event, email).
func (q *Queries) UpsertReflections(ctx context.Context, eventID string, rs []ReflectionIn, now int64) error {
	st, err := q.q.PrepareContext(ctx, `INSERT INTO reflections (id, event_id, email, submitted_at, rating, created_at) VALUES (?,?,?,?,NULL,?)
		ON CONFLICT (event_id, email) DO UPDATE SET submitted_at = excluded.submitted_at`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rs {
		if _, err := st.ExecContext(ctx, r.ID, eventID, r.Email, r.SubmittedAt, now); err != nil {
			return err
		}
	}
	return nil
}
