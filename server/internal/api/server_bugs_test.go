package api

import (
	"context"
	"strings"
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

// M-039: CSV import and Luma ingest apply the editor's field limits and chronology check,
// and an event matched through its lu.ma/event/<id> link stores that Luma id.
func TestImportAndIngestValidateEventsAndStoreMatchedLumaID(t *testing.T) {
	e := newEnv(t)
	csv := []formFile{{"file", "guests.csv", "text/csv", "Email,Name\nnew@example.org,New Guest"}}
	long := strings.Repeat("n", 201)
	for name, fields := range map[string]map[string]string{
		"201-character name":    {"kind": "luma_guests", "lumaEventId": "evt-long", "eventName": long, "eventStartsAt": "2026-11-01T10:00:00Z"},
		"whitespace name":       {"kind": "luma_guests", "lumaEventId": "evt-blank", "eventName": "   ", "eventStartsAt": "2026-11-01T10:00:00Z"},
		"101-character luma id": {"kind": "luma_guests", "lumaEventId": strings.Repeat("x", 101), "eventName": "Ok", "eventStartsAt": "2026-11-01T10:00:00Z"},
	} {
		expectStatus(t, "import "+name, e.do("POST", "/api/events/import", formBody{fields: fields, files: csv}, e.officer()), 400)
	}
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "ingest", "scopes": []string{"ingest:luma"}}, e.director()))
	tok := bearer(issued.str("plaintext"))
	guests := []any{map[string]any{"email": "g@example.org"}}
	expectStatus(t, "ingest ends before it starts", e.post("/api/ingest/luma", map[string]any{
		"event": map[string]any{"lumaId": "evt-order", "name": "Backwards", "startsAt": "2026-11-01T10:00:00Z", "endsAt": "2026-11-01T09:00:00Z"}, "guests": guests}, tok), 400)
	expectStatus(t, "ingest 201-character name", e.post("/api/ingest/luma", map[string]any{
		"event": map[string]any{"lumaId": "evt-n", "name": long, "startsAt": "2026-11-01T10:00:00Z"}, "guests": guests}, tok), 400)

	// An officer pasted the lu.ma/event link before any import.
	linked := e.mustOK(e.post("/api/events", map[string]any{"name": "Linked first", "startsAt": "2026-11-05T18:00:00-08:00", "lumaUrl": "https://lu.ma/event/evt-linked1"}, e.officer())).str("event", "id")
	got := e.mustOK(e.post("/api/ingest/luma", map[string]any{"event": map[string]any{"lumaId": "evt-linked1", "name": "Linked first", "startsAt": "2026-11-06T02:00:00Z"}, "guests": guests}, tok))
	if got.str("eventId") != linked {
		t.Fatalf("ingest made a new event %s instead of %s", got.str("eventId"), linked)
	}
	if ev, err := e.db.Read().EventByLumaID(context.Background(), "evt-linked1"); err != nil || ev.ID != linked {
		t.Fatalf("EventByLumaID after ingest: %v %v", ev, err)
	}
	// The CSV path stores it too.
	csvLinked := e.mustOK(e.post("/api/events", map[string]any{"name": "CSV linked", "startsAt": "2026-11-07T18:00:00-08:00", "lumaUrl": "https://lu.ma/event/evt-csv01"}, e.officer())).str("event", "id")
	e.mustOK(e.do("POST", "/api/events/import", formBody{fields: map[string]string{"kind": "luma_guests", "lumaEventId": "evt-csv01"}, files: csv}, e.officer()))
	if ev, err := e.db.Read().EventByLumaID(context.Background(), "evt-csv01"); err != nil || ev.ID != csvLinked {
		t.Fatalf("EventByLumaID after CSV import: %v %v", ev, err)
	}
}
