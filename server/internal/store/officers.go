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

// Delivery is what one destination (an .ics address, or one Google account) last received
// for an event. Status "sent" means the destination has the invite; Cancelled means it has
// since been withdrawn there. Error is the latest attempt's failure, kept beside the last
// success so a failed update never hides where the invite already is.
//
// AttemptSequence is set while a send is in flight: it is written before the send, with
// AttemptHash (the fingerprint of the content being sent), and both are cleared when its
// outcome is recorded. A row that still has them after a crash may or may not have
// reached the destination, so a deletion cancels it too (with a newer SEQUENCE), and the
// next delivery replays it: the same content with the same SEQUENCE, or newer content
// with a newer one. Status "pending" is such a row with no earlier success. For Google, GoogleEventID is also written before the first insert (a stable id
// the dashboard chooses), so a repeated insert finds the first copy instead of adding one.
type Delivery struct {
	EventID       string  `json:"eventId"`
	UserID        string  `json:"userId"`
	Destination   string  `json:"destination"`
	Mode          string  `json:"mode"`
	Recipient     *string `json:"recipient,omitempty"`
	GoogleEventID *string `json:"googleEventId,omitempty"`
	Sequence      int     `json:"sequence"`
	Status        string  `json:"status"`
	AttemptSeq    *int    `json:"attemptSequence,omitempty"`
	AttemptHash   *string `json:"attemptHash,omitempty"`
	Cancelled     bool    `json:"cancelled"`
	Error         *string `json:"error,omitempty"`
	ContentHash   *string `json:"contentHash,omitempty"`
	UpdatedAt     int64   `json:"updatedAt"`
}

// Live reports whether the destination still shows the event as scheduled.
func (d Delivery) Live() bool { return d.Status == "sent" && !d.Cancelled }

// Unresolved reports a send whose outcome was never recorded (see AttemptSeq).
func (d Delivery) Unresolved() bool { return d.AttemptSeq != nil }

// NeedsCancel reports whether withdrawing the event must reach this destination: it has
// the invite, or may have it from an unresolved send.
func (d Delivery) NeedsCancel() bool { return d.Live() || d.Unresolved() }

// Replays reports whether sending content with fingerprint hash repeats this row's
// unresolved attempt, which then goes out again with the SEQUENCE it was recorded with.
func (d Delivery) Replays(hash string) bool {
	return d.AttemptSeq != nil && d.AttemptHash != nil && *d.AttemptHash == hash
}

// NextSequence is the SEQUENCE for the next message to this destination: newer than
// anything it was sent, including an unresolved attempt.
func (d Delivery) NextSequence() int {
	n := d.Sequence
	if d.AttemptSeq != nil && *d.AttemptSeq > n {
		n = *d.AttemptSeq
	}
	return n + 1
}

const deliveryCols = `event_id,user_id,destination,mode,recipient,google_event_id,sequence,status,attempt_sequence,attempt_hash,cancelled,error,content_hash,updated_at`

func scanDelivery(r interface{ Scan(...any) error }) (Delivery, error) {
	var d Delivery
	var at Ms
	err := r.Scan(&d.EventID, &d.UserID, &d.Destination, &d.Mode, &d.Recipient, &d.GoogleEventID, &d.Sequence, &d.Status, &d.AttemptSeq, &d.AttemptHash, &d.Cancelled, &d.Error, &d.ContentHash, &at)
	d.UpdatedAt = int64(at)
	return d, err
}

