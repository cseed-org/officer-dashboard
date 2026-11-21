package api

import (
	"encoding/json"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/orgcal"
	"officerdashboard/internal/store"
)

// Calendar service tokens are deliberately confined to these endpoints.
func (c *Ctx) orgOfficer(write bool) error {
	if c.User != nil {
		_, err := c.officer()
		return err
	}
	scope := "calendar:read"
	if write {
		scope = "calendar:write"
	}
	tok, err := c.authenticateToken(scope)
	if err != nil && !write {
		tok, err = c.authenticateToken("calendar:write")
	}
	if err != nil {
		return err
	}
	c.User, err = c.S.db.Read().UserByID(c.Context(), tok.CreatedBy)
	if err != nil {
		return err
	}
	if pending, err := c.S.onboardingPending(c.Context(), c.User.ID); err != nil {
		return err
	} else if pending {
		return fail(403, "onboarding_required", "Finish setting up your account first")
	}
	return nil
}
func (c *Ctx) orgCalendars() error {
	if err := c.orgOfficer(false); err != nil {
		return err
	}
	out, err := c.S.db.Read().OrgCalendars(c.Context())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"calendars": out, "timezone": "America/Los_Angeles"})
	return nil
}
func orgDate(s string) bool    { _, err := time.Parse("2006-01-02", s); return err == nil }
func orgInstant(s string) bool { _, err := time.Parse(time.RFC3339Nano, s); return err == nil }
func (c *Ctx) orgEvents() error {
	if err := c.orgOfficer(false); err != nil {
		return err
	}
	v := c.R.URL.Query()
	from, to := v.Get("from"), v.Get("to")
	if !orgDate(from) || !orgDate(to) || from >= to {
		return badRequest("from and to must be dates in increasing order (to is exclusive)")
	}
	a, _ := time.Parse("2006-01-02", from)
	b, _ := time.Parse("2006-01-02", to)
	if b.Sub(a) > 366*24*time.Hour {
		return badRequest("Calendar range must be at most 366 days")
	}
	keys := map[string]bool{}
	if raw := v.Get("cal"); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if k != "internal" && k != "external" && k != "programs" && k != "events" {
				return badRequest("Unknown calendar")
			}
			keys[k] = true
		}
	}
	all, err := c.S.db.Read().OrgEvents(c.Context())
	if err != nil {
		return err
	}
	out := []store.OrgEvent{}
	for _, e := range all {
		if len(keys) > 0 && !keys[e.CalendarKey] {
			continue
		}
		if e.SourceKind == "program" && e.SourceID != nil {
			p, err := c.S.db.Read().Program(c.Context(), *e.SourceID)
			if err != nil {
				return err
			}
			for _, v := range expandOrgEvent(e, from, to) {
				date := (*v.StartsAt)[:10]
				skip := false
				for _, ex := range p.Exceptions {
					if ex.OnDate == date {
						skip = true
						break
					}
				}
				if !skip {
					out = append(out, v)
				}
			}
			for _, ex := range p.Exceptions {
				if ex.Kind != "move" {
					continue
				}
				v := e
				v.RRule = nil
				v.StartsAt = ex.NewStartsAt
				v.EndsAt = ex.NewEndsAt
				if ex.NewLocation != nil {
					v.Location = *ex.NewLocation
				}
				out = append(out, expandOrgEvent(v, from, to)...)
			}
		} else {
			items := expandOrgEvent(e, from, to)
			if e.GoogleEventID != nil && e.RRule != nil {
				overrides, err := c.S.db.Read().OrgOverrides(c.Context(), e.CalendarKey, *e.GoogleEventID)
				if err != nil {
					return err
				}
				for _, o := range overrides {
					var g orgcal.Event
					if err = json.Unmarshal([]byte(o.Payload), &g); err != nil {
						return err
					}
					keep := items[:0]
					for _, v := range items {
						matches := *v.StartsAt == o.OriginalStart
						if !matches && !e.AllDay {
							a, ea := time.Parse(time.RFC3339Nano, *v.StartsAt)
							b, eb := time.Parse(time.RFC3339Nano, o.OriginalStart)
							matches = ea == nil && eb == nil && a.Equal(b)
						}
						if !matches {
							keep = append(keep, v)
						}
					}
					items = keep
					if g.Status != "cancelled" {
						v := e
						v.RRule = nil
						v.Title = g.Summary
						v.Location = g.Location
						v.Description = g.Description
						if g.Start.Date != "" {
							v.AllDay = true
							v.StartsAt = &g.Start.Date
							v.EndsAt = &g.End.Date
						} else {
							v.AllDay = false
							v.StartsAt = &g.Start.DateTime
							v.EndsAt = &g.End.DateTime
						}
						items = append(items, expandOrgEvent(v, from, to)...)
					}
				}
			}
			out = append(out, items...)
		}
	}
	writeJSON(c.W, 200, map[string]any{"events": out, "timezone": "America/Los_Angeles"})
	return nil
}

