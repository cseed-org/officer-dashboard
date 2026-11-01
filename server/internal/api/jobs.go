package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/money"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/purchases"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

var bearerRE = regexp.MustCompile(`(?i)^Bearer ([^\s]+)$`)

// authenticateToken checks the Bearer service token and its scope. Reads leave token
// state unchanged; mutations stamp last_used_at inside their own transaction.
func (c *Ctx) authenticateToken(scope string) (*store.ServiceToken, error) {
	m := bearerRE.FindStringSubmatch(c.R.Header.Get("Authorization"))
	if m == nil {
		return nil, fail(401, "unauthenticated", "Service token required")
	}
	t, err := c.S.db.Read().TokenByHash(c.Context(), auth.SHA256Hex(m[1]))
	if errors.Is(err, store.ErrNotFound) {
		return nil, fail(401, "unauthenticated", "Invalid or expired service token")
	}
	if err != nil {
		return nil, err
	}
	if t.RevokedAt != nil || (t.ExpiresAt != nil && *t.ExpiresAt <= c.S.nowMs()) {
		return nil, fail(401, "unauthenticated", "Invalid or expired service token")
	}
	has := false
	for _, s := range t.Scopes {
		has = has || s == scope
	}
	if !has {
		return nil, forbidden("Service token lacks required scope")
	}
	c.Token, c.User = t, nil
	return t, nil
}

var jobStatuses = []string{"queued", "claimed", "held", "done", "failed", "cancelled"}

func (c *Ctx) jobView(cat *catalog, q *store.Queries, j *store.Job) (FulfillmentJob, error) {
	r, err := q.RequestByID(c.Context(), j.RequestID)
	if err != nil {
		return FulfillmentJob{}, notFoundErr("Record not found")
	}
	core, err := requestCore(c.Context(), q, cat, r)
	if err != nil {
		return FulfillmentJob{}, err
	}
	return FulfillmentJob{ID: j.ID, Status: j.Status, Attempts: j.Attempts, Request: core, CreatedAt: isoMs(int64(j.CreatedAt)), ClaimedAt: isoPtr(j.ClaimedAt)}, nil
}

func (c *Ctx) agentJobs() error {
	if _, err := c.authenticateToken("agent:jobs"); err != nil {
		return err
	}
	var status string
	if err := c.query(func(r *validate.Reader) {
		status = r.Str("status", validate.S{Enum: jobStatuses, HasDef: true, Default: "queued"}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	jobs, err := q.JobsByStatus(ctx, status)
	if err != nil {
		return err
	}
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		return err
	}
	out := []FulfillmentJob{}
	for i := range jobs {
		v, err := c.jobView(cat, q, &jobs[i])
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	writeJSON(c.W, 200, map[string]any{"jobs": out})
	return nil
}

type completeInput struct {
	Outcome string
	Done    settlement
	Notes   string
	Error   string
	Raw     map[string]any // stored as the job's result_json
}

func readComplete(r *validate.Reader) completeInput {
	out := completeInput{Outcome: r.Str("outcome", validate.S{Req: true, Enum: []string{"done", "failed"}}).V}
	switch out.Outcome {
	case "done":
		out.Done.ActualCents = r.Int("actualTotalCents", validate.I{Req: true, Min: validate.Lo(0)}).V
		out.Done.Attachments = r.Strings("attachmentIds", false, 0, 20, nil, 64)
		out.Done.OrderRef = r.Str("orderReference", validate.S{Max: 200}).V
		out.Notes = r.Str("notes", validate.S{Max: 2000}).V
		out.Done.Comment = out.Notes
		ids := out.Done.Attachments
		if ids == nil {
			ids = []string{}
		}
		out.Raw = map[string]any{"outcome": "done", "actualTotalCents": out.Done.ActualCents, "attachmentIds": ids}
		if out.Done.OrderRef != "" {
			out.Raw["orderReference"] = out.Done.OrderRef
		}
		if out.Notes != "" {
			out.Raw["notes"] = out.Notes
		}
	case "failed":
		out.Error = r.Str("error", validate.S{Req: true, Min: 1, Max: 2000}).V
		out.Raw = map[string]any{"outcome": "failed", "error": out.Error}
	}
	return out
}

func (c *Ctx) agentJobClaim() error {
	tok, err := c.authenticateToken("agent:jobs")
	if err != nil {
		return err
	}
	ctx := c.Context()
	var view FulfillmentJob
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		j, err := w.JobByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		r, err := w.RequestByID(ctx, j.RequestID)
		if err != nil {
			return notFoundErr("Record not found")
		}
		if j.Status != "queued" || r.Status != "approved" {
			return conflict("Job is not available to claim")
		}
		if j.Attempts >= 3 {
			return conflict("Job reached three attempts; a director must resolve the request manually")
		}
		now := c.S.nowMs()
		before := jobSnapshot(j)
		j.Status, j.ClaimedByTokenID, j.ClaimedAt, j.Attempts, j.UpdatedAt = "claimed", &tok.ID, &now, j.Attempts+1, store.Ms(now)
		if err := w.SetRequestStatus(ctx, r.ID, store.StatusChange{Status: "fulfilling", UpdatedAt: now}); err != nil {
			return err
		}
		if err := w.SaveJob(ctx, *j); err != nil {
			return err
		}
		if err := w.TouchToken(ctx, tok.ID, now); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "job.claimed", "fulfillment_job", j.ID, before, jobSnapshot(j)); err != nil {
			return err
		}
		cat, err := loadCatalog(ctx, w, true)
		if err != nil {
			return err
		}
		view, err = c.jobView(cat, w, j)
		return err
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"job": view})
	return nil
}

