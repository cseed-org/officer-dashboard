package api

import (
	"context"
	"testing"

	"officerdashboard/internal/accounting"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// M-123: a restricted gift posted with a future date is not available to a purchase settled today.
func TestRestrictedAvailabilityIgnoresFutureDatedGifts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	today := "2026-10-31"
	var before int64
	if err := e.db.Write(ctx, func(w *store.Queries) error {
		var err error
		before, err = w.AvailableRestricted(ctx, "fund-buildspace", today)
		if err != nil {
			return err
		}
		return w.InsertPostedEntry(ctx, accounting.Entry{ID: "je-future-gift", EntryDate: "2026-11-20", Memo: "Pledged gift", Source: "contribution",
			Lines: []accounting.Line{
				{AccountID: "acct-1000", FundID: "fund-buildspace", DebitCents: 900_000},
				{AccountID: "acct-4110", FundID: "fund-buildspace", CreditCents: 900_000},
			}}, store.EntryLinks{}, e.clock.UnixMilli(), auth.UUID)
	}); err != nil {
		t.Fatal(err)
	}
	if before <= 0 {
		t.Fatalf("seeded availability %d", before)
	}
	made := e.mustOK(e.post("/api/requests", with(purchase, "type", "reimbursement", "fundId", "fund-buildspace", "submit", true), e.officer()))
	id := made.str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
	over := before + 100_000 // covered only by the future-dated gift
	expectStatus(t, "settle against a future gift", e.post("/api/requests/"+id+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": over}, e.director()), 409)
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 8000}, e.director()))
}