// DeliveryFor is the delivery of an event to one destination of an officer.
func (q *Queries) DeliveryFor(ctx context.Context, eventID, userID, destination string) (*Delivery, error) {
	d, err := scanDelivery(q.q.QueryRowContext(ctx, `SELECT `+deliveryCols+` FROM calendar_deliveries WHERE event_id=? AND user_id=? AND destination=?`, eventID, userID, destination))
	if err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// DeliveriesForEvent lists every destination an event was delivered to, for one officer or
// (userID "") all of them. Read it before deleting the event: the rows cascade with it.
func (q *Queries) DeliveriesForEvent(ctx context.Context, eventID, userID string) ([]Delivery, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+deliveryCols+` FROM calendar_deliveries WHERE event_id=? AND (?='' OR user_id=?) ORDER BY user_id, destination`, eventID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DestinationUsed reports whether anything was ever sent or attempted to one of an
// officer's destinations, for any event still on the dashboard. A destination with that
// history had a working transport, so when the transport is missing now it is a
// temporary gap and its work is kept for later; one without it never had one.
func (q *Queries) DestinationUsed(ctx context.Context, userID, destination string) (bool, error) {
	var used bool
	err := q.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM calendar_deliveries WHERE user_id=? AND destination=?)`, userID, destination).Scan(&used)
	return used, err
}

// DeleteDelivery removes one destination row (an attempt that never went out).
func (q *Queries) DeleteDelivery(ctx context.Context, eventID, userID, destination string) error {
	return q.Exec(ctx, `DELETE FROM calendar_deliveries WHERE event_id=? AND user_id=? AND destination=?`, eventID, userID, destination)
}

func (q *Queries) SaveDelivery(ctx context.Context, d Delivery) error {
	return q.Exec(ctx, `INSERT INTO calendar_deliveries(`+deliveryCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(event_id,user_id,destination) DO UPDATE SET mode=excluded.mode,recipient=excluded.recipient,google_event_id=excluded.google_event_id,
		sequence=excluded.sequence,status=excluded.status,attempt_sequence=excluded.attempt_sequence,attempt_hash=excluded.attempt_hash,cancelled=excluded.cancelled,error=excluded.error,content_hash=excluded.content_hash,updated_at=excluded.updated_at`,
		d.EventID, d.UserID, d.Destination, d.Mode, d.Recipient, d.GoogleEventID, d.Sequence, d.Status, d.AttemptSeq, d.AttemptHash, d.Cancelled, d.Error, d.ContentHash, d.UpdatedAt)
}

// ─── Google tokens ───

// GoogleToken is an officer's Google Calendar grant. AccountSub is the ID token's sub,
// Google's stable id for the account; every grant since V0.14.1 has one. AccountEmail is
// for display only.
type GoogleToken struct {
	UserID        string
	AccountSub    *string
	AccountEmail  *string
	SealedRefresh string
	SealedAccess  *string
	AccessExpires *int64
	ConnectedAt   int64
}

// Destination is the calendar_deliveries destination of this grant's calendar:
// "google:sub:<sub>". A grant from before V0.14.1 has no sub and is keyed by its
// lowercased email, or by "google:" when Google sent none, which is how migration 0027
// keyed its deliveries; reconnecting moves those rows (AdoptLegacyGoogleDeliveries).
func (t GoogleToken) Destination() string {
	if t.AccountSub != nil && *t.AccountSub != "" {
		return "google:sub:" + *t.AccountSub
	}
	email := ""
	if t.AccountEmail != nil {
		email = strings.ToLower(*t.AccountEmail)
	}
	return "google:" + email
}

// AdoptLegacyGoogleDeliveries moves an officer's deliveries from destinations keyed the
// pre-V0.14.1 way onto the account of grant t (which has a sub), so reconnecting the same
// account updates the events it already has instead of adding copies. The legacy key
// does not prove which account those events are on: rows keyed "google:" never knew it,
// and an email is not a stable account id (Google may give it to another account, whose
// sub then differs). So both kinds move with their fingerprint cleared: the next
// delivery updates each stored Google event id, which on the same account changes
// nothing visible, and where t's account is a different one (Google answers 404 or 410)
// it creates the event on t's calendar. Rows keyed by another email are another account
// and stay where they are. A row whose event already has a row for t's destination is
// left alone.
func (q *Queries) AdoptLegacyGoogleDeliveries(ctx context.Context, t GoogleToken) error {
	dest := t.Destination()
	if !strings.HasPrefix(dest, "google:sub:") {
		return nil
	}
	legacy := []any{"google:"}
	if t.AccountEmail != nil && *t.AccountEmail != "" {
		legacy = append(legacy, "google:"+strings.ToLower(*t.AccountEmail))
	}
	for _, from := range legacy {
		if err := q.Exec(ctx, `UPDATE OR IGNORE calendar_deliveries SET destination=?, content_hash=NULL WHERE user_id=? AND destination=?`, dest, t.UserID, from); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queries) GoogleTokenFor(ctx context.Context, userID string) (*GoogleToken, error) {
	var t GoogleToken
	var exp *Ms
	var at Ms
	err := q.q.QueryRowContext(ctx, `SELECT user_id,account_sub,account_email,sealed_refresh_token,sealed_access_token,access_expires_at,connected_at FROM google_calendar_tokens WHERE user_id=?`, userID).
		Scan(&t.UserID, &t.AccountSub, &t.AccountEmail, &t.SealedRefresh, &t.SealedAccess, &exp, &at)
	if err != nil {
		return nil, notFound(err)
	}
	t.AccessExpires, t.ConnectedAt = msPtr(exp), int64(at)
	return &t, nil
}

func (q *Queries) SaveGoogleToken(ctx context.Context, t GoogleToken) error {
	return q.Exec(ctx, `INSERT INTO google_calendar_tokens(user_id,account_sub,account_email,sealed_refresh_token,sealed_access_token,access_expires_at,connected_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET account_sub=excluded.account_sub,account_email=excluded.account_email,sealed_refresh_token=excluded.sealed_refresh_token,
		sealed_access_token=excluded.sealed_access_token,access_expires_at=excluded.access_expires_at,connected_at=excluded.connected_at`,
		t.UserID, t.AccountSub, t.AccountEmail, t.SealedRefresh, t.SealedAccess, t.AccessExpires, t.ConnectedAt)
}

func (q *Queries) UpdateGoogleAccess(ctx context.Context, userID, sealedAccess string, expires int64) error {
	return q.Exec(ctx, `UPDATE google_calendar_tokens SET sealed_access_token=?,access_expires_at=? WHERE user_id=?`, sealedAccess, expires, userID)
}

func (q *Queries) DeleteGoogleToken(ctx context.Context, userID string) error {
	return q.Exec(ctx, `DELETE FROM google_calendar_tokens WHERE user_id=?`, userID)
}
