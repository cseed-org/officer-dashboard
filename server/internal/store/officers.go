package store

import (
	"context"
	"strings"
)

// Profile is an officer_profiles row. Optional text fields are nil when unset.
type Profile struct {
	UserID        string
	SubteamID     string
	Phone         string
	Birthday      string
	PreferredName *string
	Pronouns      *string
	PersonalEmail *string
	SchoolEmail   *string
	GradDate      *string
	Title         *string
	StartTerm     *string
	Team          *string
	LinkedIn      *string
	DeliveryMode  string
}

const profileCols = `user_id,subteam_id,phone,birthday,preferred_name,pronouns,personal_email,school_email,grad_date,title,start_term,linkedin,calendar_delivery_mode,team`

func scanProfile(s interface{ Scan(...any) error }) (*Profile, error) {
	var p Profile
	err := s.Scan(&p.UserID, &p.SubteamID, &p.Phone, &p.Birthday, &p.PreferredName, &p.Pronouns, &p.PersonalEmail, &p.SchoolEmail, &p.GradDate, &p.Title, &p.StartTerm, &p.LinkedIn, &p.DeliveryMode, &p.Team)
	return &p, err
}

func (q *Queries) ProfileByUser(ctx context.Context, userID string) (*Profile, error) {
	p, err := scanProfile(q.q.QueryRowContext(ctx, `SELECT `+profileCols+` FROM officer_profiles WHERE user_id=?`, userID))
	if err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// Profiles maps user ID to profile for everyone who has one.
func (q *Queries) Profiles(ctx context.Context) (map[string]Profile, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+profileCols+` FROM officer_profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out[p.UserID] = *p
	}
	return out, rows.Err()
}

// SaveProfile inserts or replaces the whole row.
func (q *Queries) SaveProfile(ctx context.Context, p Profile) error {
	if p.DeliveryMode == "" {
		p.DeliveryMode = "ics"
	}
	return q.Exec(ctx, `INSERT INTO officer_profiles(`+profileCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET subteam_id=excluded.subteam_id,phone=excluded.phone,birthday=excluded.birthday,
		preferred_name=excluded.preferred_name,pronouns=excluded.pronouns,personal_email=excluded.personal_email,
		school_email=excluded.school_email,grad_date=excluded.grad_date,title=excluded.title,start_term=excluded.start_term,
		linkedin=excluded.linkedin,calendar_delivery_mode=excluded.calendar_delivery_mode,team=excluded.team`,
		p.UserID, p.SubteamID, p.Phone, p.Birthday, p.PreferredName, p.Pronouns, p.PersonalEmail, p.SchoolEmail, p.GradDate, p.Title, p.StartTerm, p.LinkedIn, p.DeliveryMode, p.Team)
}

// ProfilePatch is what a director may change on someone else's profile.
type ProfilePatch struct {
	SubteamID Opt[string]
	Title     Opt[string]
}

func (q *Queries) PatchProfile(ctx context.Context, userID string, p ProfilePatch) error {
	sets, args := []string{}, []any{}
	if p.SubteamID.Set {
		sets, args = append(sets, "subteam_id=?"), append(args, p.SubteamID.val())
	}
	if p.Title.Set {
		sets, args = append(sets, "title=?"), append(args, p.Title.val())
	}
	if len(sets) == 0 {
		return nil
	}
	res, err := q.q.ExecContext(ctx, `UPDATE officer_profiles SET `+strings.Join(sets, ",")+` WHERE user_id=?`, append(args, userID)...)
	return oneRow(res, err)
}

func (q *Queries) SetDeliveryMode(ctx context.Context, userID, mode string) error {
	res, err := q.q.ExecContext(ctx, `UPDATE officer_profiles SET calendar_delivery_mode=? WHERE user_id=?`, mode, userID)
	return oneRow(res, err)
}

// ─── calendar delivery ───

// CalendarRecipient is an active user with a profile, their mode and active address.
type CalendarRecipient struct {
	UserID      string
	Name        string
	Mode        string
	ActiveEmail *string
}

func (q *Queries) CalendarRecipients(ctx context.Context) ([]CalendarRecipient, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT u.id,u.name,p.calendar_delivery_mode,(SELECT email FROM calendar_emails c WHERE c.user_id=u.id AND c.active=1)
		FROM users u JOIN officer_profiles p ON p.user_id=u.id WHERE u.active=1 ORDER BY u.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarRecipient{}
	for rows.Next() {
		var r CalendarRecipient
		if err := rows.Scan(&r.UserID, &r.Name, &r.Mode, &r.ActiveEmail); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (q *Queries) ActiveCalendarEmail(ctx context.Context, userID string) (*string, error) {
	var e *string
	err := q.q.QueryRowContext(ctx, `SELECT email FROM calendar_emails WHERE user_id=? AND active=1`, userID).Scan(&e)
	if err != nil {
		if err = notFound(err); err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

type Delivery struct {
	EventID       string
	UserID        string
	Mode          string
	Recipient     *string
	GoogleEventID *string
	Sequence      int
	Status        string
	Error         *string
	ContentHash   *string
	UpdatedAt     int64
}

func (q *Queries) DeliveryFor(ctx context.Context, eventID, userID string) (*Delivery, error) {
	var d Delivery
	var at Ms
	err := q.q.QueryRowContext(ctx, `SELECT event_id,user_id,mode,recipient,google_event_id,sequence,status,error,content_hash,updated_at FROM calendar_deliveries WHERE event_id=? AND user_id=?`, eventID, userID).
		Scan(&d.EventID, &d.UserID, &d.Mode, &d.Recipient, &d.GoogleEventID, &d.Sequence, &d.Status, &d.Error, &d.ContentHash, &at)
	if err != nil {
		return nil, notFound(err)
	}
	d.UpdatedAt = int64(at)
	return &d, nil
}

// DeliveriesForEvent snapshots what each officer received for an event (before deleting it:
// the rows cascade with the event).
func (q *Queries) DeliveriesForEvent(ctx context.Context, eventID string) (map[string]Delivery, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT event_id,user_id,mode,recipient,google_event_id,sequence,status,error,content_hash,updated_at FROM calendar_deliveries WHERE event_id=?`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Delivery{}
	for rows.Next() {
		var d Delivery
		var at Ms
		if err := rows.Scan(&d.EventID, &d.UserID, &d.Mode, &d.Recipient, &d.GoogleEventID, &d.Sequence, &d.Status, &d.Error, &d.ContentHash, &at); err != nil {
			return nil, err
		}
		d.UpdatedAt = int64(at)
		out[d.UserID] = d
	}
	return out, rows.Err()
}

func (q *Queries) SaveDelivery(ctx context.Context, d Delivery) error {
	return q.Exec(ctx, `INSERT INTO calendar_deliveries(event_id,user_id,mode,recipient,google_event_id,sequence,status,error,content_hash,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(event_id,user_id) DO UPDATE SET mode=excluded.mode,recipient=excluded.recipient,google_event_id=excluded.google_event_id,
		sequence=excluded.sequence,status=excluded.status,error=excluded.error,content_hash=excluded.content_hash,updated_at=excluded.updated_at`,
		d.EventID, d.UserID, d.Mode, d.Recipient, d.GoogleEventID, d.Sequence, d.Status, d.Error, d.ContentHash, d.UpdatedAt)
}

// ─── Google tokens ───

type GoogleToken struct {
	UserID        string
	AccountEmail  *string
	SealedRefresh string
	SealedAccess  *string
	AccessExpires *int64
	ConnectedAt   int64
}

func (q *Queries) GoogleTokenFor(ctx context.Context, userID string) (*GoogleToken, error) {
	var t GoogleToken
	var exp *Ms
	var at Ms
	err := q.q.QueryRowContext(ctx, `SELECT user_id,account_email,sealed_refresh_token,sealed_access_token,access_expires_at,connected_at FROM google_calendar_tokens WHERE user_id=?`, userID).
		Scan(&t.UserID, &t.AccountEmail, &t.SealedRefresh, &t.SealedAccess, &exp, &at)
	if err != nil {
		return nil, notFound(err)
	}
	t.AccessExpires, t.ConnectedAt = msPtr(exp), int64(at)
	return &t, nil
}

func (q *Queries) SaveGoogleToken(ctx context.Context, t GoogleToken) error {
	return q.Exec(ctx, `INSERT INTO google_calendar_tokens(user_id,account_email,sealed_refresh_token,sealed_access_token,access_expires_at,connected_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET account_email=excluded.account_email,sealed_refresh_token=excluded.sealed_refresh_token,
		sealed_access_token=excluded.sealed_access_token,access_expires_at=excluded.access_expires_at,connected_at=excluded.connected_at`,
		t.UserID, t.AccountEmail, t.SealedRefresh, t.SealedAccess, t.AccessExpires, t.ConnectedAt)
}

func (q *Queries) UpdateGoogleAccess(ctx context.Context, userID, sealedAccess string, expires int64) error {
	return q.Exec(ctx, `UPDATE google_calendar_tokens SET sealed_access_token=?,access_expires_at=? WHERE user_id=?`, sealedAccess, expires, userID)
}

func (q *Queries) DeleteGoogleToken(ctx context.Context, userID string) error {
	return q.Exec(ctx, `DELETE FROM google_calendar_tokens WHERE user_id=?`, userID)
}
