package api

import (
	"context"
	"database/sql"
	"testing"

	"officerdashboard/internal/luma"
)

func TestEveryEventWritePathKeepsTimestampColumnInSync(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	check := func(id, want string) {
		t.Helper()
		var raw string
		var ms sql.NullInt64
		if err := e.db.R.QueryRow(`SELECT starts_at,starts_at_ms FROM events WHERE id=?`, id).Scan(&raw, &ms); err != nil {
			t.Fatal(err)
		}
		at, err := luma.ParseInstant(want)
		if err != nil || raw != want || !ms.Valid || ms.Int64 != at.UnixMilli() {
			t.Fatalf("%s: got %q / %+v, want %q (%v)", id, raw, ms, want, err)
		}
	}
	// The seed uses its own guarded SQL insert, rather than store.InsertEvent.
	seeded, err := e.db.Read().Events(ctx)
	if err != nil || len(seeded) == 0 {
		t.Fatalf("seed: %d events, %v", len(seeded), err)
	}
	for _, event := range seeded {
		check(event.ID, event.StartsAt)
	}
	start := "2026-12-01T19:00:00.123-08:00"
	id := e.mustOK(e.post("/api/events", map[string]any{"name": "Timestamp create", "startsAt": start}, e.officer())).str("event", "id")
	check(id, start)
	updated := "2026-12-02T19:00:00.456-08:00"
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"startsAt": updated}, e.director()))
	check(id, updated)
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"location": "Room 5"}, e.director()))
	check(id, updated)
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "timestamp import", "scopes": []string{"ingest:luma"}}, e.director()))
	for _, instant := range []string{"2026-11-01T01:30:00.123-07:00", "2026-11-01T01:30:00.456-08:00"} {
		e.mustOK(e.post("/api/ingest/luma", map[string]any{"event": map[string]any{"lumaId": "timestamp-ingest", "name": "Ingest timestamp", "startsAt": instant}, "guests": []any{}}, bearer(issued.str("plaintext"))))
		event, err := e.db.Read().EventByLumaID(ctx, "timestamp-ingest")
		if err != nil {
			t.Fatal(err)
		}
		check(event.ID, instant)
	}
	importStart := "2026-12-03T19:00:00.789-08:00"
	e.mustOK(e.do("POST", "/api/events/import", formBody{
		fields: map[string]string{"kind": "luma_guests", "lumaEventId": "timestamp-csv", "eventName": "CSV timestamp", "eventStartsAt": importStart},
		files:  []formFile{{"file", "guests.csv", "text/csv", "Name,Email\nAnn,ann@example.org\n"}},
	}, e.officer()))
	event, err := e.db.Read().EventByLumaID(ctx, "timestamp-csv")
	if err != nil {
		t.Fatal(err)
	}
	check(event.ID, importStart)
}
