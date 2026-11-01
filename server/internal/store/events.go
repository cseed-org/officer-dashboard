package store

import (
	"context"
	"strings"

	"officerdashboard/internal/luma"
)

// Event is an events row.
type Event struct {
	ID         string
	LumaID     *string
	Name       string
	StartsAt   string // ISO with offset, as supplied
	EndsAt     *string
	Location   *string
	URL        *string
	HostUserID *string
	CreatedAt  Ms
	UpdatedAt  Ms
}

const eventCols = `id, luma_id, name, starts_at, ends_at, location, url, host_user_id, created_at, updated_at`

func scanEvent(s interface{ Scan(...any) error }) (*Event, error) {
	var e Event
	if err := s.Scan(&e.ID, &e.LumaID, &e.Name, &e.StartsAt, &e.EndsAt, &e.Location, &e.URL, &e.HostUserID, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

// Events lists every event (callers filter and page: there are tens per year).
func (q *Queries) Events(ctx context.Context) ([]Event, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+eventCols+` FROM events ORDER BY rowid`)
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
	e, err := scanEvent(q.q.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id = ?`, id))
	return e, notFound(err)
}

// EventByLumaID returns ErrNotFound when no event has the Luma id.
func (q *Queries) EventByLumaID(ctx context.Context, lumaID string) (*Event, error) {
	e, err := scanEvent(q.q.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE luma_id = ?`, lumaID))
	return e, notFound(err)
}

// InsertEvent adds an event.
func (q *Queries) InsertEvent(ctx context.Context, e Event) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO events (`+eventCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.LumaID, e.Name, e.StartsAt, e.EndsAt, e.Location, e.URL, e.HostUserID, int64(e.CreatedAt), int64(e.UpdatedAt))
	return err
}

// EventPatch lists the event fields that can change.
type EventPatch struct {
	Name       Opt[string]
	StartsAt   Opt[string]
	EndsAt     Opt[string]
	Location   Opt[string]
	URL        Opt[string]
	LumaID     Opt[string]
	HostUserID Opt[string]
}

// UpdateEvent applies a patch and stamps updated_at.
func (q *Queries) UpdateEvent(ctx context.Context, id string, p EventPatch, now int64) error {
	var sets []string
	var args []any
	add := func(col string, o Opt[string]) {
		if o.Set {
			sets = append(sets, col+" = ?")
			args = append(args, o.val())
		}
	}
	add("name", p.Name)
	add("starts_at", p.StartsAt)
	add("ends_at", p.EndsAt)
	add("location", p.Location)
	add("url", p.URL)
	add("luma_id", p.LumaID)
	add("host_user_id", p.HostUserID)
	sets = append(sets, "updated_at = ?")
	args = append(args, now, id)
	_, err := q.q.ExecContext(ctx, `UPDATE events SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
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
	rows.Close()
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
