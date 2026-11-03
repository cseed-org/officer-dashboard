package api

import (
	"errors"
	"net/url"
	"regexp"
	"time"

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
	b, err := c.S.db.Read().Buildspace(c.Context())
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("America/Los_Angeles")
	today := c.S.now().In(loc)
	day := (int(today.Weekday()) + 6) % 7
	b.WeekStart = today.AddDate(0, 0, -day).Format("2006-01-02")
	b.Today = today.Format("2006-01-02")
	writeJSON(c.W, 200, b)
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
		for _, s := range p.Links {
			u, e := url.Parse(s)
			if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				r.Issues.Add("links", "Use http or https links")
			}
		}
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
			found := false
			for _, m := range b.Mentors {
				found = found || m.ID == id
			}
			if !found || seen[id] {
				return badRequest("Select distinct mentor contacts")
			}
			seen[id] = true
		}
		var before any
		for _, old := range b.Projects {
			if old.ID == p.ID {
				before = old
			}
		}
		if err = q.SaveBuildspaceProject(c.Context(), p); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace.project.saved", "buildspace_project", p.ID, before, p)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"project": p})
	return nil
}
func (c *Ctx) buildspacePersonSave() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	p := store.BuildspacePerson{ID: c.R.PathValue("id")}
	if err := c.jsonBody(func(r *validate.Reader) {
		p.Name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 200}).V
		p.ProjectID = r.Str("projectId", validate.S{Req: true, Max: 64}).V
		p.JoinedOn = r.Str("joinedOn", validate.S{Req: true, Max: 10}).V
		m := r.Str("memberId", validate.S{Max: 64, Null: true}).V
		if m != "" {
			p.MemberID = &m
		}
		d := r.Str("discordId", validate.S{Max: 20, Null: true}).V
		if d != "" {
			p.DiscordID = &d
			if !regexp.MustCompile(`^\d{17,20}$`).MatchString(d) {
				r.Issues.Add("discordId", "Use a 17–20 digit Discord ID")
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
		found := false
		for _, v := range b.Projects {
			found = found || v.ID == p.ProjectID
		}
		if !found {
			return badRequest("Project not found")
		}
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
		return c.audit(c.Context(), q, "buildspace.person.saved", "buildspace_person", p.ID, before, p)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"person": p})
	return nil
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
	}
	if err := c.jsonBody(func(r *validate.Reader) {
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
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
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
		var project *store.BuildspaceProject
		for i := range b.Projects {
			if b.Projects[i].ID == a.ProjectID {
				project = &b.Projects[i]
			}
		}
		if project == nil {
			return badRequest("Project not found")
		}
		if a.PersonID != nil {
			found := false
			for _, p := range b.People {
				found = found || (p.ID == *a.PersonID && p.ProjectID == a.ProjectID)
			}
			if !found {
				return badRequest("Builder must belong to the project")
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
		return c.audit(c.Context(), q, "buildspace.activity.created", "buildspace_activity", a.ID, nil, a)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
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
				if err = q.ResolveBuildspaceActivity(c.Context(), a.ID); err != nil {
					return err
				}
				return c.audit(c.Context(), q, "buildspace.activity.resolved", "buildspace_activity", a.ID, a, map[string]string{"status": "resolved"})
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
