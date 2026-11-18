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

// M-075: a weekly entry dated later this week does not complete today's check-in (the
// builder's thisWeek and the mentor's awaiting flag both use weekStatus).
func TestWeekStatusIgnoresFutureDatedEntries(t *testing.T) {
	pid := "b1"
	sched := store.BuildspaceSchedule{Today: "2026-11-17", Week: store.BuildspaceWeek{Start: "2026-11-16", Due: "2026-11-22"}}
	person := &store.BuildspacePerson{ID: pid, JoinedOn: "2026-09-01"}
	with := func(dates ...string) *store.Buildspace {
		b := &store.Buildspace{}
		for _, d := range dates {
			b.Activity = append(b.Activity, store.BuildspaceActivity{Kind: "weekly", PersonID: &pid, Date: d})
		}
		return b
	}
	if got := weekStatus(with("2026-11-22"), sched, person); got != "awaiting" {
		t.Errorf("future-only entry: %q, want awaiting", got)
	}
	past := sched
	past.PastDue = true
	if got := weekStatus(with("2026-11-22"), past, person); got != "past-due" {
		t.Errorf("future-only entry past due: %q, want past-due", got)
	}
	for _, d := range []string{"2026-11-16", "2026-11-17"} {
		if got := weekStatus(with(d), sched, person); got != "done" {
			t.Errorf("entry on %s: %q, want done", d, got)
		}
	}
}

// M-082: with a frozen clock, a builder archived on their own in the same millisecond as
// their project's archive stays archived when the project is restored.
func TestProjectRestoreKeepsIndependentlyArchivedBuilder(t *testing.T) {
	e := newEnv(t) // e.clock does not move between requests
	d := e.director()
	e.mustOK(e.post("/api/buildspace-launchpad/people/bs-person-01/archive", map[string]any{}, d))
	e.mustOK(e.post("/api/buildspace-launchpad/projects/bs-moss/archive", map[string]any{}, d))
	e.mustOK(e.post("/api/buildspace-launchpad/projects/bs-moss/restore", map[string]any{}, d))
	if v := e.scalar(`SELECT archived_at IS NOT NULL FROM buildspace_launchpad_people WHERE id='bs-person-01'`); v != int64(1) {
		t.Errorf("independently archived builder was restored with the project")
	}
	if v := e.scalar(`SELECT archived_at IS NULL FROM buildspace_launchpad_people WHERE id='bs-person-02'`); v != int64(1) {
		t.Errorf("builder archived with the project was not restored")
	}
}
