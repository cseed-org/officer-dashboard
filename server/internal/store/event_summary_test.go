package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestEventSummaryIncludesHistoryBeyondPage(t *testing.T) {
	x := newTDB(t)
	ctx := context.Background()
	q := On(x.db)
	now := Ms(time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC).UnixMilli()) // November in Pacific time, before DST ends.
	end := Ms(time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC).UnixMilli())
	if got, err := q.EventSummary(ctx, now, now, end); err != nil || got != (EventTotals{}) {
		t.Fatalf("empty: %+v %v", got, err)
	}
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("history-%03d", i)
		if err := q.InsertEvent(ctx, Event{ID: id, Name: id, StartsAt: "2025-01-01T00:00:00Z", Status: "completed"}); err != nil {
			t.Fatal(err)
		}
		if _, err := x.db.Exec(`INSERT INTO event_guests(id,event_id,email,checked_in_at,created_at) VALUES(?,?,?,1,1)`, id, id, id+"@example.org"); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []Event{
		{ID: "draft", Name: "Draft", StartsAt: "2026-11-20T00:00:00Z", Status: "draft"},
		{ID: "cancel", Name: "Cancelled", StartsAt: "2026-11-20T00:00:00Z", Status: "cancelled"},
		{ID: "month", Name: "This month", StartsAt: "2026-11-30T23:59:59-08:00"},
		{ID: "future", Name: "Next month", StartsAt: "2026-12-01T00:00:00-08:00"},
		{ID: "past", Name: "Before local month", StartsAt: "2026-11-01T06:59:59Z"},
		// Tentative Events are left out of time-based counts.
		{ID: "tentative", Name: "Tentative", TentativeFrom: strp("2026-11-24"), TentativeTo: strp("2026-11-30"), Status: "published"},
	} {
		if err := q.InsertEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := q.EventSummary(ctx, now, now, end)
	if err != nil {
		t.Fatal(err)
	}
	want := EventTotals{Upcoming: 2, ThisMonth: 1, Attendees: 205, Drafts: 1}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	page, err := q.EventPage(ctx, EventFilter{When: "all", Limit: 1, Now: time.UnixMilli(int64(now))})
	if err != nil || len(page) != 1 {
		t.Fatalf("page %d: %v", len(page), err)
	}
	again, err := q.EventSummary(ctx, now, now, end)
	if err != nil || again != want {
		t.Fatalf("page changed summary: %+v %v", again, err)
	}
}

func strp(s string) *string { return &s }
