package store

import (
	"context"
	"fmt"
	"strings"

	"officerdashboard/internal/accounting"
)

// Request is a purchase_requests row.
type Request struct {
	ID               string
	Number           string
	RequesterID      string
	Type             string
	Title            string
	Vendor           string
	ItemURL          *string
	Quantity         int64
	UnitCostCents    int64
	TaxShipCents     int64
	TotalCents       int64
	ActualTotalCents *int64
	ExpenseAccountID string
	FundID           string
	EventID          *string
	FunctionalClass  string
	Justification    string
	NeededBy         *string
	Status           string
	SubmittedAt      *int64
	DecidedAt        *int64
	CreatedAt        Ms
	UpdatedAt        Ms
}

const requestCols = `id, number, requester_id, type, title, vendor, item_url, quantity, unit_cost_cents, tax_ship_cents, total_cents, actual_total_cents,
	expense_account_id, fund_id, event_id, functional_class, justification, needed_by, status, submitted_at, decided_at, created_at, updated_at`

func scanRequest(s interface{ Scan(...any) error }) (*Request, error) {
	var r Request
	var sub, dec *Ms
	err := s.Scan(&r.ID, &r.Number, &r.RequesterID, &r.Type, &r.Title, &r.Vendor, &r.ItemURL, &r.Quantity, &r.UnitCostCents, &r.TaxShipCents, &r.TotalCents, &r.ActualTotalCents,
		&r.ExpenseAccountID, &r.FundID, &r.EventID, &r.FunctionalClass, &r.Justification, &r.NeededBy, &r.Status, &sub, &dec, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if sub != nil {
		v := int64(*sub)
		r.SubmittedAt = &v
	}
	if dec != nil {
		v := int64(*dec)
		r.DecidedAt = &v
	}
	return &r, nil
}

// RequestFilter selects purchase requests for a list.
type RequestFilter struct {
	RequesterID string // "" = everyone
	EventID     string
	Q           string // case-insensitive substring of title, vendor or number
	Status      string // "" = any
	Limit       int    // 0 = no limit
	// Cursor continues after this (created_at, id), newest first.
	CursorCreatedAt int64
	CursorID        string
	HasCursor       bool
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (f RequestFilter) where(withStatus bool) (string, []any) {
	var conds []string
	var args []any
	if f.RequesterID != "" {
		conds = append(conds, "requester_id = ?")
		args = append(args, f.RequesterID)
	}
	if f.EventID != "" {
		conds = append(conds, "event_id = ?")
		args = append(args, f.EventID)
	}
	if f.Q != "" {
		conds = append(conds, `lower(title || ' ' || vendor || ' ' || number) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(strings.ToLower(f.Q))+"%")
	}
	if withStatus && f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, f.Status)
	}
	if withStatus && f.HasCursor {
		conds = append(conds, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, f.CursorCreatedAt, f.CursorCreatedAt, f.CursorID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// RequestCounts counts requests per status over the filter, ignoring status and cursor.
func (q *Queries) RequestCounts(ctx context.Context, f RequestFilter) (map[string]int, error) {
	where, args := f.where(false)
	rows, err := q.q.QueryContext(ctx, `SELECT status, count(*) FROM purchase_requests`+where+` GROUP BY status`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// Requests lists requests newest first.
func (q *Queries) Requests(ctx context.Context, f RequestFilter) ([]Request, error) {
	where, args := f.where(true)
	query := `SELECT ` + requestCols + ` FROM purchase_requests` + where + ` ORDER BY created_at DESC, id DESC`
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	return q.requests(ctx, query, args...)
}

// AllRequests loads every request (finance reports need them all).
func (q *Queries) AllRequests(ctx context.Context) ([]Request, error) {
	return q.requests(ctx, `SELECT `+requestCols+` FROM purchase_requests ORDER BY rowid`)
}

// RequestsByEvent lists the requests linked to an event.
func (q *Queries) RequestsByEvent(ctx context.Context, eventID string) ([]Request, error) {
	return q.requests(ctx, `SELECT `+requestCols+` FROM purchase_requests WHERE event_id = ? ORDER BY created_at DESC, id DESC`, eventID)
}

func (q *Queries) requests(ctx context.Context, query string, args ...any) ([]Request, error) {
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// RequestByID returns ErrNotFound for an unknown id.
func (q *Queries) RequestByID(ctx context.Context, id string) (*Request, error) {
	r, err := scanRequest(q.q.QueryRowContext(ctx, `SELECT `+requestCols+` FROM purchase_requests WHERE id = ?`, id))
	return r, notFound(err)
}

// InsertRequest adds a request and allocates its number. Numbers are `<FY>-<NNNN>`,
// sequential within the fiscal year; the next one is computed inside the INSERT itself and
// the write transaction serialises allocation. Legacy non-numeric suffixes (e.g.
// 2026-C2552D9F) are ignored by the GLOB filters. The stored number is returned.
func (q *Queries) InsertRequest(ctx context.Context, r Request, fiscalYear int) (string, error) {
	_, err := q.q.ExecContext(ctx, `INSERT INTO purchase_requests (id, number, requester_id, type, title, vendor, item_url, quantity, unit_cost_cents, tax_ship_cents, total_cents,
		expense_account_id, fund_id, event_id, functional_class, justification, needed_by, status, submitted_at, created_at, updated_at)
		VALUES (?, (SELECT printf('%d-%04d', ?, coalesce(max(CAST(substr(number, 6) AS INTEGER)), 0) + 1) FROM purchase_requests WHERE number GLOB ? AND substr(number, 6) NOT GLOB '*[^0-9]*'),
		?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, fiscalYear, fmt.Sprintf("%d-[0-9]*", fiscalYear),
		r.RequesterID, r.Type, r.Title, r.Vendor, r.ItemURL, r.Quantity, r.UnitCostCents, r.TaxShipCents, r.TotalCents,
		r.ExpenseAccountID, r.FundID, r.EventID, r.FunctionalClass, r.Justification, r.NeededBy, r.Status, r.SubmittedAt, int64(r.CreatedAt), int64(r.UpdatedAt))
	if err != nil {
		return "", err
	}
	var number string
	err = q.q.QueryRowContext(ctx, `SELECT number FROM purchase_requests WHERE id = ?`, r.ID).Scan(&number)
	return number, err
}

// UpdateDraftFields rewrites the editable fields of a request (drafts only; the caller checks).
func (q *Queries) UpdateDraftFields(ctx context.Context, r Request, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE purchase_requests SET type=?, title=?, vendor=?, item_url=?, quantity=?, unit_cost_cents=?, tax_ship_cents=?, total_cents=?,
		expense_account_id=?, fund_id=?, event_id=?, functional_class=?, justification=?, needed_by=?, updated_at=? WHERE id = ?`,
		r.Type, r.Title, r.Vendor, r.ItemURL, r.Quantity, r.UnitCostCents, r.TaxShipCents, r.TotalCents,
		r.ExpenseAccountID, r.FundID, r.EventID, r.FunctionalClass, r.Justification, r.NeededBy, now, r.ID)
	return err
}

// DeleteRequestWithAttachments removes a draft and its attachment rows.
func (q *Queries) DeleteRequestWithAttachments(ctx context.Context, id string) error {
	if _, err := q.q.ExecContext(ctx, `DELETE FROM attachments WHERE request_id = ?`, id); err != nil {
		return err
	}
	_, err := q.q.ExecContext(ctx, `DELETE FROM purchase_requests WHERE id = ?`, id)
	return err
}

// StatusChange is the set of columns a lifecycle move writes.
type StatusChange struct {
	Status           string
	UpdatedAt        int64
	SubmittedAt      *int64
	DecidedAt        *int64
	ActualTotalCents *int64
}

// SetRequestStatus applies a lifecycle move.
func (q *Queries) SetRequestStatus(ctx context.Context, id string, c StatusChange) error {
	_, err := q.q.ExecContext(ctx, `UPDATE purchase_requests SET status = ?, updated_at = ?,
		submitted_at = coalesce(?, submitted_at), decided_at = coalesce(?, decided_at), actual_total_cents = coalesce(?, actual_total_cents) WHERE id = ?`,
		c.Status, c.UpdatedAt, c.SubmittedAt, c.DecidedAt, c.ActualTotalCents, id)
	return err
}

// Approval is an approvals row.
type Approval struct {
	ID         string
	RequestID  string
	DirectorID string
	Decision   string
	Comment    *string
	CreatedAt  Ms
}

// InsertApproval records a decision.
func (q *Queries) InsertApproval(ctx context.Context, a Approval) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO approvals (id, request_id, director_id, decision, comment, created_at) VALUES (?,?,?,?,?,?)`,
		a.ID, a.RequestID, a.DirectorID, a.Decision, a.Comment, int64(a.CreatedAt))
	return err
}

// Approvals lists a request's decisions, oldest first.
func (q *Queries) Approvals(ctx context.Context, requestID string) ([]Approval, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id, request_id, director_id, decision, comment, created_at FROM approvals WHERE request_id = ? ORDER BY created_at, rowid`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var a Approval
		if err := rows.Scan(&a.ID, &a.RequestID, &a.DirectorID, &a.Decision, &a.Comment, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Attachment is an attachments row.
type Attachment struct {
	ID           string
	RequestID    *string
	JournalEntry *string
	Kind         string
	Filename     string
	MimeType     string
	SizeBytes    int64
	Storage      string
	StorageKey   string
	WebViewLink  *string
	UploadedBy   string
	CreatedAt    Ms
}

const attachmentCols = `id, request_id, journal_entry_id, kind, filename, mime_type, size_bytes, storage, storage_key, web_view_link, uploaded_by, created_at`

func scanAttachment(s interface{ Scan(...any) error }) (*Attachment, error) {
	var a Attachment
	err := s.Scan(&a.ID, &a.RequestID, &a.JournalEntry, &a.Kind, &a.Filename, &a.MimeType, &a.SizeBytes, &a.Storage, &a.StorageKey, &a.WebViewLink, &a.UploadedBy, &a.CreatedAt)
	return &a, err
}

// InsertAttachment records an uploaded file.
func (q *Queries) InsertAttachment(ctx context.Context, a Attachment) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO attachments (`+attachmentCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.RequestID, a.JournalEntry, a.Kind, a.Filename, a.MimeType, a.SizeBytes, a.Storage, a.StorageKey, a.WebViewLink, a.UploadedBy, int64(a.CreatedAt))
	return err
}

// AttachmentByID returns ErrNotFound for an unknown id.
func (q *Queries) AttachmentByID(ctx context.Context, id string) (*Attachment, error) {
	a, err := scanAttachment(q.q.QueryRowContext(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE id = ?`, id))
	return a, notFound(err)
}

// RequestAttachments lists a request's attachments, oldest first.
func (q *Queries) RequestAttachments(ctx context.Context, requestID string) ([]Attachment, error) {
	return q.attachments(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE request_id = ? ORDER BY created_at, rowid`, requestID)
}

// AttachmentsByIDs fetches several attachments.
func (q *Queries) AttachmentsByIDs(ctx context.Context, ids []string) ([]Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return q.attachments(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE id IN (`+marks+`)`, args...)
}

func (q *Queries) attachments(ctx context.Context, query string, args ...any) ([]Attachment, error) {
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// LinkAttachments ties receipts to the journal entry that posted them.
func (q *Queries) LinkAttachments(ctx context.Context, ids []string, entryID string) error {
	for _, id := range ids {
		if _, err := q.q.ExecContext(ctx, `UPDATE attachments SET journal_entry_id = ? WHERE id = ?`, entryID, id); err != nil {
			return err
		}
	}
	return nil
}

// EntryLinks tie a posted entry to what produced it.
type EntryLinks struct {
	PurchaseRequestID string
	SettlementKey     string
	CreatedBy         string
	PostedBy          string
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// InsertPostedEntry writes an engine-validated entry through the real draft → lines → post
// path, so the integrity triggers re-check balance and the fiscal period.
func (q *Queries) InsertPostedEntry(ctx context.Context, e accounting.Entry, links EntryLinks, now int64, newLineID func() string) error {
	if _, err := q.q.ExecContext(ctx, `INSERT INTO journal_entries (id, entry_date, memo, status, source, purchase_request_id, reverses_entry_id, settlement_key, created_by, created_at)
		VALUES (?,?,?, 'draft', ?,?,?,?,?,?)`,
		e.ID, e.EntryDate, e.Memo, e.Source, nz(links.PurchaseRequestID), nz(e.ReversesEntryID), nz(links.SettlementKey), nz(links.CreatedBy), now); err != nil {
		return err
	}
	for i, l := range e.Lines {
		if _, err := q.q.ExecContext(ctx, `INSERT INTO journal_lines (id, entry_id, line_no, account_id, fund_id, functional_class, debit_cents, credit_cents, memo) VALUES (?,?,?,?,?,?,?,?,?)`,
			newLineID(), e.ID, i+1, l.AccountID, l.FundID, nz(l.FunctionalClass), l.DebitCents, l.CreditCents, nz(l.Memo)); err != nil {
			return err
		}
	}
	_, err := q.q.ExecContext(ctx, `UPDATE journal_entries SET status = 'posted', posted_at = ?, posted_by = ? WHERE id = ?`, now, nz(links.PostedBy), e.ID)
	return err
}

// PostedEntryIDs lists the posted journal entries of a request.
func (q *Queries) PostedEntryIDs(ctx context.Context, requestID string) ([]string, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id FROM journal_entries WHERE purchase_request_id = ? AND status = 'posted' ORDER BY rowid`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ─── fulfilment jobs ───

// Job is a fulfillment_jobs row.
type Job struct {
	ID               string
	RequestID        string
	Status           string
	ClaimedByTokenID *string
	ClaimedAt        *int64
	Attempts         int
	LastError        *string
	ResultJSON       *string
	PayloadJSON      *string
	CreatedAt        Ms
	UpdatedAt        Ms
}

const jobCols = `id, request_id, status, claimed_by_token_id, claimed_at, attempts, last_error, result_json, payload_json, created_at, updated_at`

func scanJob(s interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var claimed *Ms
	err := s.Scan(&j.ID, &j.RequestID, &j.Status, &j.ClaimedByTokenID, &claimed, &j.Attempts, &j.LastError, &j.ResultJSON, &j.PayloadJSON, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if claimed != nil {
		v := int64(*claimed)
		j.ClaimedAt = &v
	}
	return &j, nil
}

// InsertJob queues a purchasing job.
func (q *Queries) InsertJob(ctx context.Context, j Job) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO fulfillment_jobs (`+jobCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.RequestID, j.Status, j.ClaimedByTokenID, j.ClaimedAt, j.Attempts, j.LastError, j.ResultJSON, j.PayloadJSON, int64(j.CreatedAt), int64(j.UpdatedAt))
	return err
}

// JobsByStatus lists jobs in a status, oldest first.
func (q *Queries) JobsByStatus(ctx context.Context, status string) ([]Job, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+jobCols+` FROM fulfillment_jobs WHERE status = ? ORDER BY created_at, rowid`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// JobByID returns ErrNotFound for an unknown id.
func (q *Queries) JobByID(ctx context.Context, id string) (*Job, error) {
	j, err := scanJob(q.q.QueryRowContext(ctx, `SELECT `+jobCols+` FROM fulfillment_jobs WHERE id = ?`, id))
	return j, notFound(err)
}

// HeldJob returns the request's held job, or ErrNotFound.
func (q *Queries) HeldJob(ctx context.Context, requestID string) (*Job, error) {
	j, err := scanJob(q.q.QueryRowContext(ctx, `SELECT `+jobCols+` FROM fulfillment_jobs WHERE request_id = ? AND status = 'held' ORDER BY rowid LIMIT 1`, requestID))
	return j, notFound(err)
}

// SaveJob rewrites a job's mutable columns.
func (q *Queries) SaveJob(ctx context.Context, j Job) error {
	_, err := q.q.ExecContext(ctx, `UPDATE fulfillment_jobs SET status=?, claimed_by_token_id=?, claimed_at=?, attempts=?, last_error=?, result_json=?, updated_at=? WHERE id = ?`,
		j.Status, j.ClaimedByTokenID, j.ClaimedAt, j.Attempts, j.LastError, j.ResultJSON, int64(j.UpdatedAt), j.ID)
	return err
}

// MoveJobs changes the status of a request's jobs that are currently in one of `from`.
func (q *Queries) MoveJobs(ctx context.Context, requestID string, from []string, to string, now int64) error {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	args := []any{to, now, requestID}
	for _, f := range from {
		args = append(args, f)
	}
	_, err := q.q.ExecContext(ctx, `UPDATE fulfillment_jobs SET status = ?, updated_at = ? WHERE request_id = ? AND status IN (`+marks+`)`, args...)
	return err
}
