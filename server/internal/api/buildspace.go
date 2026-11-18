package api

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

var buildspaceTracks = []string{"Software", "Hardware", "Creative", "Extended Virtual Reality (XR)"}
var buildspaceStages = []string{"Ideating", "Building", "Demo-ready", "Shipped", "At risk"}

func (c *Ctx) buildspaceList() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	qs := c.R.URL.Query()
	cohortID, archived := qs.Get("cohort"), qs.Get("archived")
	if len(cohortID) > 64 || (archived != "" && archived != "0" && archived != "1") {
		return badRequest("Invalid query")
	}
	q := c.S.db.Read()
	sched, err := c.buildspaceSchedule(q)
	if err != nil {
		return err
	}
	activityCursor, err := c.activityCursor()
	if err != nil {
		return err
	}
	b, err := q.BuildspaceCohortPage(c.Context(), cohortID, store.BuildspaceActivityPage{WeekStart: sched.Week.Start, WeekDue: sched.Week.Due, Cursor: activityCursor, FilterArchived: archived != "1"})
	if err != nil {
		return err
	}
	b.Schedule = sched
	b.WeekStart, b.WeekDue, b.Today = b.Schedule.Week.Start, b.Schedule.Week.Due, b.Schedule.Today
	out, err := launchpadView(b, cohortID, archived == "1", c.S.nowMs())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, out)
	return nil
}

// launchpadView narrows the whole register to one cohort (default: newest active) and, unless
// archived is set, to rows that are not archived. Mentors are a club-wide roster.
func launchpadView(b store.Buildspace, cohortID string, archived bool, now int64) (store.Buildspace, error) {
	b.MarkExpired(now)
	var cohort *store.LaunchpadCohort
	if cohortID != "" {
		if cohort = b.Cohort(cohortID); cohort == nil {
			return b, notFoundErr("Cohort not found")
		}
	} else {
		cohort = b.DefaultCohort()
	}
	out := b
	out.Projects, out.People, out.Activity, out.Accounts = []store.BuildspaceProject{}, []store.BuildspacePerson{}, []store.BuildspaceActivity{}, []store.LaunchpadAccount{}
	if cohort == nil {
		return out, nil
	}
	id := cohort.ID
	out.CohortID = &id
	projects, people := map[string]bool{}, map[string]bool{}
	for _, p := range b.Projects {
		if p.CohortID == id && (archived || p.ArchivedAt == nil) {
			out.Projects = append(out.Projects, p)
			projects[p.ID] = true
		}
	}
	for _, p := range b.People {
		if p.CohortID == id && (archived || p.ArchivedAt == nil) {
			out.People = append(out.People, p)
			people[p.ID] = true
		}
	}
	for _, a := range b.Activity {
		if projects[a.ProjectID] && (a.PersonID == nil || people[*a.PersonID]) {
			out.Activity = append(out.Activity, a)
		}
	}
	for _, a := range b.Accounts {
		if a.Kind == "builder" && a.CohortID != nil && *a.CohortID == id {
			out.Accounts = append(out.Accounts, a)
		}
	}
	return out, nil
}

// writableProject refuses writes to an archived project or a project in an archived cohort.
func writableProject(b *store.Buildspace, p *store.BuildspaceProject) error {
	if p.ArchivedAt != nil {
		return conflict("This project is archived; restore it first")
	}
	if c := b.Cohort(p.CohortID); c == nil || c.Status != "active" {
		return conflict("This cohort is archived")
	}
	return nil
}

