package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// RequestDetails batches the five lookups used for job DTOs. Each query is restricted
// to the selected jobs' requests; unrelated purchase/attachment history stays in SQLite.
type RequestDetails struct {
	Requests    map[string]*Request
	Approvals   map[string][]Approval
	Attachments map[string][]Attachment
	Entries     map[string][]string
	Held        map[string]*Job
}

func (q *Queries) RequestDetails(ctx context.Context, ids []string) (RequestDetails, error) {
	out := RequestDetails{map[string]*Request{}, map[string][]Approval{}, map[string][]Attachment{}, map[string][]string{}, map[string]*Job{}}
	if len(ids) == 0 {
		return out, nil
	}
	raw, _ := json.Marshal(ids)
	arg := string(raw)
	requests, err := q.requests(ctx, `SELECT `+requestCols+` FROM purchase_requests WHERE id IN (SELECT value FROM json_each(?))`, arg)
	if err != nil {
		return out, err
	}
	for i := range requests {
		out.Requests[requests[i].ID] = &requests[i]
	}
	err = iter(q.q.QueryContext(ctx, `SELECT id,request_id,director_id,decision,comment,created_at FROM approvals WHERE request_id IN (SELECT value FROM json_each(?)) ORDER BY created_at,rowid`, arg)).each(func(rows *sql.Rows) error {
		var a Approval
		if err := rows.Scan(&a.ID, &a.RequestID, &a.DirectorID, &a.Decision, &a.Comment, &a.CreatedAt); err != nil {
			return err
		}
		out.Approvals[a.RequestID] = append(out.Approvals[a.RequestID], a)
		return nil
	})
	if err != nil {
		return out, err
	}
	attachments, err := q.attachments(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE request_id IN (SELECT value FROM json_each(?)) ORDER BY created_at,rowid`, arg)
	if err != nil {
		return out, err
	}
	for _, a := range attachments {
		out.Attachments[*a.RequestID] = append(out.Attachments[*a.RequestID], a)
	}
	err = iter(q.q.QueryContext(ctx, `SELECT purchase_request_id,id FROM journal_entries WHERE purchase_request_id IN (SELECT value FROM json_each(?)) AND status='posted' ORDER BY rowid`, arg)).each(func(rows *sql.Rows) error {
		var rid, id string
		if err := rows.Scan(&rid, &id); err != nil {
			return err
		}
		out.Entries[rid] = append(out.Entries[rid], id)
		return nil
	})
	if err != nil {
		return out, err
	}
	err = iter(q.q.QueryContext(ctx, `SELECT `+jobCols+` FROM fulfillment_jobs WHERE request_id IN (SELECT value FROM json_each(?)) AND status='held' ORDER BY rowid`, arg)).each(func(rows *sql.Rows) error {
		j, err := scanJob(rows)
		if err != nil {
			return err
		}
		if out.Held[j.RequestID] == nil {
			out.Held[j.RequestID] = j
		}
		return nil
	})
	return out, err
}
