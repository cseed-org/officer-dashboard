package api

import (
	"context"
	"errors"
	"net/url"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/orgcal"
	"officerdashboard/internal/store"
	"strings"
	"time"
)

// StartOrgCalendar drains on startup, polls Google every five minutes, checks roster
// changes each minute, and stops with the server context. Only one sync runs at a time.
func (s *Server) StartOrgCalendar(ctx context.Context) {
	s.orgRunMu.Lock()
	s.orgCtx = ctx
	s.orgRunMu.Unlock()
	s.kickOrgCalendar()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.kickOrgCalendar()
			}
		}
	}()
}

func (s *Server) resumeOrgCalendar() {
	if s.cfg.Google == nil {
		return
	}
	_ = s.db.Write(context.Background(), func(q *store.Queries) error { return q.RetryOrgJobs(context.Background()) })
	s.kickOrgCalendar()
}
func (s *Server) kickOrgCalendar() {
	if s.cfg.Google == nil {
		return
	}
	s.orgRunMu.Lock()
	s.orgKicked = true
	if s.orgRunning {
		s.orgRunMu.Unlock()
		return
	}
	ctx := s.orgCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		s.orgRunMu.Unlock()
		return
	}
	s.orgRunning = true
	s.bg.Add(1)
	s.orgRunMu.Unlock()
	go func() {
		defer s.bg.Done()
		for {
			s.orgRunMu.Lock()
			s.orgKicked = false
			s.orgRunMu.Unlock()
			pass, cancel := context.WithTimeout(ctx, 4*time.Minute)
			s.orgRunMu.Lock()
			s.orgPassCancel = cancel
			s.orgRunMu.Unlock()
			err := s.syncOrgCalendar(pass)
			cancel()
			s.orgRunMu.Lock()
			s.orgPassCancel = nil
			s.orgRunMu.Unlock()
			if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, context.Canceled) {
				s.log.Warn("org calendar sync failed; retained for retry", "error", err)
			}
			s.orgRunMu.Lock()
			if !s.orgKicked || ctx.Err() != nil {
				s.orgRunning = false
				s.orgRunMu.Unlock()
				return
			}
			s.orgRunMu.Unlock()
		}
	}()
}
func (s *Server) syncOrgCalendar(ctx context.Context) error {
	if !s.orgMu.TryLock() {
		return nil
	}
	defer s.orgMu.Unlock()
	if s.now().Before(s.orgRetryAt) {
		return nil
	}
	err := s.syncOrgLocked(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.orgFailures++
		s.orgRetryAt = s.now().Add(calendarBackoff(s.orgFailures))
	} else {
		s.orgFailures = 0
		s.orgRetryAt = time.Time{}
	}
	if ctx.Err() == nil { // a cancelled pass (account change, shutdown) says nothing about Google
		s.recordOrgSync(err)
	}
	return err
}

// recordOrgSync keeps the last pass outcome for GET /api/settings/org-google/status.
func (s *Server) recordOrgSync(err error) {
	s.orgRunMu.Lock()
	defer s.orgRunMu.Unlock()
	if err == nil || errors.Is(err, store.ErrNotFound) {
		s.orgLastError, s.orgNeedsReauth = "", false
		return
	}
	s.orgLastError = err.Error()
	s.orgNeedsReauth = errors.Is(err, calendar.ErrGoogleRevoked)
}

