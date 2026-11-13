package store

import (
	"context"
	"testing"
)

func TestMyHostingStats(t *testing.T) {
	db, err := Open(t.TempDir() + "/stats.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	const now = int64(1_800_000_000_000) // 2027-01-15
	me, other := "u-me", "u-other"
	err = db.Write(ctx, func(q *Queries) error {
		for _, id := range []string{me, other, "u-none"} {
			if err := q.InsertUser(ctx, User{ID: id, DiscordID: "d" + id, Name: id, Role: "officer", Active: true}); err != nil {
				return err
			}
		}
		evs := []Event{
			{ID: "old", Name: "Old", StartsAt: "2026-10-01T18:00:00-07:00", HostUserID: &me, Status: "completed"},
			{ID: "recent", Name: "Recent", StartsAt: "2026-12-01T18:00:00-07:00", HostUserID: &me, Status: "published"},
			{ID: "future", Name: "Future", StartsAt: "2027-06-01T18:00:00-07:00", HostUserID: &me, Status: "published"},
			{ID: "draft", Name: "Draft", StartsAt: "2026-12-20T18:00:00-07:00", HostUserID: &me, Status: "draft"},
			{ID: "cancelled", Name: "Cancelled", StartsAt: "2026-12-25T18:00:00-07:00", HostUserID: &me, Status: "cancelled"},
			{ID: "theirs", Name: "Theirs", StartsAt: "2026-12-10T18:00:00-07:00", HostUserID: &other, Status: "completed"},
		}
		for _, e := range evs {
			if err := q.InsertEvent(ctx, e); err != nil {
				return err
			}
		}
		at := int64(1)
		n := 0
		g := func(email string, checked bool) GuestIn {
			n++
			x := GuestIn{ID: "g" + string(rune('a'+n)), Email: email}
			if checked {
				x.CheckedInAt = &at
			}
			return x
		}
		if err := q.UpsertGuests(ctx, "old", []GuestIn{g("a@x.com", true), g("b@x.com", true), g("c@x.com", false)}, 1); err != nil {
			return err
		}
		if err := q.UpsertGuests(ctx, "recent", []GuestIn{g("a@x.com", true)}, 1); err != nil {
			return err
		}
		// Not counted: other host, future, draft, cancelled.
		for _, id := range []string{"theirs", "future", "draft", "cancelled"} {
			if err := q.UpsertGuests(ctx, id, []GuestIn{g("z@x.com", true)}, 1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := db.Read().MyHostingStats(ctx, me, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.EventsHosted != 2 || s.Attendees != 3 {
		t.Fatalf("counts %+v", s)
	}
	if s.LastEvent == nil || s.LastEvent.ID != "recent" || s.LastEvent.Name != "Recent" || s.LastEvent.StartsAt != "2026-12-01T18:00:00-07:00" {
		t.Fatalf("last %+v", s.LastEvent)
	}

	z, err := db.Read().MyHostingStats(ctx, "u-none", now)
	if err != nil {
		t.Fatal(err)
	}
	if z.EventsHosted != 0 || z.Attendees != 0 || z.LastEvent != nil {
		t.Fatalf("zero case %+v", z)
	}
}
