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

// M-020: the first profile save during setup keeps the title a director gave the account.
func TestFirstProfileSaveKeepsDirectorTitle(t *testing.T) {
	e := newEnv(t)
	created := e.mustOK(e.post("/api/admin/users", map[string]any{"discordId": "100000000000000098", "name": "Tess Reyes", "subteamId": "team-events", "title": "Treasurer"}, e.director()))
	id := created.str("user", "id")
	if id == "" {
		t.Fatalf("created %s", created.Raw)
	}
	e.mustOK(e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0199", "birthday": "03-04"}, e.as(id)))
	if got := e.mustOK(e.get("/api/officers/"+id, e.director())).str("officer", "title"); got != "Treasurer" {
		t.Fatalf("title after first profile save = %q, want Treasurer", got)
	}
	// Choosing a title later still replaces it.
	e.mustOK(e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0199", "birthday": "03-04", "title": "Finance lead"}, e.as(id)))
	if got := e.mustOK(e.get("/api/officers/"+id, e.director())).str("officer", "title"); got != "Finance lead" {
		t.Fatalf("title after choosing one = %q", got)
	}
}