// cancelOrgPass stops a running sync pass so connect/disconnect need not wait for it.
func (s *Server) cancelOrgPass() {
	s.orgRunMu.Lock()
	defer s.orgRunMu.Unlock()
	if s.orgPassCancel != nil {
		s.orgPassCancel()
	}
}
func (s *Server) syncOrgLocked(ctx context.Context) error {
	token, err := s.orgAccess(ctx)
	if err != nil {
		return err
	}
	client := orgcal.Client{OAuth: s.orgGoogle()}
	cals, err := s.db.Read().OrgCalendars(ctx)
	if err != nil {
		return err
	}
	for i := range cals {
		c := &cals[i]
		if c.GoogleCalendarID != nil {
			continue
		}
		var out struct {
			ID string `json:"id"`
		}
		if err = client.Request(ctx, token, "POST", "/calendars", map[string]any{"summary": c.Name, "timeZone": "America/Los_Angeles"}, &out); err != nil {
			return err
		}
		if out.ID == "" {
			return errors.New("Google calendar bootstrap returned no ID")
		}
		if err = s.db.Write(ctx, func(q *store.Queries) error { return q.SetOrgCalendarID(ctx, c.Key, out.ID) }); err != nil {
			return err
		}
		c.GoogleCalendarID = &out.ID
	}
	generation, err := s.db.Read().OrgACLGeneration(ctx)
	if err != nil {
		return err
	}
	// ACL sync is best effort with its own backoff: a roster address Google rejects
	// must never stop meeting, Event and program delivery or the pull.
	if generation != s.orgACLGeneration || !s.now().Before(s.orgNextACL) {
		s.orgACLGeneration = generation
		if err := s.orgSyncACL(ctx, client, token, cals); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.orgACLFailures++
			s.orgNextACL = s.now().Add(calendarBackoff(s.orgACLFailures))
			s.log.Warn("org calendar ACL sync incomplete; retrying with backoff", "error", err, "failures", s.orgACLFailures)
		} else {
			s.orgACLFailures = 0
			s.orgNextACL = s.now().Add(24 * time.Hour)
		}
	}
	if err = s.orgDrain(ctx, client, token, cals); err != nil {
		return err
	}
	if !s.now().Before(s.orgNextPull) {
		for _, c := range cals {
			if err = s.orgPull(ctx, client, token, c); err != nil {
				return err
			}
		}
		s.orgNextPull = s.now().Add(5 * time.Minute)
		return s.orgDrain(ctx, client, token, cals)
	}
	return nil
}
func (s *Server) orgSyncACL(ctx context.Context, client orgcal.Client, token string, cals []store.OrgCalendar) error {
	users, err := s.db.Read().Users(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, u := range users {
		if u.Active && u.Email != nil && strings.Contains(*u.Email, "@") {
			want[strings.ToLower(strings.TrimSpace(*u.Email))] = true
		}
	}
	// Each failing entry is logged with its address and skipped; the rest still apply.
	var failed []error
	skip := func(c store.OrgCalendar, email, action string, err error) {
		s.log.Warn("org calendar ACL entry failed", "calendar", c.Key, "email", email, "action", action, "error", err)
		failed = append(failed, err)
	}
	for _, c := range cals {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		acl, err := client.ACLs(ctx, token, *c.GoogleCalendarID)
		if err != nil {
			skip(c, "", "list", err)
			continue
		}
		left := map[string]bool{}
		for e := range want {
			left[e] = true
		}
		base := orgcal.CalendarPath(*c.GoogleCalendarID) + "/acl"
		for _, a := range acl {
			email := strings.ToLower(a.Scope.Value)
			if a.Role == "owner" {
				delete(left, email)
				continue
			}
			if a.Scope.Type != "user" {
				continue
			}
			if want[email] {
				delete(left, email)
				if a.Role != "reader" {
					if err = client.Request(ctx, token, "PATCH", base+"/"+url.PathEscape(a.ID), map[string]string{"role": "reader"}, nil); err != nil {
						skip(c, email, "set reader", err)
					}
				}
			} else {
				if err = client.Request(ctx, token, "DELETE", base+"/"+url.PathEscape(a.ID), nil, nil); err != nil && !orgcal.IsStatus(err, 404) {
					skip(c, email, "remove", err)
				}
			}
		}
		for email := range left {
			body := map[string]any{"role": "reader", "scope": map[string]string{"type": "user", "value": email}}
			if err = client.Request(ctx, token, "POST", base+"?sendNotifications=false", body, nil); err != nil {
				skip(c, email, "add reader", err)
			}
		}
	}
	return errors.Join(failed...)
}
func (s *Server) orgDrain(ctx context.Context, client orgcal.Client, token string, cals []store.OrgCalendar) error {
	ids := map[string]string{}
	for _, c := range cals {
		ids[c.Key] = *c.GoogleCalendarID
	}
	var failures []error
	for {
		jobs, err := s.db.Read().OrgCalendarJobs(ctx, s.nowMs())
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			break
		}
		for _, job := range jobs {
			e, err := s.db.Read().OrgEvent(ctx, job.EventID)
			if err != nil {
				return err
			}
			if e.Revision != job.Revision {
				continue
			}
			id := calendar.EventID(e.ID, ids[e.CalendarKey])
			existing := ""
			if e.GoogleEventID != nil {
				existing = *e.GoogleEventID
			}
			etag := ""
			if e.DeletedAt != nil {
				if existing == "" {
					existing = id
				}
				id = existing
				err = client.Delete(ctx, token, ids[e.CalendarKey], existing, e.SendInvites)
			} else {
				body, berr := s.orgGoogleEvent(ctx, *e)
				err = berr
				if err == nil {
					var out orgcal.Event
					out, err = client.Upsert(ctx, token, ids[e.CalendarKey], existing, id, body, e.SendInvites)
					id = out.ID
					etag = out.ETag
					if err == nil && e.SourceKind == "program" {
						err = s.orgPushExceptions(ctx, client, token, ids[e.CalendarKey], id, *e)
					}
				}
			}
			if err == nil {
				err = s.db.Write(ctx, func(q *store.Queries) error { return q.FinishOrgJob(ctx, job, id, etag) })
			} else {
				sendErr := err
				err = s.db.Write(ctx, func(q *store.Queries) error {
					return q.FailOrgJob(ctx, job, s.now().Add(calendarBackoff(job.Attempts+1)).UnixMilli(), sendErr.Error())
				})
				failures = append(failures, sendErr)
			}
			if err != nil {
				return err
			}
		}
	}
	return errors.Join(failures...)
}
func (s *Server) orgGoogleEvent(ctx context.Context, e store.OrgEvent) (orgcal.Event, error) {
	zone := e.TimeZone
	if zone == "" {
		zone = "America/Los_Angeles"
	}
	body := orgcal.Event{Summary: e.Title, Description: e.Description, Location: e.Location, Status: "confirmed", Attendees: []orgcal.Attendee{}, Recurrence: []string{}, Reminders: orgcal.Reminders{Overrides: []orgcal.Reminder{}}}
	if e.StartsAt == nil {
		body.Summary = "[Tentative] " + body.Summary
		body.Start = orgcal.Time{Date: *e.TentativeFrom}
		d, _ := time.Parse("2006-01-02", *e.TentativeTo)
		body.End = orgcal.Time{Date: d.AddDate(0, 0, 1).Format("2006-01-02")}
	} else if e.AllDay {
		body.Start = orgcal.Time{Date: *e.StartsAt}
		body.End = orgcal.Time{Date: *e.EndsAt}
	} else {
		body.Start = orgcal.Time{DateTime: *e.StartsAt, TimeZone: zone}
		body.End = orgcal.Time{DateTime: *e.EndsAt, TimeZone: zone}
	}
	if e.RRule != nil {
		body.Recurrence = append(body.Recurrence, *e.RRule)
	}
	if len(e.Recurrence) > 0 {
		body.Recurrence = append([]string{}, e.Recurrence...)
		for i, rule := range body.Recurrence {
			if strings.HasPrefix(rule, "RRULE:") && e.RRule != nil {
				body.Recurrence[i] = *e.RRule
			}
		}
	}
	if e.SourceKind == "program" && e.SourceID != nil {
		p, err := s.db.Read().Program(ctx, *e.SourceID)
		if err != nil {
			return body, err
		}
		for _, ex := range p.Exceptions {
			if ex.Kind == "skip" {
				t, _ := time.ParseInLocation("2006-01-02 15:04", ex.OnDate+" "+p.StartTime, orgTZ())
				body.Recurrence = append(body.Recurrence, "EXDATE;TZID=America/Los_Angeles:"+t.Format("20060102T150405"))
			}
		}
	}
	emails, err := s.db.Read().OrgInviteeEmails(ctx, e.ID)
	if err != nil {
		return body, err
	}
	for _, email := range emails {
		// Carry the last pulled RSVP so a PATCH (which replaces attendees) keeps responses.
		body.Attendees = append(body.Attendees, orgcal.Attendee{Email: email, ResponseStatus: e.AttendeeStatus[email]})
	}
	for _, r := range e.Reminders {
		body.Reminders.Overrides = append(body.Reminders.Overrides, orgcal.Reminder{Method: r.Method, Minutes: r.Minutes})
	}
	return body, nil
}

