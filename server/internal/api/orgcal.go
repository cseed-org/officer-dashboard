package api

import (
	"encoding/json"
	"strconv"
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
		// Every occurrence below is a copy of e, so all carry the master's times.
		e = withSeries(e)
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
		if err := validateOrgRecurrence(*e.RRule, e); err != nil {
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
	for _, k := range []string{"calendarKey", "title", "description", "location", "startsAt", "endsAt", "allDay", "tentativeFrom", "tentativeTo", "rrule", "reminders", "sendInvites", "invitees", "moveSeriesStart"} {
		allowed[k] = true
	}
	for k := range fields {
		if !allowed[k] {
			return badRequest("Unknown field: " + k)
		}
	}
	moveSeries := false
	if raw := fields["moveSeriesStart"]; raw != nil {
		if create || json.Unmarshal(raw, &moveSeries) != nil {
			return badRequest("moveSeriesStart is a boolean for PATCH only")
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
		// Unmarshal writes through existing pointers, so the audit snapshot needs its own copies.
		before := *e
		before.StartsAt, before.EndsAt, before.RRule = clonePtr(e.StartsAt), clonePtr(e.EndsAt), clonePtr(e.RRule)
		before.TentativeFrom, before.TentativeTo = clonePtr(e.TentativeFrom), clonePtr(e.TentativeTo)
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
		if !create && !moveSeries && before.RRule != nil && e.RRule != nil && before.StartsAt != nil && (fields["startsAt"] != nil || fields["endsAt"] != nil) {
			keepSeriesDate(e, before)
		}
		if create && fields["sendInvites"] == nil {
			e.SendInvites = e.CalendarKey == "external"
		}
		if !create && oldKey != e.CalendarKey {
			return badRequest("A meeting cannot change calendars; create it in the other calendar")
		}
		checkRule := create || (fields["rrule"] != nil && e.RRule != nil && *e.RRule != oldRule)
		// A moved start must still fall on a manual rule's BYDAY; pulled rules outside the subset are kept.
		if !checkRule && fields["startsAt"] != nil && e.RRule != nil && validateOrgRecurrence(*e.RRule, nil) == nil {
			checkRule = true
		}
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
		saved := withSeries(*e)
		out = &saved
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

// validateOrgRecurrence accepts the manual-meeting subset that Google and the local
// expander agree on: FREQ=DAILY|WEEKLY|MONTHLY, INTERVAL=1..52, BYDAY (weekly only) and
// at most one of UNTIL / COUNT=1..500. Monthly repeats on the start's day of month.
func validateOrgRecurrence(rule string, e *store.OrgEvent) error {
	const help = "Supported recurrence: RRULE:FREQ=DAILY|WEEKLY|MONTHLY with optional INTERVAL=1..52, BYDAY (weekly only, e.g. MO,WE) and one of UNTIL or COUNT=1..500"
	if len(rule) > 500 || !strings.HasPrefix(rule, "RRULE:") || strings.ContainsAny(rule, "\r\n") {
		return badRequest("Use a single RRULE line. " + help)
	}
	fields := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(rule, "RRULE:"), ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || kv[1] == "" {
			return badRequest("Invalid recurrence part \"" + part + "\". " + help)
		}
		if _, dup := fields[kv[0]]; dup {
			return badRequest("Recurrence repeats " + kv[0] + ". " + help)
		}
		fields[kv[0]] = kv[1]
	}
	for k, v := range fields {
		switch k {
		case "FREQ":
			if v != "DAILY" && v != "WEEKLY" && v != "MONTHLY" {
				return badRequest("Recurrence FREQ must be DAILY, WEEKLY or MONTHLY")
			}
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 52 || strconv.Itoa(n) != v {
				return badRequest("Recurrence INTERVAL must be 1 to 52")
			}
		case "COUNT":
			if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 500 || strconv.Itoa(n) != v {
				return badRequest("Recurrence COUNT must be 1 to 500")
			}
		case "UNTIL":
			if _, err := time.Parse("20060102T150405Z", v); err != nil {
				if _, err := time.Parse("20060102", v); err != nil || len(v) != 8 {
					return badRequest("Recurrence UNTIL must be YYYYMMDD or YYYYMMDDTHHMMSSZ")
				}
			}
		case "BYDAY":
			seen := map[string]bool{}
			for _, d := range strings.Split(v, ",") {
				if !strings.Contains(" SU MO TU WE TH FR SA ", " "+d+" ") || len(d) != 2 || seen[d] {
					return badRequest("Recurrence BYDAY must list distinct weekdays such as MO,WE")
				}
				seen[d] = true
			}
		default:
			return badRequest("Unsupported recurrence field " + k + ". " + help)
		}
	}
	freq := fields["FREQ"]
	if freq == "" {
		return badRequest("Recurrence FREQ is required. " + help)
	}
	if fields["UNTIL"] != "" && fields["COUNT"] != "" {
		return badRequest("Use UNTIL or COUNT, not both")
	}
	if days := fields["BYDAY"]; days != "" {
		if freq != "WEEKLY" {
			return badRequest("BYDAY is only supported with FREQ=WEEKLY")
		}
		// RFC 5545 always counts the start; Google does not. Require it to match the rule.
		if e != nil && e.StartsAt != nil {
			start, err := orgStartInZone(*e)
			if err == nil && !strings.Contains(days, [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}[start.Weekday()]) {
				return badRequest("The start date must fall on one of the BYDAY weekdays")
			}
		}
	}
	return nil
}

// orgStartInZone is the meeting start in its own timezone (America/Los_Angeles by default).
func orgStartInZone(e store.OrgEvent) (time.Time, error) {
	loc := orgTZ()
	if e.TimeZone != "" {
		if z, err := time.LoadLocation(e.TimeZone); err == nil {
			loc = z
		}
	}
	if e.AllDay {
		return time.ParseInLocation("2006-01-02", *e.StartsAt, loc)
	}
	t, err := time.Parse(time.RFC3339Nano, *e.StartsAt)
	return t.In(loc), err
}

// withSeries fills the recurring master's DTSTART/DTEND for the response.
func withSeries(e store.OrgEvent) store.OrgEvent {
	e.SeriesStartsAt, e.SeriesEndsAt = nil, nil
	if e.RRule != nil && e.StartsAt != nil {
		e.SeriesStartsAt, e.SeriesEndsAt = clonePtr(e.StartsAt), clonePtr(e.EndsAt)
	}
	return e
}

func clonePtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// keepSeriesDate stops a PATCH sent from a later occurrence from re-anchoring the series
// (which would drop every earlier occurrence, in Google too). The new times of day and
// duration apply on the series' original date; moveSeriesStart:true skips this.
// Unparseable values are left for validateOrgEvent to reject.
func keepSeriesDate(e *store.OrgEvent, before store.OrgEvent) {
	if e.StartsAt == nil || e.EndsAt == nil {
		return
	}
	orig, err := orgStartInZone(before)
	if err != nil {
		return
	}
	y, m, d := orig.Date()
	loc := orig.Location()
	if e.AllDay {
		a, ea := time.Parse("2006-01-02", *e.StartsAt)
		b, eb := time.Parse("2006-01-02", *e.EndsAt)
		if ea != nil || eb != nil {
			return
		}
		start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		days := int(b.Sub(a).Hours() / 24)
		e.StartsAt, e.EndsAt = strPtr(start.Format("2006-01-02")), strPtr(start.AddDate(0, 0, days).Format("2006-01-02"))
		return
	}
	a, ea := time.Parse(time.RFC3339Nano, *e.StartsAt)
	b, eb := time.Parse(time.RFC3339Nano, *e.EndsAt)
	if ea != nil || eb != nil || before.AllDay {
		return
	}
	w := a.In(loc)
	start := time.Date(y, m, d, w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), loc)
	end := start.Add(b.Sub(a))
	if a.Equal(orig) && b.After(a.Add(24*time.Hour)) {
		// Start untouched but endsAt taken from a later occurrence: use its time on the series date.
		v := b.In(loc)
		end = time.Date(y, m, d, v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), loc)
		if !end.After(start) {
			end = end.AddDate(0, 0, 1)
		}
	}
	e.StartsAt, e.EndsAt = strPtr(start.UTC().Format(time.RFC3339Nano)), strPtr(end.UTC().Format(time.RFC3339Nano))
}
