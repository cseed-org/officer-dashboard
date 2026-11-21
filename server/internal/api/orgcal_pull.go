package api

import (
	"context"
	"encoding/json"
	"errors"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/orgcal"
	"officerdashboard/internal/store"
	"strings"
	"time"
)

func (s *Server) orgPull(ctx context.Context, client orgcal.Client, token string, cal store.OrgCalendar) error {
	syncToken, err := s.db.Read().OrgSyncToken(ctx, cal.Key)
	if err != nil {
		return err
	}
	remote, next, err := client.Events(ctx, token, *cal.GoogleCalendarID, syncToken)
	if orgcal.IsStatus(err, 410) {
		syncToken = ""
		remote, next, err = client.Events(ctx, token, *cal.GoogleCalendarID, "")
	}
	if err != nil {
		return err
	}
	if cal.Locked {
		return s.orgPullLocked(ctx, client, token, cal, remote, next, syncToken == "")
	}
	return s.db.Write(ctx, func(q *store.Queries) error {
		if syncToken == "" {
			if err := q.ClearOrgOverrides(ctx, cal.Key); err != nil {
				return err
			}
		}
		seen := []string{}
		for _, g := range remote {
			seen = append(seen, g.ID)
			if g.RecurringEventID != "" && g.OriginalStartTime != nil {
				raw, _ := json.Marshal(g)
				original := g.OriginalStartTime.DateTime
				if original == "" {
					original = g.OriginalStartTime.Date
				}
				if err := q.SaveOrgOverride(ctx, cal.Key, store.OrgRemoteOverride{ID: g.ID, MasterID: g.RecurringEventID, OriginalStart: original, Payload: string(raw)}); err != nil {
					return err
				}
				continue
			}
			e, err := q.OrgEventByGoogle(ctx, cal.Key, g.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if err == nil {
				pending, err := q.OrgEventPending(ctx, e.ID)
				if err != nil {
					return err
				}
				if pending {
					continue
				}
				if e.GoogleETag != nil && *e.GoogleETag == g.ETag && g.Status != "cancelled" {
					continue
				}
			}
			if g.Status == "cancelled" {
				if e != nil && err == nil {
					now := s.nowMs()
					e.DeletedAt = &now
					e.UpdatedAt = now
					e.GoogleETag = &g.ETag
					if err = q.SaveOrgEvent(ctx, *e, false); err != nil {
						return err
					}
				}
				continue
			}
			if e == nil || err != nil {
				e = &store.OrgEvent{ID: auth.UUID(), CalendarKey: cal.Key, SourceKind: "manual", CreatedAt: s.nowMs(), SendInvites: cal.Key == "external"}
			}
			if g.RecurringEventID != "" {
				continue
			} // persisted separately as occurrence overrides below
			e.Title = g.Summary
			e.Description = g.Description
			e.Location = g.Location
			e.DeletedAt = nil
			e.UpdatedAt = s.nowMs()
			e.GoogleEventID = &g.ID
			e.GoogleETag = &g.ETag
			e.StartsAt = nil
			e.EndsAt = nil
			e.TentativeFrom = nil
			e.TentativeTo = nil
			e.AllDay = g.Start.Date != ""
			if e.AllDay {
				e.StartsAt = &g.Start.Date
				e.EndsAt = &g.End.Date
			} else {
				e.StartsAt = &g.Start.DateTime
				e.EndsAt = &g.End.DateTime
			}
			if *e.StartsAt == "" || *e.EndsAt == "" {
				continue
			}
			if e.AllDay && strings.HasPrefix(e.Title, "[Tentative] ") {
				e.Title = strings.TrimPrefix(e.Title, "[Tentative] ")
				e.TentativeFrom = e.StartsAt
				end, err := time.Parse("2006-01-02", *e.EndsAt)
				if err != nil {
					return err
				}
				last := end.AddDate(0, 0, -1).Format("2006-01-02")
				e.TentativeTo = &last
				e.StartsAt = nil
				e.EndsAt = nil
			}
			e.RRule = nil
			e.Recurrence = g.Recurrence
			e.TimeZone = g.Start.TimeZone
			if e.TimeZone == "" {
				e.TimeZone = "America/Los_Angeles"
			}
			for _, rule := range g.Recurrence {
				if strings.HasPrefix(rule, "RRULE:") {
					e.RRule = &rule
					break
				}
			}
			e.Reminders = []store.OrgReminder{}
			for _, r := range g.Reminders.Overrides {
				e.Reminders = append(e.Reminders, store.OrgReminder{Method: r.Method, Minutes: r.Minutes})
			}
			e.Invitees = []store.OrgInvitee{}
			for _, a := range g.Attendees {
				e.Invitees = append(e.Invitees, store.OrgInvitee{Kind: "email", Ref: a.Email})
			}
			if err = q.SaveOrgEvent(ctx, *e, false); err != nil {
				return err
			}
		}
		if syncToken == "" {
			if err = q.OrgFullResync(ctx, cal.Key, seen, s.nowMs()); err != nil {
				return err
			}
		}
		return q.SetOrgSyncToken(ctx, cal.Key, next)
	})
}

// Locked calendars never import Google edits. Etag changes and missing/cancelled
// events requeue the authoritative dashboard projection; extra remote events are removed.
func (s *Server) orgPullLocked(ctx context.Context, client orgcal.Client, token string, cal store.OrgCalendar, remote []orgcal.Event, next string, full bool) error {
	seen := map[string]bool{}
	for _, g := range remote {
		seen[g.ID] = true
		if g.RecurringEventID != "" {
			master, err := s.db.Read().OrgEventByGoogle(ctx, cal.Key, g.RecurringEventID)
			if err == nil {
				if err = s.db.Write(ctx, func(q *store.Queries) error { return q.EnqueueOrgEvent(ctx, master.ID) }); err != nil {
					return err
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			continue
		}
		e, err := s.db.Read().OrgEventByGoogle(ctx, cal.Key, g.ID)
		if errors.Is(err, store.ErrNotFound) {
			if g.Status != "cancelled" {
				if err = client.Delete(ctx, token, *cal.GoogleCalendarID, g.ID, false); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return err
		}
		if e.DeletedAt != nil {
			if g.Status != "cancelled" {
				if err = client.Delete(ctx, token, *cal.GoogleCalendarID, g.ID, false); err != nil {
					return err
				}
			}
			continue
		}
		if g.Status == "cancelled" || e.GoogleETag == nil || *e.GoogleETag != g.ETag {
			if err = s.db.Write(ctx, func(q *store.Queries) error { return q.EnqueueOrgEvent(ctx, e.ID) }); err != nil {
				return err
			}
		}
	}
	return s.db.Write(ctx, func(q *store.Queries) error {
		if full {
			all, err := q.OrgEvents(ctx)
			if err != nil {
				return err
			}
			for _, e := range all {
				if e.CalendarKey == cal.Key && e.GoogleEventID != nil && !seen[*e.GoogleEventID] {
					if err = q.EnqueueOrgEvent(ctx, e.ID); err != nil {
						return err
					}
				}
			}
		}
		return q.SetOrgSyncToken(ctx, cal.Key, next)
	})
}