func (c *Ctx) agentJobComplete() error {
	tok, err := c.authenticateToken("agent:jobs")
	if err != nil {
		return err
	}
	var in completeInput
	if err := c.jsonBody(func(r *validate.Reader) { in = readComplete(r) }); err != nil {
		return err
	}
	ctx := c.Context()
	var view FulfillmentJob
	var heldMsg string
	var heldNote *notify.Event
	var doneReq *store.Request
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		j, err := w.JobByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		r, err := w.RequestByID(ctx, j.RequestID)
		if err != nil {
			return notFoundErr("Record not found")
		}
		if j.Status != "claimed" || j.ClaimedByTokenID == nil || *j.ClaimedByTokenID != tok.ID || r.Status != "fulfilling" {
			return conflict("Only the token holding this active claim may complete it")
		}
		now := c.S.nowMs()
		before := jobSnapshot(j)
		raw, _ := json.Marshal(in.Raw)
		result := string(raw)
		if in.Outcome == "done" {
			// Automation may not book more than the approved estimate plus tolerance. The
			// attempt is recorded and the request stays `fulfilling` for a director to settle.
			ceiling := purchases.MaxAgentSettlementCents(r.TotalCents, c.S.cfg.Tolerance)
			if in.Done.ActualCents > ceiling {
				heldMsg = fmt.Sprintf("Actual %s exceeds the approved %s plus tolerance (max %s); held for director review",
					money.Format(in.Done.ActualCents), money.Format(r.TotalCents), money.Format(ceiling))
				// Terminal for the agent: the claim is cleared and the job is out of `queued`,
				// so neither a later complete/failed nor a claim can re-queue it and trigger a
				// second purchase.
				j.Status, j.ClaimedByTokenID, j.ClaimedAt, j.LastError, j.ResultJSON, j.UpdatedAt = "held", nil, nil, &heldMsg, &result, store.Ms(now)
				if err := w.SaveJob(ctx, *j); err != nil {
					return err
				}
				if err := c.audit(ctx, w, "job.held_for_review", "fulfillment_job", j.ID, before, jobSnapshot(j)); err != nil {
					return err
				}
				heldNote = &notify.Event{Type: "request.held", RequestID: r.ID, Number: r.Number, Title: r.Title,
					AttemptedCents: notify.Cents(in.Done.ActualCents), ApprovedCents: notify.Cents(r.TotalCents), Reason: heldMsg}
				return nil // commit the hold, then answer 409 below
			}
		}
		j.ResultJSON, j.UpdatedAt = &result, store.Ms(now)
		var reqChange store.StatusChange
		if in.Outcome == "failed" {
			j.Status = "failed"
			if j.Attempts < 3 {
				j.Status = "queued"
			}
			j.ClaimedByTokenID, j.ClaimedAt = nil, nil
			j.LastError = &in.Error
			reqChange = store.StatusChange{Status: "approved", UpdatedAt: now}
		} else {
			j.Status, j.LastError = "done", nil
			if err := c.settleWrites(ctx, w, r, in.Done); err != nil {
				return err
			}
			a := in.Done.ActualCents
			reqChange = store.StatusChange{Status: "fulfilled", UpdatedAt: now, ActualTotalCents: &a}
		}
		if err := w.SetRequestStatus(ctx, r.ID, reqChange); err != nil {
			return err
		}
		if err := w.SaveJob(ctx, *j); err != nil {
			return err
		}
		if err := w.TouchToken(ctx, tok.ID, now); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "job."+j.Status, "fulfillment_job", j.ID, before, jobSnapshot(j)); err != nil {
			return err
		}
		if j.Status == "done" {
			doneReq, err = w.RequestByID(ctx, r.ID)
			if err != nil {
				return err
			}
		}
		cat, err := loadCatalog(ctx, w, true)
		if err != nil {
			return err
		}
		view, err = c.jobView(cat, w, j)
		return err
	})
	if err != nil {
		return err
	}
	if heldNote != nil {
		c.S.cfg.Notifier.Notify(ctx, *heldNote, c.S.link("/requests/"+heldNote.RequestID, c.R))
		return conflict(heldMsg)
	}
	if doneReq != nil {
		c.notify(doneReq, "status", "")
	}
	writeJSON(c.W, 200, map[string]any{"job": view})
	return nil
}