// mergeOrgJSON preserves omitted PATCH fields while rejecting malformed JSON types.
func (c *Ctx) mergeOrgJSON(dst any) error {
	raw, err := readBody(c, 1<<20)
	if err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return badRequest("Expected a JSON object")
	}
	if err = json.Unmarshal(raw, dst); err != nil {
		return badRequest("Invalid field type")
	}
	return nil
}
func validateOrgEvent(e *store.OrgEvent, checkRecurrence bool) error {
	if e.CalendarKey != "internal" && e.CalendarKey != "external" {
		return forbidden("Weekly Programs and Events calendars are locked; edit the source program or event")
	}
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" || len(e.Title) > 200 || len(e.Description) > 10000 || len(e.Location) > 500 {
		return badRequest("Title is required (at most 200 characters); description/location too long")
	}
	if e.CalendarKey != "internal" && e.CalendarKey != "external" {
		return forbidden("Weekly Programs and Events calendars are locked; edit the source program or event")
	}
	if e.StartsAt != nil {
		if e.EndsAt == nil {
			return badRequest("endsAt is required")
		}
		layout := orgInstant
		if e.AllDay {
			layout = orgDate
		}
		if !layout(*e.StartsAt) || !layout(*e.EndsAt) {
			return badRequest("Invalid start/end")
		}
		if e.AllDay {
			if *e.EndsAt <= *e.StartsAt {
				return badRequest("End must follow start")
			}
		} else {
			a, _ := time.Parse(time.RFC3339Nano, *e.StartsAt)
			b, _ := time.Parse(time.RFC3339Nano, *e.EndsAt)
			if !b.After(a) {
				return badRequest("End must follow start")
			}
		}
		e.TentativeFrom = nil
		e.TentativeTo = nil
	} else {
		if e.TentativeFrom == nil || e.TentativeTo == nil || !orgDate(*e.TentativeFrom) || !orgDate(*e.TentativeTo) || *e.TentativeTo < *e.TentativeFrom {
			return badRequest("Set a start/end or a tentativeFrom/tentativeTo date range (inclusive)")
		}
		e.EndsAt = nil
		e.AllDay = true
	}
	if e.RRule != nil && checkRecurrence {
		if err := validateOrgRecurrence(*e.RRule); err != nil {
			return err
		}
	}
	if len(e.Invitees) > 200 || len(e.Reminders) > 5 {
		return badRequest("Too many invitees or reminders")
	}
	for _, v := range e.Invitees {
		if v.Ref == "" || len(v.Ref) > 254 || (v.Kind != "email" && v.Kind != "user" && v.Kind != "subteam") {
			return badRequest("Invalid invitee")
		}
		if v.Kind == "email" && !strings.Contains(v.Ref, "@") {
			return badRequest("Invalid invitee email")
		}
	}
	for _, v := range e.Reminders {
		if (v.Method != "email" && v.Method != "popup") || v.Minutes < 0 || v.Minutes > 40320 {
			return badRequest("Invalid reminder")
		}
	}
	return nil
}
func (c *Ctx) orgEventWrite() error {
	if err := c.orgOfficer(true); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	create := id == ""
	if create {
		id = auth.UUID()
	}
	// Decode to raw fields first: clients cannot alter source or Google metadata.
	raw, err := readBody(c, 1<<20)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return badRequest("Expected JSON object")
	}
	allowed := map[string]bool{}
	for _, k := range []string{"calendarKey", "title", "description", "location", "startsAt", "endsAt", "allDay", "tentativeFrom", "tentativeTo", "rrule", "reminders", "sendInvites", "invitees"} {
		allowed[k] = true
	}
	for k := range fields {
		if !allowed[k] {
			return badRequest("Unknown field: " + k)
		}
	}
	var out *store.OrgEvent
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		e := &store.OrgEvent{ID: id, SourceKind: "manual", CreatedBy: &c.User.ID, UpdatedBy: &c.User.ID, CreatedAt: c.S.nowMs(), Reminders: []store.OrgReminder{}, Invitees: []store.OrgInvitee{}}
		if !create {
			var err error
			e, err = q.OrgEvent(ctx, id)
			if err != nil {
				return err
			}
			if e.DeletedAt != nil {
				return store.ErrNotFound
			}
			if e.SourceKind != "manual" {
				return forbidden("This calendar is locked; edit the source event or program")
			}
		}
		before := *e
		oldKey := e.CalendarKey
		oldRule := ""
		if e.RRule != nil {
			oldRule = *e.RRule
		}
		if err := json.Unmarshal(raw, e); err != nil {
			return badRequest("Invalid field type")
		}
		if fields["rrule"] != nil {
			e.Recurrence = nil
		}
		if fields["startsAt"] != nil && fields["allDay"] == nil && e.StartsAt != nil {
			e.AllDay = orgDate(*e.StartsAt)
		}
		if create && fields["sendInvites"] == nil {
			e.SendInvites = e.CalendarKey == "external"
		}
		if !create && oldKey != e.CalendarKey {
			return badRequest("A meeting cannot change calendars; create it in the other calendar")
		}
		checkRule := create || (fields["rrule"] != nil && e.RRule != nil && *e.RRule != oldRule)
		if err := validateOrgEvent(e, checkRule); err != nil {
			return err
		}
		if e.Invitees == nil {
			e.Invitees = []store.OrgInvitee{}
		}
		if e.Reminders == nil {
			e.Reminders = []store.OrgReminder{}
		}
		e.UpdatedBy = &c.User.ID
		e.UpdatedAt = c.S.nowMs()
		if c.Token != nil {
			if err := q.TouchToken(ctx, c.Token.ID, c.S.nowMs()); err != nil {
				return err
			}
		}
		if err := q.SaveOrgEvent(ctx, *e, true); err != nil {
			return err
		}
		if err := c.audit(ctx, q, "orgcal.saved", "org_cal_event", id, before, e); err != nil {
			return err
		}
		out = e
		return nil
	})
	if err != nil {
		return err
	}
	c.S.kickOrgCalendar()
	writeJSON(c.W, 200, map[string]any{"event": out})
	return nil
}
func (c *Ctx) orgEventDelete() error {
	if err := c.orgOfficer(true); err != nil {
		return err
	}
	id := c.R.PathValue("id")
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		e, err := q.OrgEvent(c.Context(), id)
		if err != nil {
			return err
		}
		if e.SourceKind != "manual" || (e.CalendarKey != "internal" && e.CalendarKey != "external") {
			return forbidden("This calendar is locked; edit the source event or program")
		}
		if e.DeletedAt != nil {
			return store.ErrNotFound
		}
		now := c.S.nowMs()
		e.DeletedAt = &now
		e.UpdatedAt = now
		e.UpdatedBy = &c.User.ID
		if err = q.SaveOrgEvent(c.Context(), *e, true); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "orgcal.deleted", "org_cal_event", id, e, nil)
	})
	if err != nil {
		return err
	}
	c.S.kickOrgCalendar()
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

func validateOrgRecurrence(rule string) error {
	if len(rule) > 500 || !strings.HasPrefix(rule, "RRULE:") || strings.ContainsAny(rule, "\r\n") {
		return badRequest("Use a single RRULE recurrence")
	}
	// Weekly recurrence is shared with programs and expanded locally for the calendar UI.
	for _, part := range strings.Split(strings.TrimPrefix(rule, "RRULE:"), ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return badRequest("Invalid recurrence")
		}
		switch kv[0] {
		case "FREQ":
			if kv[1] != "WEEKLY" {
				return badRequest("Only WEEKLY recurrence is supported")
			}
		case "UNTIL":
			if _, err := time.Parse("20060102T150405Z", kv[1]); err != nil {
				return badRequest("Invalid recurrence UNTIL")
			}
		default:
			return badRequest("Supported recurrence fields: FREQ and UNTIL")
		}
	}
	if !strings.Contains(rule, "FREQ=WEEKLY") {
		return badRequest("Weekly recurrence required")
	}
	return nil
}
