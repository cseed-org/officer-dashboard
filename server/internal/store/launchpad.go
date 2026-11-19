package store

import (
	"context"
	"database/sql"
	"strings"
)

// LaunchpadCohort groups projects and builders by intake (docs/launchpad-cohorts.md).
type LaunchpadCohort struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	StartsOn  string       `json:"startsOn"`
	EndsOn    *string      `json:"endsOn"`
	Status    string       `json:"status"`
	CreatedAt int64        `json:"createdAt"`
	UpdatedAt int64        `json:"updatedAt"`
	Counts    CohortCounts `json:"counts"`
}
type CohortCounts struct {
	Projects int `json:"projects"`
	Builders int `json:"builders"`
	Invited  int `json:"invited"`
}

// LaunchpadAccount is a builder or mentor portal account. It is never a users row.
type LaunchpadAccount struct {
	ID              string  `json:"id"`
	Kind            string  `json:"kind"`
	Email           string  `json:"email"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	CohortID        *string `json:"cohortId"`
	PersonID        *string `json:"personId"`
	ContactID       *string `json:"contactId"`
	DiscordID       *string `json:"-"`
	DiscordLinked   bool    `json:"discordLinked"`
	InviteTokenHash *string `json:"-"`
	InviteExpiresAt *int64  `json:"inviteExpiresAt"`
	InviteExpired   bool    `json:"inviteExpired"`
	InviteSentAt    *int64  `json:"inviteSentAt"`
	InviteError     *string `json:"inviteError"`
	InvitedBy       *string `json:"-"`
	CreatedAt       int64   `json:"createdAt"`
	UpdatedAt       int64   `json:"-"`
	AcceptedAt      *int64  `json:"acceptedAt"`
	OnboardedAt     *int64  `json:"onboardedAt"`
	LastLoginAt     *int64  `json:"lastLoginAt"`
}

// MarkExpired fills InviteExpired against now (milliseconds).
func (a *LaunchpadAccount) MarkExpired(now int64) {
	a.InviteExpired = a.Status == "invited" && a.InviteExpiresAt != nil && *a.InviteExpiresAt <= now
}

// ── cohorts ──

const cohortCols = `c.id,c.name,c.starts_on,c.ends_on,c.status,c.created_at,c.updated_at,
 (SELECT count(*) FROM buildspace_launchpad_projects p WHERE p.cohort_id=c.id AND p.archived_at IS NULL),
 (SELECT count(*) FROM buildspace_launchpad_people b WHERE b.cohort_id=c.id AND b.archived_at IS NULL),
 (SELECT count(*) FROM buildspace_launchpad_accounts a WHERE a.cohort_id=c.id AND a.status='invited')`

func scanCohort(r interface{ Scan(...any) error }) (LaunchpadCohort, error) {
	var c LaunchpadCohort
	err := r.Scan(&c.ID, &c.Name, &c.StartsOn, &c.EndsOn, &c.Status, &c.CreatedAt, &c.UpdatedAt, &c.Counts.Projects, &c.Counts.Builders, &c.Counts.Invited)
	return c, err
}

// LaunchpadCohorts lists cohorts newest first (start date, then creation).
func (q *Queries) LaunchpadCohorts(ctx context.Context) ([]LaunchpadCohort, error) {
	out := []LaunchpadCohort{}
	err := iter(q.query(ctx, `SELECT `+cohortCols+` FROM buildspace_launchpad_cohorts c ORDER BY c.starts_on DESC,c.created_at DESC,c.id`)).each(func(rows *sql.Rows) error {
		c, err := scanCohort(rows)
		out = append(out, c)
		return err
	})
	return out, err
}

func (q *Queries) LaunchpadCohort(ctx context.Context, id string) (LaunchpadCohort, error) {
	c, err := scanCohort(q.q.QueryRowContext(ctx, `SELECT `+cohortCols+` FROM buildspace_launchpad_cohorts c WHERE c.id=?`, id))
	return c, notFound(err)
}

func (q *Queries) LaunchpadCohortNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM buildspace_launchpad_cohorts WHERE lower(name)=lower(?) AND id<>?`, name, exceptID).Scan(&n)
	return n > 0, err
}

func (q *Queries) SaveLaunchpadCohort(ctx context.Context, c LaunchpadCohort) error {
	return q.Exec(ctx, `INSERT INTO buildspace_launchpad_cohorts(id,name,starts_on,ends_on,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,starts_on=excluded.starts_on,ends_on=excluded.ends_on,status=excluded.status,updated_at=excluded.updated_at`,
		c.ID, c.Name, c.StartsOn, c.EndsOn, c.Status, c.CreatedAt, c.UpdatedAt)
}