// buildspaceSchedule reads the due-day rules and the club-time-zone clock together.
func (c *Ctx) buildspaceSchedule(q *store.Queries) (store.BuildspaceSchedule, error) {
	rules, err := q.BuildspaceWeekRules(c.Context())
	if err != nil {
		return store.BuildspaceSchedule{}, err
	}
	clock, err := q.BuildspaceClock(c.Context(), c.S.now())
	if err != nil {
		return store.BuildspaceSchedule{}, err
	}
	return store.BuildspaceScheduleAt(rules, clock), nil
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// buildspaceSchedulePut changes the weekly check-in due day. Leadership only, audited.
func (c *Ctx) buildspaceSchedulePut() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	var weekday int
	var dueTime *string
	if err := c.jsonBody(func(r *validate.Reader) {
		weekday = int(r.Int("dueWeekday", validate.I{Req: true, Min: validate.Lo(0), Max: validate.Lo(6)}).V)
		t := r.Str("dueTime", validate.S{Max: 5, Null: true}).V
		if t != "" {
			dueTime = &t
			if !hhmm.MatchString(t) {
				r.Issues.Add("dueTime", "Use a 24-hour HH:MM time")
			}
		}
	}); err != nil {
		return err
	}
	var out store.BuildspaceSchedule
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		u, err := c.requireLeadershipIn(q)
		if err != nil {
			return err
		}
		before, err := c.buildspaceSchedule(q)
		if err != nil {
			return err
		}
		clock, err := q.BuildspaceClock(c.Context(), c.S.now())
		if err != nil {
			return err
		}
		changed, err := q.SetBuildspaceDue(c.Context(), clock, weekday, dueTime, u.ID, c.S.nowMs())
		if err != nil {
			return err
		}
		if out, err = c.buildspaceSchedule(q); err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.schedule.updated", "buildspace_launchpad_schedule", "weekly", before, out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, out)
	return nil
}
func (c *Ctx) buildspaceProjectSave() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	p := store.BuildspaceProject{ID: c.R.PathValue("id"), UpdatedAt: c.S.nowMs()}
	if err := c.jsonBody(func(r *validate.Reader) {
		p.Name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 200}).V
		p.Description = r.Str("description", validate.S{Max: 10000}).V
		p.Track = r.Str("track", validate.S{Req: true, Enum: buildspaceTracks}).V
		p.Stage = r.Str("stage", validate.S{Req: true, Enum: buildspaceStages}).V
		p.Links = r.Strings("links", true, 0, 10, nil, 2000)
		p.MentorIDs = r.Strings("mentorIds", true, 0, 2, nil, 64)
		p.CohortID = r.Str("cohortId", validate.S{Min: 1, Max: 64}).V
		r.Issues = checkLinks(r.Issues, p.Links)
	}); err != nil {
		return err
	}
	if len(p.ID) > 64 || p.ID == "" {
		return badRequest("Invalid project ID")
	}
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range p.MentorIDs {
			if b.Mentor(id) == nil || seen[id] {
				return badRequest("Select distinct mentor contacts")
			}
			seen[id] = true
			// The mentor list also carries contacts already assigned somewhere (so a retyped
			// contact still shows), but a new assignment needs a mentor or both contact.
			kept := false
			for _, old := range b.Projects {
				if old.ID == p.ID {
					for _, m := range old.MentorIDs {
						kept = kept || m == id
					}
				}
			}
			if !kept {
				ct, err := q.ContactByID(c.Context(), id)
				if err != nil || (ct.Type != "mentor" && ct.Type != "both") {
					return badRequest("Mentors must be External Contacts of type mentor or both")
				}
			}
		}
		var before any
		if old := b.Project(p.ID); old != nil {
			// A project's cohort never changes; cohortId is ignored on update.
			if err := writableProject(&b, old); err != nil {
				return err
			}
			before = *old
			p.CohortID = old.CohortID
		} else {
			// Creating: the named cohort, or the newest active one for older clients.
			var cohort *store.LaunchpadCohort
			if p.CohortID != "" {
				cohort = b.Cohort(p.CohortID)
			} else if cohort = b.DefaultCohort(); cohort == nil {
				return badRequest("Create a cohort first")
			}
			if cohort == nil {
				return badRequest("Cohort not found")
			}
			if cohort.Status != "active" {
				return conflict("This cohort is archived")
			}
			p.CohortID = cohort.ID
		}
		if err = q.SaveBuildspaceProject(c.Context(), p); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.project.saved", "buildspace_launchpad_project", p.ID, before, p)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"project": p})
	return nil
}

// checkLinks requires http(s) URLs with a host.
func checkLinks(issues *validate.Issues, links []string) *validate.Issues {
	for _, s := range links {
		u, e := url.Parse(s)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			issues.Add("links", "Use http or https links")
			break
		}
	}
	return issues
}

var discordIDRE = regexp.MustCompile(`^\d{17,20}$`)

