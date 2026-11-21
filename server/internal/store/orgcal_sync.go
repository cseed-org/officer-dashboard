package store

import (
	"context"
	"encoding/json"
	"strings"
)

type OrgGoogleAccount struct {
	Subject, Email, SealedRefresh, SealedAccess string
	ExpiresAt                                   int64
	ConnectedBy                                 *string
}

func (q *Queries) OrgGoogleAccount(ctx context.Context) (*OrgGoogleAccount, error) {
	var a OrgGoogleAccount
	err := q.q.QueryRowContext(ctx, `SELECT subject,email,sealed_refresh,sealed_access,expires_at,connected_by FROM org_google_account WHERE id=1`).Scan(&a.Subject, &a.Email, &a.SealedRefresh, &a.SealedAccess, &a.ExpiresAt, &a.ConnectedBy)
	return &a, notFound(err)
}
func (q *Queries) SaveOrgGoogleAccount(ctx context.Context, a OrgGoogleAccount) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO org_google_account VALUES(1,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET subject=excluded.subject,email=excluded.email,sealed_refresh=excluded.sealed_refresh,sealed_access=excluded.sealed_access,expires_at=excluded.expires_at,connected_by=excluded.connected_by`, a.Subject, a.Email, a.SealedRefresh, a.SealedAccess, a.ExpiresAt, a.ConnectedBy)
	return err
}
func (q *Queries) DisconnectOrgGoogle(ctx context.Context) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM org_google_account`)
	return err
}
func (q *Queries) ResetOrgGoogle(ctx context.Context) error {
	_, err := q.q.ExecContext(ctx, `UPDATE org_calendars SET google_calendar_id=NULL; DELETE FROM org_cal_sync_state; DELETE FROM org_cal_overrides; UPDATE org_cal_events SET google_event_id=NULL,google_etag=NULL; INSERT INTO org_cal_outbox(event_id,revision) SELECT id,revision FROM org_cal_events WHERE deleted_at IS NULL ON CONFLICT(event_id) DO UPDATE SET revision=excluded.revision,attempts=0,next_attempt_at=0;`)
	return err
}

type OrgRemoteOverride struct{ ID, MasterID, OriginalStart, Payload string }