// Move a recurrence instance using its original start; reset a move removed locally.
func (s *Server) orgPushExceptions(ctx context.Context, client orgcal.Client, token, calID, master string, e store.OrgEvent) error {
	p, err := s.db.Read().Program(ctx, *e.SourceID)
	if err != nil {
		return err
	}
	base := orgcal.CalendarPath(calID) + "/events/" + url.PathEscape(master)
	page := ""
	for {
		// Program masters are finite (at most five years). Enumerate all instances:
		// actual-date bounds would hide sessions moved outside the original window.
		v := url.Values{"maxResults": {"2500"}, "showDeleted": {"true"}}
		if page != "" {
			v.Set("pageToken", page)
		}
		var out orgcal.EventPage
		if err = client.Request(ctx, token, "GET", base+"/instances?"+v.Encode(), nil, &out); err != nil {
			return err
		}
		for _, instance := range out.Items {
			if instance.OriginalStartTime == nil || instance.OriginalStartTime.DateTime == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, instance.OriginalStartTime.DateTime)
			if err != nil {
				return err
			}
			date := t.In(orgTZ()).Format("2006-01-02")
			var ex *store.ProgramException
			for i := range p.Exceptions {
				if p.Exceptions[i].OnDate == date {
					ex = &p.Exceptions[i]
					break
				}
			}
			if ex != nil && ex.Kind == "skip" {
				continue
			}
			start := t
			end := t.Add(time.Duration(p.DurationMin) * time.Minute)
			loc := p.Location
			if ex != nil {
				start, _ = time.Parse(time.RFC3339Nano, *ex.NewStartsAt)
				end, _ = time.Parse(time.RFC3339Nano, *ex.NewEndsAt)
				if ex.NewLocation != nil {
					loc = *ex.NewLocation
				}
			}
			if sameInstant(instance.Start.DateTime, start) && sameInstant(instance.End.DateTime, end) && instance.Location == loc && instance.Status != "cancelled" && instance.Summary == p.Name && instance.Description == p.Description && len(instance.Attendees) == 0 && len(instance.Reminders.Overrides) == 0 && !instance.Reminders.UseDefault {
				continue
			}
			patch := map[string]any{"status": "confirmed", "summary": p.Name, "description": p.Description, "attendees": []orgcal.Attendee{}, "reminders": orgcal.Reminders{Overrides: []orgcal.Reminder{}}, "start": orgcal.Time{DateTime: start.Format(time.RFC3339), TimeZone: "America/Los_Angeles"}, "end": orgcal.Time{DateTime: end.Format(time.RFC3339), TimeZone: "America/Los_Angeles"}, "location": loc}
			if err = client.Request(ctx, token, "PATCH", orgcal.CalendarPath(calID)+"/events/"+url.PathEscape(instance.ID)+"?sendUpdates=none", patch, nil); err != nil {
				return err
			}
		}
		if out.NextPageToken == "" {
			return nil
		}
		page = out.NextPageToken
	}
}

// sameInstant compares a Google dateTime with t as instants, whatever offset form either uses.
func sameInstant(raw string, t time.Time) bool {
	v, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && v.Equal(t)
}
