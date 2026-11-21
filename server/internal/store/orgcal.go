package store

import (
	"context"
	"encoding/json"
)

type OrgCalendar struct {
	Key              string  `json:"key"`
	Name             string  `json:"name"`
	GoogleCalendarID *string `json:"googleCalendarId"`
	Hue              string  `json:"hue"`
	Locked           bool    `json:"locked"`
}
type OrgInvitee struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}
type OrgReminder struct {
	Method  string `json:"method"`
	Minutes int    `json:"minutes"`
}
type OrgEvent struct {
	ID            string        `json:"id"`
	CalendarKey   string        `json:"calendarKey"`
	Title         string        `json:"title"`
	Description   string        `json:"description"`
	Location      string        `json:"location"`
	StartsAt      *string       `json:"startsAt"`
	EndsAt        *string       `json:"endsAt"`
	AllDay        bool          `json:"allDay"`
	TentativeFrom *string       `json:"tentativeFrom"`
	TentativeTo   *string       `json:"tentativeTo"`
	RRule         *string       `json:"rrule"`
	Reminders     []OrgReminder `json:"reminders"`
	SendInvites   bool          `json:"sendInvites"`
	Invitees      []OrgInvitee  `json:"invitees"`
	SourceKind    string        `json:"sourceKind"`
	SourceID      *string       `json:"sourceId"`
	SourceHref    *string       `json:"sourceHref"`
	GoogleEventID *string       `json:"googleEventId"`
	GoogleETag    *string       `json:"googleEtag"`
	CreatedBy     *string       `json:"createdBy"`
	UpdatedBy     *string       `json:"updatedBy"`
	CreatedAt     int64         `json:"createdAt"`
	UpdatedAt     int64         `json:"updatedAt"`
	DeletedAt     *int64        `json:"-"`
	Revision      int64         `json:"-"`
	Recurrence    []string      `json:"-"`
	TimeZone      string        `json:"-"`
}

