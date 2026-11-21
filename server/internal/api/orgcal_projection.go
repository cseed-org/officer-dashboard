package api

import (
	"context"
	"errors"
	"officerdashboard/internal/orgcal"
	"officerdashboard/internal/store"
	"strings"
	"time"
)

func orgTZ() *time.Location { loc, _ := time.LoadLocation("America/Los_Angeles"); return loc }
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

// expandOrgEvent returns visible instances, preserving the master's editing id.
func expandOrgEvent(e store.OrgEvent, from, to string) []store.OrgEvent {
	if e.StartsAt == nil {
		if e.TentativeFrom != nil && *e.TentativeFrom < to && *e.TentativeTo >= from {
			return []store.OrgEvent{e}
		}
		return nil
	}
	loc := orgTZ()
	if e.TimeZone != "" {
		if z, err := time.LoadLocation(e.TimeZone); err == nil {
			loc = z
		}
	}
	parse := func(s string) (time.Time, error) {
		if e.AllDay {
			return time.ParseInLocation("2006-01-02", s, loc)
		}
		return time.Parse(time.RFC3339Nano, s)
	}
	start, err := parse(*e.StartsAt)
	if err != nil {
		return nil
	}
	end, err := parse(*e.EndsAt)
	if err != nil {
		return nil
	}
	start = start.In(loc)
	end = end.In(loc)
	a, _ := time.ParseInLocation("2006-01-02", from, orgTZ())
	b, _ := time.ParseInLocation("2006-01-02", to, orgTZ())
	overlaps := func(s, t time.Time) bool { return s.Before(b) && t.After(a) }
	if e.RRule == nil {
		if overlaps(start, end) {
			return []store.OrgEvent{e}
		}
		return nil
	}
	// Include long sessions beginning before the requested range.
	first := a.Add(-end.Sub(start)).In(loc).Format("2006-01-02")
	dates := orgcal.Occurrences(start, *e.RRule, first, to)
	out := []store.OrgEvent{}
	for _, t := range dates {
		excluded := false
		for _, line := range e.Recurrence {
			if !strings.HasPrefix(line, "EXDATE") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			for _, d := range strings.Split(parts[1], ",") {
				if d == t.Format("20060102T150405") || d == t.UTC().Format("20060102T150405Z") || d == t.Format("20060102") {
					excluded = true
				}
			}
		}
		if excluded {
			continue
		}
		finish := t.Add(end.Sub(start))
		if e.AllDay {
			a := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
			b := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
			finish = t.AddDate(0, 0, int(b.Sub(a)/(24*time.Hour)))
		}
		if !overlaps(t, finish) {
			continue
		}
		v := e
		if e.AllDay {
			v.StartsAt = strPtr(t.Format("2006-01-02"))
			v.EndsAt = strPtr(finish.Format("2006-01-02"))
		} else {
			v.StartsAt = strPtr(t.Format(time.RFC3339))
			v.EndsAt = strPtr(finish.Format(time.RFC3339))
		}
		out = append(out, v)
	}
	return out
}