// LaunchpadCohortEmpty reports whether nothing references the cohort (archived rows count).
func (q *Queries) LaunchpadCohortEmpty(ctx context.Context, id string) (bool, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM buildspace_launchpad_projects WHERE cohort_id=?)+(SELECT count(*) FROM buildspace_launchpad_people WHERE cohort_id=?)+(SELECT count(*) FROM buildspace_launchpad_accounts WHERE cohort_id=?)`, id, id, id).Scan(&n)
	return n == 0, err
}

func (q *Queries) DeleteLaunchpadCohort(ctx context.Context, id string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_cohorts WHERE id=?`, id)
}

// ── accounts ──

const accountCols = `id,kind,email,name,status,cohort_id,person_id,contact_id,discord_id,invite_token_hash,invite_expires_at,invite_sent_at,invite_error,invited_by,created_at,updated_at,accepted_at,onboarded_at,last_login_at`

func scanAccount(r interface{ Scan(...any) error }) (LaunchpadAccount, error) {
	var a LaunchpadAccount
	err := r.Scan(&a.ID, &a.Kind, &a.Email, &a.Name, &a.Status, &a.CohortID, &a.PersonID, &a.ContactID, &a.DiscordID, &a.InviteTokenHash, &a.InviteExpiresAt, &a.InviteSentAt, &a.InviteError, &a.InvitedBy, &a.CreatedAt, &a.UpdatedAt, &a.AcceptedAt, &a.OnboardedAt, &a.LastLoginAt)
	a.DiscordLinked = a.DiscordID != nil
	return a, err
}

func (q *Queries) LaunchpadAccounts(ctx context.Context) ([]LaunchpadAccount, error) {
	out := []LaunchpadAccount{}
	err := iter(q.query(ctx, `SELECT `+accountCols+` FROM buildspace_launchpad_accounts ORDER BY created_at DESC,email`)).each(func(rows *sql.Rows) error {
		a, err := scanAccount(rows)
		out = append(out, a)
		return err
	})
	return out, err
}

func (q *Queries) launchpadAccountWhere(ctx context.Context, where string, args ...any) (LaunchpadAccount, error) {
	a, err := scanAccount(q.q.QueryRowContext(ctx, `SELECT `+accountCols+` FROM buildspace_launchpad_accounts WHERE `+where, args...))
	return a, notFound(err)
}

func (q *Queries) LaunchpadAccount(ctx context.Context, id string) (LaunchpadAccount, error) {
	return q.launchpadAccountWhere(ctx, `id=?`, id)
}
func (q *Queries) LaunchpadAccountByTokenHash(ctx context.Context, hash string) (LaunchpadAccount, error) {
	return q.launchpadAccountWhere(ctx, `invite_token_hash=?`, hash)
}
func (q *Queries) LaunchpadAccountByDiscordID(ctx context.Context, discordID string) (LaunchpadAccount, error) {
	return q.launchpadAccountWhere(ctx, `discord_id=?`, discordID)
}
func (q *Queries) LaunchpadBuilderAccountByEmail(ctx context.Context, cohortID, email string) (LaunchpadAccount, error) {
	return q.launchpadAccountWhere(ctx, `kind='builder' AND cohort_id=? AND email=?`, cohortID, email)
}
func (q *Queries) LaunchpadAccountByContact(ctx context.Context, contactID string) (LaunchpadAccount, error) {
	return q.launchpadAccountWhere(ctx, `contact_id=?`, contactID)
}
func (q *Queries) LaunchpadAccountByPerson(ctx context.Context, personID string) (LaunchpadAccount, error) {
	return q.launchpadAccountWhere(ctx, `person_id=?`, personID)
}

// SaveLaunchpadAccount inserts or fully replaces an account row.
func (q *Queries) SaveLaunchpadAccount(ctx context.Context, a LaunchpadAccount) error {
	return q.Exec(ctx, `INSERT INTO buildspace_launchpad_accounts(`+accountCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET email=excluded.email,name=excluded.name,status=excluded.status,person_id=excluded.person_id,discord_id=excluded.discord_id,
 invite_token_hash=excluded.invite_token_hash,invite_expires_at=excluded.invite_expires_at,invite_sent_at=excluded.invite_sent_at,invite_error=excluded.invite_error,
 invited_by=excluded.invited_by,updated_at=excluded.updated_at,accepted_at=excluded.accepted_at,onboarded_at=excluded.onboarded_at,last_login_at=excluded.last_login_at`,
		a.ID, a.Kind, a.Email, a.Name, a.Status, a.CohortID, a.PersonID, a.ContactID, a.DiscordID, a.InviteTokenHash, a.InviteExpiresAt, a.InviteSentAt, a.InviteError, a.InvitedBy, a.CreatedAt, a.UpdatedAt, a.AcceptedAt, a.OnboardedAt, a.LastLoginAt)
}

// RecordLaunchpadInviteDelivery stores the email outcome, only if the token is still the one sent.
func (q *Queries) RecordLaunchpadInviteDelivery(ctx context.Context, id, tokenHash string, sentAt *int64, errMsg *string) error {
	return q.Exec(ctx, `UPDATE buildspace_launchpad_accounts SET invite_sent_at=?,invite_error=? WHERE id=? AND invite_token_hash=?`, sentAt, errMsg, id, tokenHash)
}

func (q *Queries) DeleteLaunchpadAccount(ctx context.Context, id string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_accounts WHERE id=?`, id)
}