func (q *Queries) OrgCalendars(ctx context.Context) ([]OrgCalendar, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT key,name,google_calendar_id,hue,locked FROM org_calendars ORDER BY CASE key WHEN 'internal' THEN 0 WHEN 'external' THEN 1 WHEN 'programs' THEN 2 ELSE 3 END`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgCalendar{}
	for rows.Next() {
		var c OrgCalendar
		if err := rows.Scan(&c.Key, &c.Name, &c.GoogleCalendarID, &c.Hue, &c.Locked); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const orgEventCols = `id,calendar_key,title,description,location,starts_at,ends_at,all_day,tentative_from,tentative_to,rrule,reminders,send_invites,source_kind,source_id,google_event_id,google_etag,created_by,updated_by,created_at,updated_at,deleted_at,revision,recurrence,timezone`

func scanOrgEvent(s interface{ Scan(...any) error }) (*OrgEvent, error) {
	var e OrgEvent
	var rem, rec string
	err := s.Scan(&e.ID, &e.CalendarKey, &e.Title, &e.Description, &e.Location, &e.StartsAt, &e.EndsAt, &e.AllDay, &e.TentativeFrom, &e.TentativeTo, &e.RRule, &rem, &e.SendInvites, &e.SourceKind, &e.SourceID, &e.GoogleEventID, &e.GoogleETag, &e.CreatedBy, &e.UpdatedBy, &e.CreatedAt, &e.UpdatedAt, &e.DeletedAt, &e.Revision, &rec, &e.TimeZone)
	if err != nil {
		return nil, notFound(err)
	}
	if err = json.Unmarshal([]byte(rec), &e.Recurrence); err != nil {
		return nil, err
	}
	e.Reminders = []OrgReminder{}
	if err = json.Unmarshal([]byte(rem), &e.Reminders); err != nil {
		return nil, err
	}
	e.Invitees = []OrgInvitee{}
	if e.SourceID != nil {
		href := ""
		if e.SourceKind == "event" {
			href = "/events?id=" + *e.SourceID
		}
		if e.SourceKind == "program" {
			href = "/buildspace?tab=programs&program=" + *e.SourceID
		}
		if href != "" {
			e.SourceHref = &href
		}
	}
	return &e, nil
}
func (q *Queries) OrgEvent(ctx context.Context, id string) (*OrgEvent, error) {
	e, err := scanOrgEvent(q.q.QueryRowContext(ctx, `SELECT `+orgEventCols+` FROM org_cal_events WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	rows, err := q.q.QueryContext(ctx, `SELECT kind,ref FROM org_cal_invitees WHERE event_id=? ORDER BY kind,ref`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v OrgInvitee
		if err = rows.Scan(&v.Kind, &v.Ref); err != nil {
			return nil, err
		}
		e.Invitees = append(e.Invitees, v)
	}
	return e, rows.Err()
}
func (q *Queries) OrgEvents(ctx context.Context) ([]OrgEvent, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id FROM org_cal_events WHERE deleted_at IS NULL ORDER BY coalesce(starts_at,tentative_from),id`)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = closeRows(rows); err != nil {
		return nil, err
	}
	out := []OrgEvent{}
	for _, id := range ids {
		e, err := q.OrgEvent(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, nil
}
func (q *Queries) SaveOrgEvent(ctx context.Context, e OrgEvent, enqueue bool) error {
	if e.Reminders == nil {
		e.Reminders = []OrgReminder{}
	}
	raw, err := json.Marshal(e.Reminders)
	if err != nil {
		return err
	}
	if e.Recurrence == nil {
		e.Recurrence = []string{}
	}
	rec, _ := json.Marshal(e.Recurrence)
	if e.TimeZone == "" {
		e.TimeZone = "America/Los_Angeles"
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO org_cal_events (`+orgEventCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)
 ON CONFLICT(id) DO UPDATE SET calendar_key=excluded.calendar_key,title=excluded.title,description=excluded.description,location=excluded.location,starts_at=excluded.starts_at,ends_at=excluded.ends_at,all_day=excluded.all_day,tentative_from=excluded.tentative_from,tentative_to=excluded.tentative_to,rrule=excluded.rrule,reminders=excluded.reminders,send_invites=excluded.send_invites,source_kind=excluded.source_kind,source_id=excluded.source_id,google_event_id=excluded.google_event_id,google_etag=excluded.google_etag,updated_by=excluded.updated_by,updated_at=excluded.updated_at,deleted_at=excluded.deleted_at,revision=org_cal_events.revision+1,recurrence=excluded.recurrence,timezone=excluded.timezone`,
		e.ID, e.CalendarKey, e.Title, e.Description, e.Location, e.StartsAt, e.EndsAt, e.AllDay, e.TentativeFrom, e.TentativeTo, e.RRule, string(raw), e.SendInvites, e.SourceKind, e.SourceID, e.GoogleEventID, e.GoogleETag, e.CreatedBy, e.UpdatedBy, e.CreatedAt, e.UpdatedAt, e.DeletedAt, string(rec), e.TimeZone)
	if err != nil {
		return err
	}
	if _, err = q.q.ExecContext(ctx, `DELETE FROM org_cal_invitees WHERE event_id=?`, e.ID); err != nil {
		return err
	}
	for _, v := range e.Invitees {
		if _, err = q.q.ExecContext(ctx, `INSERT OR IGNORE INTO org_cal_invitees VALUES(?,?,?)`, e.ID, v.Kind, v.Ref); err != nil {
			return err
		}
	}
	if enqueue {
		return q.EnqueueOrgEvent(ctx, e.ID)
	}
	return nil
}
func (q *Queries) EnqueueOrgEvent(ctx context.Context, id string) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO org_cal_outbox(event_id,revision) SELECT id,revision FROM org_cal_events WHERE id=? ON CONFLICT(event_id) DO UPDATE SET revision=excluded.revision,attempts=0,next_attempt_at=0,last_error=NULL`, id)
	return err
}
func (q *Queries) DeleteOrgSource(ctx context.Context, kind, id string, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE org_cal_events SET deleted_at=?,updated_at=?,revision=revision+1 WHERE source_kind=? AND source_id=?`, now, now, kind, id)
	if err != nil {
		return err
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO org_cal_outbox(event_id,revision) SELECT id,revision FROM org_cal_events WHERE source_kind=? AND source_id=? ON CONFLICT(event_id) DO UPDATE SET revision=excluded.revision,attempts=0,next_attempt_at=0`, kind, id)
	return err
}