func (c *Ctx) buildspacePersonSave() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	p := store.BuildspacePerson{ID: c.R.PathValue("id")}
	// Profile fields a builder fills in on the portal: kept unless the body sends them.
	var has struct{ designation, pronouns, bio, email bool }
	if err := c.jsonBody(func(r *validate.Reader) {
		p.Name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 200}).V
		p.ProjectID = r.Str("projectId", validate.S{Req: true, Max: 64}).V
		p.JoinedOn = r.Str("joinedOn", validate.S{Req: true, Max: 10}).V
		f := r.Str("designation", validate.S{Trim: true, Max: 80})
		p.Designation, has.designation = f.V, f.Set
		f = r.Str("pronouns", validate.S{Trim: true, Max: 40})
		p.Pronouns, has.pronouns = f.V, f.Set
		f = r.Str("bio", validate.S{Trim: true, Max: 500})
		p.Bio, has.bio = f.V, f.Set
		has.email = r.Has("email")
		m := r.Str("memberId", validate.S{Max: 64, Null: true}).V
		if m != "" {
			p.MemberID = &m
		}
		d := r.Str("discordId", validate.S{Max: 20, Null: true}).V
		if d != "" {
			p.DiscordID = &d
			if !discordIDRE.MatchString(d) {
				r.Issues.Add("discordId", "Use a 17–20 digit Discord ID")
			}
		}
		if em := strings.ToLower(r.Str("email", validate.S{Trim: true, Max: 254, Null: true}).V); em != "" {
			p.Email = &em
			if !validate.IsEmail(em) {
				r.Issues.Add("email", "Invalid email")
			}
		}
	}); err != nil {
		return err
	}
	if !validate.IsDate(p.JoinedOn) {
		return badRequest("Invalid join date")
	}
	if p.ID == "" || len(p.ID) > 64 {
		return badRequest("Invalid builder ID")
	}
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		project := b.Project(p.ProjectID)
		if project == nil {
			return badRequest("Project not found")
		}
		if err := writableProject(&b, project); err != nil {
			return err
		}
		p.CohortID = project.CohortID
		if p.MemberID != nil {
			if _, err = q.Member(c.Context(), *p.MemberID); errors.Is(err, store.ErrNotFound) {
				return badRequest("Member not found")
			} else if err != nil {
				return err
			}
		}
		var before any
		for _, old := range b.People {
			if old.ID == p.ID {
				before = old
				if !has.designation {
					p.Designation = old.Designation
				}
				if !has.pronouns {
					p.Pronouns = old.Pronouns
				}
				if !has.bio {
					p.Bio = old.Bio
				}
				if !has.email {
					p.Email = old.Email
				}
				if old.ArchivedAt != nil {
					return conflict("This builder is archived; restore them first")
				}
				if old.CohortID != p.CohortID {
					return conflict("Builders cannot move between cohorts")
				}
				if old.ProjectID != p.ProjectID {
					for _, a := range b.Activity {
						if a.PersonID != nil && *a.PersonID == p.ID {
							return conflict("Builder has activity; moving projects needs a history migration")
						}
					}
				}
			} else if p.DiscordID != nil && old.DiscordID != nil && *p.DiscordID == *old.DiscordID {
				return conflict("Discord ID already belongs to a builder")
			}
		}
		if err = q.SaveBuildspacePerson(c.Context(), p); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.person.saved", "buildspace_launchpad_person", p.ID, before, p)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"person": c.reloadPerson(p)})
	return nil
}