// ── portal sessions ──

type LaunchpadSession struct {
	ID        string
	ExpiresAt int64
	CreatedAt int64
	Account   LaunchpadAccount
}

func (q *Queries) InsertLaunchpadSession(ctx context.Context, idHash, accountID string, expiresAt int64, userAgent string, now int64) error {
	return q.Exec(ctx, `INSERT INTO buildspace_launchpad_sessions(id,account_id,expires_at,created_at,user_agent) VALUES(?,?,?,?,?)`, idHash, accountID, expiresAt, now, userAgent)
}

// LaunchpadSessionByHash returns a live session of an active account, or ErrNotFound.
func (q *Queries) LaunchpadSessionByHash(ctx context.Context, idHash string, now int64) (*LaunchpadSession, error) {
	var s LaunchpadSession
	row := q.queryRow(ctx, `SELECT s.id,s.expires_at,s.created_at,`+accountColsAliased+` FROM buildspace_launchpad_sessions s JOIN buildspace_launchpad_accounts a ON a.id=s.account_id
 WHERE s.id=? AND s.expires_at>? AND a.status='active'`, idHash, now)
	var a LaunchpadAccount
	err := row.Scan(&s.ID, &s.ExpiresAt, &s.CreatedAt, &a.ID, &a.Kind, &a.Email, &a.Name, &a.Status, &a.CohortID, &a.PersonID, &a.ContactID, &a.DiscordID, &a.InviteTokenHash, &a.InviteExpiresAt, &a.InviteSentAt, &a.InviteError, &a.InvitedBy, &a.CreatedAt, &a.UpdatedAt, &a.AcceptedAt, &a.OnboardedAt, &a.LastLoginAt)
	if err != nil {
		return nil, notFound(err)
	}
	a.DiscordLinked = a.DiscordID != nil
	s.Account = a
	return &s, nil
}

var accountColsAliased = "a." + strings.ReplaceAll(accountCols, ",", ",a.")

func (q *Queries) ExtendLaunchpadSession(ctx context.Context, idHash string, expiresAt int64) error {
	return q.Exec(ctx, `UPDATE buildspace_launchpad_sessions SET expires_at=? WHERE id=?`, expiresAt, idHash)
}
func (q *Queries) DeleteLaunchpadSession(ctx context.Context, idHash string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_sessions WHERE id=?`, idHash)
}

// DeleteLaunchpadSessionsFor signs an account out everywhere.
func (q *Queries) DeleteLaunchpadSessionsFor(ctx context.Context, accountID string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_sessions WHERE account_id=?`, accountID)
}

// DeleteLaunchpadSessionsForPeople signs out the portal accounts of these builders.
func (q *Queries) DeleteLaunchpadSessionsForProject(ctx context.Context, projectID string) error {
	return q.Exec(ctx, `DELETE FROM buildspace_launchpad_sessions WHERE account_id IN (SELECT a.id FROM buildspace_launchpad_accounts a JOIN buildspace_launchpad_people b ON b.id=a.person_id WHERE b.project_id=?)`, projectID)
}

func (q *Queries) TouchLaunchpadLogin(ctx context.Context, accountID string, now int64) error {
	return q.Exec(ctx, `UPDATE buildspace_launchpad_accounts SET last_login_at=? WHERE id=?`, now, accountID)
}

// ── mentor settings ──

func (q *Queries) SaveLaunchpadMentorSettings(ctx context.Context, contactID string, available bool, maxProjects *int, now int64) error {
	return q.Exec(ctx, `INSERT INTO buildspace_launchpad_mentor_settings(contact_id,available,max_projects,updated_at) VALUES(?,?,?,?)
 ON CONFLICT(contact_id) DO UPDATE SET available=excluded.available,max_projects=excluded.max_projects,updated_at=excluded.updated_at`, contactID, available, maxProjects, now)
}