func (q *Queries) ClearOrgOverrides(ctx context.Context, key string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM org_cal_overrides WHERE calendar_key=?`, key)
	return err
}
func (q *Queries) SaveOrgOverride(ctx context.Context, key string, o OrgRemoteOverride) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO org_cal_overrides VALUES(?,?,?,?,?) ON CONFLICT(calendar_key,google_event_id) DO UPDATE SET master_google_id=excluded.master_google_id,original_start=excluded.original_start,payload=excluded.payload`, key, o.ID, o.MasterID, o.OriginalStart, o.Payload)
	return err
}
func (q *Queries) OrgOverrides(ctx context.Context, key, master string) ([]OrgRemoteOverride, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT google_event_id,master_google_id,original_start,payload FROM org_cal_overrides WHERE calendar_key=? AND master_google_id=?`, key, master)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgRemoteOverride{}
	for rows.Next() {
		var o OrgRemoteOverride
		if err = rows.Scan(&o.ID, &o.MasterID, &o.OriginalStart, &o.Payload); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (q *Queries) SetOrgCalendarID(ctx context.Context, key, id string) error {
	_, err := q.q.ExecContext(ctx, `UPDATE org_calendars SET google_calendar_id=? WHERE key=?`, id, key)
	return err
}

type OrgCalendarJob struct {
	EventID  string
	Revision int64
	Attempts int
}

func (q *Queries) RetryOrgJobs(ctx context.Context) error {
	_, err := q.q.ExecContext(ctx, `UPDATE org_cal_outbox SET next_attempt_at=0`)
	return err
}

func (q *Queries) OrgCalendarJobs(ctx context.Context, now int64) ([]OrgCalendarJob, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT event_id,revision,attempts FROM org_cal_outbox WHERE next_attempt_at<=? ORDER BY next_attempt_at,event_id LIMIT 100`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrgCalendarJob{}
	for rows.Next() {
		var j OrgCalendarJob
		if err = rows.Scan(&j.EventID, &j.Revision, &j.Attempts); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (q *Queries) FinishOrgJob(ctx context.Context, j OrgCalendarJob, id, etag string) error {
	// Metadata may be recorded after a concurrent edit; acknowledge only the sent revision.
	if id != "" {
		if _, err := q.q.ExecContext(ctx, `UPDATE org_cal_events SET google_event_id=?,google_etag=? WHERE id=?`, id, etag, j.EventID); err != nil {
			return err
		}
	}
	_, err := q.q.ExecContext(ctx, `DELETE FROM org_cal_outbox WHERE event_id=? AND revision=?`, j.EventID, j.Revision)
	return err
}
func (q *Queries) FailOrgJob(ctx context.Context, j OrgCalendarJob, next int64, message string) error {
	_, err := q.q.ExecContext(ctx, `UPDATE org_cal_outbox SET attempts=attempts+1,next_attempt_at=?,last_error=? WHERE event_id=? AND revision=?`, next, message, j.EventID, j.Revision)
	return err
}
func (q *Queries) OrgSyncToken(ctx context.Context, key string) (string, error) {
	var token *string
	err := q.q.QueryRowContext(ctx, `SELECT sync_token FROM org_cal_sync_state WHERE calendar_key=?`, key).Scan(&token)
	if notFound(err) == ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", nil
	}
	return *token, nil
}
func (q *Queries) SetOrgSyncToken(ctx context.Context, key, token string) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO org_cal_sync_state VALUES(?,?) ON CONFLICT(calendar_key) DO UPDATE SET sync_token=excluded.sync_token`, key, token)
	return err
}
func (q *Queries) OrgEventByGoogle(ctx context.Context, key, id string) (*OrgEvent, error) {
	var local string
	err := q.q.QueryRowContext(ctx, `SELECT id FROM org_cal_events WHERE calendar_key=? AND google_event_id=?`, key, id).Scan(&local)
	if err != nil {
		return nil, notFound(err)
	}
	return q.OrgEvent(ctx, local)
}
func (q *Queries) OrgEventPending(ctx context.Context, id string) (bool, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM org_cal_outbox WHERE event_id=?`, id).Scan(&n)
	return n > 0, err
}
func (q *Queries) OrgACLGeneration(ctx context.Context) (int64, error) {
	var n int64
	err := q.q.QueryRowContext(ctx, `SELECT generation FROM org_cal_acl_state WHERE id=1`).Scan(&n)
	return n, err
}

// Expand invitees against the live People/Officers roster when a job is sent.
func (q *Queries) OrgInviteeEmails(ctx context.Context, id string) ([]string, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT lower(trim(ref)) FROM org_cal_invitees WHERE event_id=? AND kind='email'
 UNION SELECT lower(trim(u.email)) FROM users u JOIN org_cal_invitees i ON i.event_id=? AND ((i.kind='user' AND i.ref=u.id) OR (i.kind='subteam' AND EXISTS(SELECT 1 FROM officer_profiles p WHERE p.user_id=u.id AND p.subteam_id=i.ref))) WHERE u.active=1 AND u.email IS NOT NULL`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			return nil, err
		}
		if strings.Contains(s, "@") {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// Full resync removes only Google-origin rows absent remotely. Locally pending writes win.
func (q *Queries) OrgFullResync(ctx context.Context, key string, ids []string, now int64) error {
	raw, _ := json.Marshal(ids)
	_, err := q.q.ExecContext(ctx, `UPDATE org_cal_events SET deleted_at=?,updated_at=? WHERE calendar_key=? AND source_kind='manual' AND google_event_id IS NOT NULL AND google_event_id NOT IN (SELECT value FROM json_each(?)) AND id NOT IN (SELECT event_id FROM org_cal_outbox)`, now, now, key, string(raw))
	return err
}