// reloadPerson returns the saved builder as the register shows it (account fields included).
func (c *Ctx) reloadPerson(p store.BuildspacePerson) store.BuildspacePerson {
	if b, err := c.S.db.Read().Buildspace(c.Context()); err == nil {
		if v := b.Person(p.ID); v != nil {
			return *v
		}
	}
	return p
}
func (c *Ctx) buildspaceActivityCreate() error { return c.buildspaceActivitySave(false) }
func (c *Ctx) buildspaceIngest() error         { return c.buildspaceActivitySave(true) }
func (c *Ctx) buildspaceActivitySave(ingest bool) error {
	if ingest {
		if _, err := c.authenticateToken("ingest:buildspace"); err != nil {
			return err
		}
	} else if _, err := c.officer(); err != nil {
		return err
	}
	a := store.BuildspaceActivity{ID: auth.UUID(), CreatedAt: c.S.nowMs(), Source: "manual"}
	var discordID string
	kinds := []string{"weekly", "mentor", "request", "purchase", "absence"}
	if ingest {
		kinds = []string{"weekly"}
		a.Source = "discord"
		a.Visibility = "team"
	}
	if err := c.jsonBody(func(r *validate.Reader) {
		if !ingest {
			a.Visibility = r.Str("visibility", validate.S{Enum: []string{"team", "mentors", "officers"}, HasDef: true, Default: "officers"}).V
		}
		a.ProjectID = r.Str("projectId", validate.S{Req: !ingest, Max: 64}).V
		a.Kind = r.Str("kind", validate.S{Req: true, Enum: kinds}).V
		a.Date = r.Str("date", validate.S{Req: true, Max: 10}).V
		a.Body = r.Str("body", validate.S{Req: true, Trim: true, Min: 1, Max: 10000}).V
		a.Status = r.Str("status", validate.S{Req: true, Enum: []string{"open", "resolved", "on-track", "needs-help"}}).V
		for _, f := range []struct {
			k string
			p **string
		}{{"personId", &a.PersonID}, {"mentorId", &a.MentorID}, {"purchaseId", &a.PurchaseID}} {
			v := r.Str(f.k, validate.S{Max: 64, Null: true}).V
			if v != "" {
				*f.p = &v
			}
		}
		if ingest {
			v := r.Str("externalId", validate.S{Req: true, Trim: true, Min: 1, Max: 100}).V
			a.ExternalID = &v
			discordID = r.Str("discordId", validate.S{Req: true, Max: 20}).V
		}
	}); err != nil {
		return err
	}
	if !validate.IsDate(a.Date) {
		return badRequest("Invalid activity date")
	}
	// Every entry belongs to the check-in week containing its date under the due-day rules
	// (club time zone). The response tells the caller (and the Discord bot) which week.
	var week store.BuildspaceWeek
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		rules, err := q.BuildspaceWeekRules(c.Context())
		if err != nil {
			return err
		}
		week = store.BuildspaceWeekOf(rules, a.Date)
		if ingest {
			a.PersonID, a.MentorID, a.PurchaseID = nil, nil, nil
			for _, p := range b.People {
				if p.DiscordID != nil && *p.DiscordID == discordID {
					a.PersonID = &p.ID
					a.ProjectID = p.ProjectID
					break
				}
			}
			if a.PersonID == nil {
				return badRequest("Discord user is not linked to a builder")
			}
			for _, old := range b.Activity {
				if old.ExternalID != nil && *old.ExternalID == *a.ExternalID {
					if old.PersonID == nil || *old.PersonID != *a.PersonID || old.Body != a.Body || old.Date != a.Date || old.Status != a.Status {
						return conflict("externalId already used for a different check-in")
					}
					a = old
					return nil
				}
			}
		}
		project := b.Project(a.ProjectID)
		if project == nil {
			return badRequest("Project not found")
		}
		if ingest && !b.ProjectLive(project) {
			return badRequest("Builder is archived")
		}
		if err := writableProject(&b, project); err != nil {
			return err
		}
		if a.PersonID != nil {
			p := b.Person(*a.PersonID)
			if p == nil || p.ProjectID != a.ProjectID {
				return badRequest("Builder must belong to the project")
			}
			if p.ArchivedAt != nil {
				if ingest {
					return badRequest("Builder is archived")
				}
				return conflict("This builder is archived")
			}
		}
		if a.Kind == "weekly" && a.PersonID == nil {
			return badRequest("Weekly check-ins require a builder")
		}
		if a.Kind == "mentor" && a.MentorID == nil {
			return badRequest("Mentor check-ins require a mentor")
		}
		if a.MentorID != nil {
			found := false
			for _, id := range project.MentorIDs {
				found = found || id == *a.MentorID
			}
			if !found {
				return badRequest("Mentor must be assigned to the project")
			}
		}
		if a.PurchaseID != nil {
			if a.Kind != "purchase" {
				return badRequest("Finance links require purchase activity")
			}
			if _, err = q.RequestByID(c.Context(), *a.PurchaseID); errors.Is(err, store.ErrNotFound) {
				return badRequest("Purchase request not found")
			} else if err != nil {
				return err
			}
		}
		if err = q.InsertBuildspaceActivity(c.Context(), a); err != nil {
			return err
		}
		if c.Token != nil {
			if err = q.TouchToken(c.Context(), c.Token.ID, c.S.nowMs()); err != nil {
				return err
			}
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.activity.created", "buildspace_launchpad_activity", a.ID, nil, a)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true, "week": week})
	return nil
}
func (c *Ctx) buildspaceActivityResolve() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		for _, a := range b.Activity {
			if a.ID == c.R.PathValue("id") {
				if a.Kind != "request" && a.Kind != "purchase" {
					return badRequest("Only requests can be resolved")
				}
				if a.PurchaseID != nil {
					return badRequest("Resolve linked purchases in Finance")
				}
				if a.Status != "open" {
					return conflict("Only open requests can be resolved")
				}
				if err := writableProject(&b, b.Project(a.ProjectID)); err != nil {
					return err
				}
				if err = q.ResolveBuildspaceActivity(c.Context(), a.ID); err != nil {
					return err
				}
				return c.audit(c.Context(), q, "buildspace_launchpad.activity.resolved", "buildspace_launchpad_activity", a.ID, a, map[string]string{"status": "resolved"})
			}
		}
		return notFoundErr("Activity not found")
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}
