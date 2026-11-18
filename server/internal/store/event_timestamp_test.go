package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/luma"
)

func TestEventTimestampMigrationBackfillsExactInstants(t *testing.T) {
	db, err := sql.Open(driverName, "file:"+t.TempDir()+"/upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`)
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0028" {
			continue
		}
		raw, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(raw), "--> statement-breakpoint") {
			if strings.TrimSpace(statement) != "" {
				must(statement)
			}
		}
		must(`INSERT INTO schema_migrations VALUES(?,0)`, entry.Name())
	}
	starts := []string{"2026-11-01T01:30:00.123999999-0700", "1969-12-31T23:59:59.999999999Z", "1500-01-01T00:00:00Z", "invalid"}
	for i, start := range starts {
		must(`INSERT INTO events(id,name,starts_at) VALUES(?, 'Legacy', ?)`, fmt.Sprint(i), start)
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for i, wantRaw := range starts {
		var raw string
		var ms sql.NullInt64
		if err := db.QueryRow(`SELECT starts_at,starts_at_ms FROM events WHERE id=?`, fmt.Sprint(i)).Scan(&raw, &ms); err != nil {
			t.Fatal(err)
		}
		at, err := luma.ParseInstant(wantRaw)
		if raw != wantRaw || ms.Valid != (err == nil) || (err == nil && ms.Int64 != at.UnixMilli()) {
			t.Fatalf("%q migrated to %q / %+v", wantRaw, raw, ms)
		}
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migration rerun: %v", err)
	}
}

func TestEventTimestampWritersAndPagePlan(t *testing.T) {
	x := newTDB(t)
	q := On(x.db)
	ctx := context.Background()
	for _, start := range []string{"1969-12-31T23:59:59.999999999Z", "2026-11-01T01:30:00.123999999-07:00", "invalid"} {
		id := x.key()
		if err := q.InsertEvent(ctx, Event{ID: id, Name: id, StartsAt: start}); err != nil {
			t.Fatal(err)
		}
		check := func(want string) {
			t.Helper()
			var raw string
			var ms sql.NullInt64
			if err := x.db.QueryRow(`SELECT starts_at,starts_at_ms FROM events WHERE id=?`, id).Scan(&raw, &ms); err != nil {
				t.Fatal(err)
			}
			at, err := luma.ParseInstant(want)
			if raw != want || ms.Valid != (err == nil) || (err == nil && ms.Int64 != at.UnixMilli()) {
				t.Fatalf("%q: %q / %+v", want, raw, ms)
			}
		}
		check(start)
		if err := q.UpdateEvent(ctx, id, EventPatch{Name: Opt[string]{Set: true, V: "Renamed"}}, 1); err != nil {
			t.Fatal(err)
		}
		check(start)
		changed := "2026-11-01T01:30:00.234999999-08:00"
		if err := q.UpdateEvent(ctx, id, EventPatch{StartsAt: Opt[string]{Set: true, V: changed}}, 2); err != nil {
			t.Fatal(err)
		}
		check(changed)
	}
	for _, filter := range []EventFilter{
		{Limit: 1},
		{Limit: 1, HasCursor: true, CursorMs: 1800000000000, CursorID: "cursor"},
	} {
		recorder := &recordingQ{Q: x.db}
		if _, err := On(recorder).EventPage(ctx, filter); err != nil {
			t.Fatal(err)
		}
		plan := explain(t, x.db, recorder.statements[0], recorder.arguments[0]...)
		if !strings.Contains(plan, "events_starts_ms_id_idx") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatal(plan)
		}
	}
}

func TestEventTimestampColumnPreservesExactTimeBounds(t *testing.T) {
	for _, instant := range []string{"1500-01-01T00:00:00.0000005Z", "1969-12-31T23:59:59.9999995Z", "2026-11-01T01:30:00.123456789-07:00", "2026-11-01T01:30:00.123-08:00"} {
		t.Run(instant, func(t *testing.T) {
			x := newTDB(t)
			q := On(x.db)
			ctx := context.Background()
			cutoff, err := luma.ParseInstant(instant)
			if err != nil {
				t.Fatal(err)
			}
			for i, delta := range []time.Duration{-time.Millisecond, -time.Nanosecond, 0, time.Nanosecond, time.Millisecond} {
				if err := q.InsertEvent(ctx, Event{ID: fmt.Sprint(i), Name: "Boundary", StartsAt: cutoff.Add(delta).Format(time.RFC3339Nano)}); err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				filter EventFilter
				past   bool
			}{
				{EventFilter{When: "past", Now: cutoff}, true},
				{EventFilter{When: "upcoming", Now: cutoff}, false},
				{EventFilter{To: &cutoff}, true},
				{EventFilter{From: &cutoff}, false},
			} {
				rows, err := q.EventPage(ctx, tc.filter)
				want := 3
				if tc.past {
					want = 2
				}
				if err != nil || len(rows) != want {
					t.Fatalf("%+v: %d rows, %v", tc.filter, len(rows), err)
				}
				for _, row := range rows {
					at, _ := luma.ParseInstant(row.StartsAt)
					if at.Before(cutoff) != tc.past {
						t.Fatalf("%s on wrong side of %s", row.StartsAt, instant)
					}
				}
			}
		})
	}
}
