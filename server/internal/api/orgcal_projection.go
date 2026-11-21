package api

import (
	"context"
	"errors"
	"officerdashboard/internal/store"
	"strings"
	"time"
)

func orgTZ() *time.Location   { loc, _ := time.LoadLocation("America/Los_Angeles"); return loc }
func (s *Server) projectProgram(ctx context.Context, q *store.Queries, p store.Program) error {
	start, _ := time.ParseInLocation("2006-01-02 15:04", p.StartsOn+" "+p.StartTime, orgTZ())
	for int(start.Weekday()) != p.Weekday {
		start = start.AddDate(0, 0, 1)
	}
	end := start.Add(time.Duration(p.DurationMin) * time.Minute)
	until, _ := time.ParseInLocation("2006-01-02", p.EndsOn, orgTZ())
	until = until.AddDate(0, 0, 1).Add(-time.Second)
	e := store.OrgEvent{ID: "program-" + p.ID, CalendarKey: "programs", Title: p.Name, Description: p.Description, Location: p.Location, StartsAt: strPtr(start.Format(time.RFC3339)), EndsAt: strPtr(end.Format(time.RFC3339)), RRule: strPtr("RRULE:FREQ=WEEKLY;UNTIL=" + until.UTC().Format("20060102T150405Z")), SourceKind: "program", SourceID: &p.ID, CreatedAt: p.CreatedAt, UpdatedAt: s.nowMs(), Reminders: []store.OrgReminder{}, Invitees: []store.OrgInvitee{}}
	if old, err := q.OrgEvent(ctx, e.ID); err == nil {
		e.GoogleEventID = old.GoogleEventID
		e.GoogleETag = old.GoogleETag
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return q.SaveOrgEvent(ctx, e, true)
}
func (s *Server) projectOrgEvent(ctx context.Context, q *store.Queries, id string) error {
	ev, err := q.EventByID(ctx, id)
	if err != nil {
		return err
	}
	if ev.Status == "cancelled" {
		return q.DeleteOrgSource(ctx, "event", id, s.nowMs())
	}
	e := store.OrgEvent{ID: "event-" + id, CalendarKey: "events", Title: ev.Name, SourceKind: "event", SourceID: &id, CreatedAt: int64(ev.CreatedAt), UpdatedAt: s.nowMs(), Reminders: []store.OrgReminder{}, Invitees: []store.OrgInvitee{}}
	if ev.Status == "draft" {
		e.Title = "[Draft] " + e.Title
	}
	if ev.Description != nil {
		e.Description = *ev.Description
	}
	if ev.Location != nil {
		e.Location = *ev.Location
	}
	if ev.StartsAt != "" {
		e.StartsAt = &ev.StartsAt
		e.EndsAt = ev.EndsAt
		if e.EndsAt == nil {
			t, err := time.Parse(time.RFC3339Nano, ev.StartsAt)
			if err != nil {
				return err
			}
			e.EndsAt = strPtr(t.Add(time.Hour).Format(time.RFC3339))
		}
	} else {
		e.TentativeFrom = ev.TentativeFrom
		e.TentativeTo = ev.TentativeTo
		e.AllDay = true
	}
	if old, err := q.OrgEvent(ctx, e.ID); err == nil {
		e.GoogleEventID = old.GoogleEventID
		e.GoogleETag = old.GoogleETag
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return q.SaveOrgEvent(ctx, e, true)
}

// expandOrgEvent expands weekly masters into local wall-time instances, preserving DST.
func expandOrgEvent(e store.OrgEvent, from, to string) []store.OrgEvent {
	overlaps := func(v store.OrgEvent) bool {
		a, b := "", ""
		if v.StartsAt != nil {
			a = *v.StartsAt
			b = a
			if v.EndsAt != nil {
				b = *v.EndsAt
			}
		} else if v.TentativeFrom != nil {
			a = *v.TentativeFrom
			b = *v.TentativeTo
		}
		if len(a) > 10 {
			a = a[:10]
		}
		if len(b) > 10 {
			b = b[:10]
		}
		return a < to && b >= from
	}
	if e.RRule == nil || e.StartsAt == nil || e.EndsAt == nil {
		if overlaps(e) {
			return []store.OrgEvent{e}
		}
		return nil
	}
	start, err := time.Parse(time.RFC3339Nano, *e.StartsAt)
	if err != nil {
		return nil
	}
	end, err := time.Parse(time.RFC3339Nano, *e.EndsAt)
	if err != nil {
		return nil
	}
	duration := end.Sub(start)
	start = start.In(orgTZ())
	until := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, part := range strings.Split(*e.RRule, ";") {
		if strings.HasPrefix(part, "UNTIL=") {
			if u, err := time.Parse("20060102T150405Z", strings.TrimPrefix(part, "UNTIL=")); err == nil {
				until = u
			}
		}
	}
	out := []store.OrgEvent{}
	for n := 0; n < 26000 && start.Format("2006-01-02") < to && !start.After(until); n++ {
		v := e
		v.StartsAt = strPtr(start.Format(time.RFC3339))
		v.EndsAt = strPtr(start.Add(duration).Format(time.RFC3339))
		if overlaps(v) {
			out = append(out, v)
		}
		start = start.AddDate(0, 0, 7)
	}
	return out
}