type ProgramTrack struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Hue  string `json:"hue"`
}
type ProgramException struct {
	ProgramID   string  `json:"programId"`
	OnDate      string  `json:"onDate"`
	Kind        string  `json:"kind"`
	NewStartsAt *string `json:"newStartsAt"`
	NewEndsAt   *string `json:"newEndsAt"`
	NewLocation *string `json:"newLocation"`
}
type Program struct {
	ID          string             `json:"id"`
	TrackID     string             `json:"trackId"`
	Name        string             `json:"name"`
	Weekday     int                `json:"weekday"`
	StartTime   string             `json:"startTime"`
	DurationMin int                `json:"durationMin"`
	Location    string             `json:"location"`
	StartsOn    string             `json:"startsOn"`
	EndsOn      string             `json:"endsOn"`
	Description string             `json:"description"`
	CreatedAt   int64              `json:"createdAt"`
	UpdatedAt   int64              `json:"updatedAt"`
	Exceptions  []ProgramException `json:"exceptions"`
}

func (q *Queries) ProgramTracks(ctx context.Context) ([]ProgramTrack, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,key,name,hue FROM program_tracks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProgramTrack{}
	for rows.Next() {
		var v ProgramTrack
		if err = rows.Scan(&v.ID, &v.Key, &v.Name, &v.Hue); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (q *Queries) SaveProgramTrack(ctx context.Context, t ProgramTrack) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO program_tracks VALUES(?,?,?,?)`, t.ID, t.Key, t.Name, t.Hue)
	return err
}
func (q *Queries) Programs(ctx context.Context) ([]Program, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,track_id,name,weekday,start_time,duration_min,location,starts_on,ends_on,description,created_at,updated_at FROM programs ORDER BY starts_on,name`)
	if err != nil {
		return nil, err
	}
	out := []Program{}
	for rows.Next() {
		var v Program
		v.Exceptions = []ProgramException{}
		if err = rows.Scan(&v.ID, &v.TrackID, &v.Name, &v.Weekday, &v.StartTime, &v.DurationMin, &v.Location, &v.StartsOn, &v.EndsOn, &v.Description, &v.CreatedAt, &v.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	if err = closeRows(rows); err != nil {
		return nil, err
	}
	for i := range out {
		ex, err := q.ProgramExceptions(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Exceptions = ex
	}
	return out, nil
}
func (q *Queries) Program(ctx context.Context, id string) (*Program, error) {
	all, err := q.Programs(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, ErrNotFound
}
func (q *Queries) SaveProgram(ctx context.Context, p Program) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO programs VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET track_id=excluded.track_id,name=excluded.name,weekday=excluded.weekday,start_time=excluded.start_time,duration_min=excluded.duration_min,location=excluded.location,starts_on=excluded.starts_on,ends_on=excluded.ends_on,description=excluded.description,updated_at=excluded.updated_at`, p.ID, p.TrackID, p.Name, p.Weekday, p.StartTime, p.DurationMin, p.Location, p.StartsOn, p.EndsOn, p.Description, p.CreatedAt, p.UpdatedAt)
	return err
}
func (q *Queries) DeleteProgram(ctx context.Context, id string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM programs WHERE id=?`, id)
	return err
}

func (q *Queries) PruneProgramExceptions(ctx context.Context, p Program) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM program_exceptions WHERE program_id=? AND (on_date<? OR on_date>? OR CAST(strftime('%w',on_date) AS INTEGER)!=?)`, p.ID, p.StartsOn, p.EndsOn, p.Weekday)
	return err
}
func (q *Queries) ProgramExceptions(ctx context.Context, id string) ([]ProgramException, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT program_id,on_date,kind,new_starts_at,new_ends_at,new_location FROM program_exceptions WHERE program_id=? ORDER BY on_date`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProgramException{}
	for rows.Next() {
		var v ProgramException
		if err = rows.Scan(&v.ProgramID, &v.OnDate, &v.Kind, &v.NewStartsAt, &v.NewEndsAt, &v.NewLocation); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (q *Queries) SaveProgramException(ctx context.Context, e ProgramException) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO program_exceptions VALUES(?,?,?,?,?,?) ON CONFLICT(program_id,on_date) DO UPDATE SET kind=excluded.kind,new_starts_at=excluded.new_starts_at,new_ends_at=excluded.new_ends_at,new_location=excluded.new_location`, e.ProgramID, e.OnDate, e.Kind, e.NewStartsAt, e.NewEndsAt, e.NewLocation)
	return err
}
func (q *Queries) DeleteProgramException(ctx context.Context, id, date string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM program_exceptions WHERE program_id=? AND on_date=?`, id, date)
	return err
}
