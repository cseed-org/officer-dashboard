package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// The builder/mentor portal (docs/launchpad-cohorts.md). Portal accounts are never users:
// they have their own session table and cookie, and only the /api/portal handlers here read
// that cookie. Officer APIs resolve users rows only, so a portal session can never reach them.

const (
	portalCookieBase   = "cseed_launchpad"
	lpInviteCookieName = "cseed_lp_invite"
	lpLoginCookieName  = "cseed_lp_login"
)

func (s *Server) portalCookieName() string { return s.hostPrefixed(portalCookieBase) }

// createPortalSession stores sha256(token) and sets the portal cookie, replacing the browser's
// previous portal session (if any).
func (s *Server) createPortalSession(ctx context.Context, w http.ResponseWriter, r *http.Request, accountID string) error {
	token := auth.RandomToken(32)
	expires := s.now().Add(min(sessionTTL, sessionMaxAge))
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	var replaces string
	if ck, err := r.Cookie(s.portalCookieName()); err == nil && ck.Value != "" {
		replaces = auth.SHA256Hex(ck.Value)
	}
	err := s.db.Write(ctx, func(q *store.Queries) error {
		if replaces != "" {
			if err := q.DeleteLaunchpadSession(ctx, replaces); err != nil {
				return err
			}
		}
		if err := q.InsertLaunchpadSession(ctx, auth.SHA256Hex(token), accountID, expires.UnixMilli(), ua, s.nowMs()); err != nil {
			return err
		}
		return q.TouchLaunchpadLogin(ctx, accountID, s.nowMs())
	})
	if err != nil {
		return err
	}
	s.setCookie(w, s.portalCookieName(), token, expires, 0)
	return nil
}

type portalSession struct {
	id      string
	account store.LaunchpadAccount
}

// portal authenticates the portal cookie. kind is "builder", "mentor" or "" (either). It
// slides the expiry like officer sessions, refuses cross-site writes, and refuses builders
// whose record was archived (their sessions were already ended; this covers races).
func (c *Ctx) portal(kind string) (*portalSession, error) {
	ck, err := c.R.Cookie(c.S.portalCookieName())
	if err != nil || ck.Value == "" {
		return nil, fail(401, "unauthenticated", "Sign in to Launchpad first")
	}
	id := auth.SHA256Hex(ck.Value)
	ctx := c.Context()
	sess, err := c.S.db.Read().LaunchpadSessionByHash(ctx, id, c.S.nowMs())
	hardStop := time.Time{}
	if err == nil {
		hardStop = time.UnixMilli(sess.CreatedAt).Add(sessionMaxAge)
		if !c.S.now().Before(hardStop) {
			_ = c.S.db.Write(ctx, func(q *store.Queries) error { return q.DeleteLaunchpadSession(ctx, id) })
			err = store.ErrNotFound
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		c.S.clearCookie(c.W, c.S.portalCookieName())
		return nil, fail(401, "unauthenticated", "Sign in to Launchpad first")
	} else if err != nil {
		return nil, err
	}
	if !c.S.originOK(c.R) {
		return nil, forbidden("Cross-origin request blocked")
	}
	a := sess.Account
	if kind != "" && a.Kind != kind {
		return nil, forbidden("This page is for " + kind + "s")
	}
	if a.Kind == "builder" && a.PersonID != nil {
		archived, err := c.S.db.Read().LaunchpadPersonArchived(ctx, *a.PersonID)
		if err != nil {
			return nil, err
		}
		if archived {
			return nil, fail(403, "archived", "Your Launchpad access is paused. Contact an officer.")
		}
	}
	if time.UnixMilli(sess.ExpiresAt).Sub(c.S.now()) < renewWhenLeft {
		exp := c.S.now().Add(sessionTTL)
		if exp.After(hardStop) {
			exp = hardStop
		}
		if exp.UnixMilli() > sess.ExpiresAt {
			if werr := c.S.db.Write(ctx, func(q *store.Queries) error { return q.ExtendLaunchpadSession(ctx, id, exp.UnixMilli()) }); werr == nil {
				c.S.setCookie(c.W, c.S.portalCookieName(), ck.Value, exp, 0)
			}
		}
	}
	return &portalSession{id: id, account: a}, nil
}

// portalIn revalidates the session in a mutation's write transaction. Authentication may
// have happened before a slow body arrived; a logout, disable or expiry since then must win.
// Replacing the account also makes all person/contact linkage and scope current.
func (c *Ctx) portalIn(ctx context.Context, q *store.Queries, s *portalSession, kind string) error {
	sess, err := q.LaunchpadSessionByHash(ctx, s.id, c.S.nowMs())
	if errors.Is(err, store.ErrNotFound) {
		return fail(401, "unauthenticated", "Sign in to Launchpad first")
	}
	if err != nil {
		return err
	}
	if kind != "" && sess.Account.Kind != kind {
		return forbidden("This page is for " + kind + "s")
	}
	s.account = sess.Account
	return nil
}

// portalAudit records a portal write. The actor is the account (in the after JSON), never an
// officer, even when the same browser also holds an officer session.
func (c *Ctx) portalAudit(ctx context.Context, q *store.Queries, accountID, action, entityType, entityID string, before, after any) error {
	a := store.AuditEntry{ID: auth.UUID(), Action: action, EntityType: entityType, BeforeJSON: snapshot(before),
		AfterJSON: snapshot(map[string]any{"accountId": accountID, "value": after}), CreatedAt: store.Ms(c.S.nowMs())}
	if entityID != "" {
		a.EntityID = &entityID
	}
	return q.InsertAudit(ctx, a)
}

// ── sign-in ──

// portalRedirect sends the browser to a portal page after Discord sign-in.
func (c *Ctx) portalRedirect(target string) error {
	http.Redirect(c.W, c.R, target, http.StatusFound)
	return nil
}

// portalAccept binds an invite to the Discord account that just signed in, then starts a
// portal session. Failures land on the join page with a reason.
func (c *Ctx) portalAccept(discordID, tokenHash string) error {
	ctx := c.Context()
	var accountID, reason string
	signIn := false
	now := c.S.nowMs()
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		acc, err := q.LaunchpadAccountByTokenHash(ctx, tokenHash)
		if errors.Is(err, store.ErrNotFound) {
			reason = "invalid"
			return nil
		} else if err != nil {
			return err
		}
		r, err := inviteRefusal(ctx, q, &acc, now)
		if err != nil {
			return err
		}
		if r != "" {
			if r == "used" && acc.DiscordID != nil && *acc.DiscordID == discordID {
				signIn = true // the same person opening their link again just signs in
				return nil
			}
			reason = r
			return nil
		}
		if other, err := q.LaunchpadAccountByDiscordID(ctx, discordID); err == nil {
			// A returning builder from an archived cohort moves on to the new cohort; any other
			// account already bound to this Discord ID keeps it.
			detach := other.Kind == "builder" && acc.Kind == "builder" && other.CohortID != nil
			if detach {
				co, err := q.LaunchpadCohort(ctx, *other.CohortID)
				if err != nil {
					return err
				}
				detach = co.Status == "archived"
			}
			if !detach {
				reason = "discord_taken"
				return nil
			}
			other.DiscordID, other.Status, other.UpdatedAt = nil, "disabled", now
			if err := q.SaveLaunchpadAccount(ctx, other); err != nil {
				return err
			}
			if err := q.DeleteLaunchpadSessionsFor(ctx, other.ID); err != nil {
				return err
			}
			if err := c.portalAudit(ctx, q, acc.ID, "buildspace_launchpad.account.detached", "buildspace_launchpad_account", other.ID, nil, map[string]any{"movedTo": acc.ID}); err != nil {
				return err
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		acc.DiscordID, acc.Status, acc.AcceptedAt, acc.UpdatedAt = &discordID, "active", &now, now
		if err := q.SaveLaunchpadAccount(ctx, acc); err != nil {
			return err
		}
		accountID = acc.ID
		return c.portalAudit(ctx, q, acc.ID, "buildspace_launchpad.account.accepted", "buildspace_launchpad_account", acc.ID, nil, map[string]any{"kind": acc.Kind})
	})
	if err != nil {
		c.S.log.Error("launchpad invite acceptance failed", "error", err)
		return c.portalRedirect("/launchpad/join?error=failed")
	}
	if reason != "" {
		return c.portalRedirect("/launchpad/join?error=" + reason)
	}
	if signIn {
		// Same checks as any returning sign-in (archived builder, disabled account).
		_, err := c.portalSignIn(discordID)
		return err
	}
	if err := c.S.createPortalSession(ctx, c.W, c.R, accountID); err != nil {
		c.S.log.Error("create portal session failed", "error", err)
		return c.portalRedirect("/launchpad/join?error=failed")
	}
	return c.portalRedirect("/launchpad")
}

// inviteRefusal says why an invite cannot be accepted ("" when it can).
func inviteRefusal(ctx context.Context, q *store.Queries, a *store.LaunchpadAccount, now int64) (string, error) {
	switch a.Status {
	case "active":
		return "used", nil
	case "disabled":
		return "disabled", nil
	}
	if a.InviteExpiresAt == nil || *a.InviteExpiresAt <= now {
		return "expired", nil
	}
	if a.CohortID != nil {
		co, err := q.LaunchpadCohort(ctx, *a.CohortID)
		if errors.Is(err, store.ErrNotFound) {
			return "disabled", nil
		} else if err != nil {
			return "", err // a real failure, not a refusal
		}
		if co.Status != "active" {
			return "disabled", nil
		}
	}
	return "", nil
}

// portalAccountFor finds the active, usable portal account of a Discord ID ("" + reason if none).
func (s *Server) portalAccountFor(ctx context.Context, discordID string) (string, string, error) {
	q := s.db.Read()
	a, err := q.LaunchpadAccountByDiscordID(ctx, discordID)
	if errors.Is(err, store.ErrNotFound) {
		return "", "not_found", nil
	} else if err != nil {
		return "", "", err
	}
	if a.Status != "active" {
		return "", "disabled", nil
	}
	if a.PersonID != nil {
		archived, err := q.LaunchpadPersonArchived(ctx, *a.PersonID)
		if err != nil {
			return "", "", err
		}
		if archived {
			return "", "archived", nil
		}
	}
	return a.ID, "", nil
}

// portalSignIn starts a portal session for an already-bound account.
func (c *Ctx) portalSignIn(discordID string) (bool, error) {
	id, reason, err := c.S.portalAccountFor(c.Context(), discordID)
	if err != nil {
		return false, err
	}
	if reason != "" {
		return false, c.portalRedirect("/launchpad/login?error=" + reason)
	}
	if err := c.S.createPortalSession(c.Context(), c.W, c.R, id); err != nil {
		return false, err
	}
	return true, c.portalRedirect("/launchpad")
}

// ── endpoints ──

// launchpadID is a short random ID for portal-created rows, so derived IDs (calendar items
// "buildspace-weekly-<person>-<date>") stay within the 64-character ID contract.
func launchpadID(prefix string) string { return prefix + auth.RandomToken(12) }

func maskEmail(e string) string {
	at := strings.LastIndex(e, "@")
	if at < 1 {
		return ""
	}
	return e[:1] + "•••" + e[at:]
}

func (c *Ctx) portalInvitePreview() error {
	token := c.R.URL.Query().Get("token")
	if token == "" || len(token) > 128 {
		return badRequest("Invalid invite link")
	}
	q := c.S.db.Read()
	out := map[string]any{"valid": false, "reason": "not_found", "kind": nil, "cohortName": nil, "emailHint": nil, "name": nil}
	a, err := q.LaunchpadAccountByTokenHash(c.Context(), auth.SHA256Hex(token))
	if err == nil { // the lookup is by the token's hash, so a hit is a match
		out["kind"], out["emailHint"], out["name"] = a.Kind, maskEmail(a.Email), a.Name
		if a.CohortID != nil {
			if co, err := q.LaunchpadCohort(c.Context(), *a.CohortID); err == nil {
				out["cohortName"] = co.Name
			}
		}
		r, rerr := inviteRefusal(c.Context(), q, &a, c.S.nowMs())
		if rerr != nil {
			return rerr
		}
		if r != "" {
			out["reason"] = r
		} else {
			out["valid"], out["reason"] = true, nil
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	writeJSON(c.W, 200, out)
	return nil
}

func (c *Ctx) portalMe() error {
	s, err := c.portal("")
	if err != nil {
		return err
	}
	a := s.account
	var cohort any
	if a.CohortID != nil {
		co, err := c.S.db.Read().LaunchpadCohort(c.Context(), *a.CohortID)
		if err != nil {
			return err
		}
		cohort = map[string]any{"id": co.ID, "name": co.Name, "status": co.Status}
	}
	writeJSON(c.W, 200, map[string]any{
		"account": map[string]any{"id": a.ID, "kind": a.Kind, "name": a.Name, "email": a.Email, "status": a.Status, "onboarded": a.Kind == "mentor" || a.OnboardedAt != nil},
		"cohort":  cohort, "officer": c.User != nil,
	})
	return nil
}

func (c *Ctx) portalLogout() error {
	s, err := c.portal("")
	if err != nil {
		return err
	}
	ctx := context.WithoutCancel(c.Context())
	if err := c.S.db.Write(ctx, func(q *store.Queries) error { return q.DeleteLaunchpadSession(ctx, s.id) }); err != nil {
		return err
	}
	c.S.clearCookie(c.W, c.S.portalCookieName())
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

// Portal DTOs (PortalPerson, PortalProject, PortalActivity, PortalMentorCard in the contracts).
type portalPerson struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Pronouns    string `json:"pronouns"`
	Designation string `json:"designation"`
	Bio         string `json:"bio"`
	JoinedOn    string `json:"joinedOn"`
}
type portalMentorCard struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Org    string   `json:"org"`
	Topics []string `json:"topics"`
}
type portalProject struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Track       string   `json:"track"`
	Stage       string   `json:"stage"`
	Links       []string `json:"links"`
	Archived    bool     `json:"archived"`
}
type portalActivity struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"projectId"`
	PersonID   *string `json:"personId"`
	MentorID   *string `json:"mentorId"`
	Kind       string  `json:"kind"`
	Date       string  `json:"date"`
	Body       string  `json:"body"`
	Status     string  `json:"status"`
	Visibility string  `json:"visibility"`
	AuthorName *string `json:"authorName"`
	CreatedAt  int64   `json:"createdAt"`
}

func toPortalPerson(p *store.BuildspacePerson) portalPerson {
	return portalPerson{p.ID, p.Name, p.Pronouns, p.Designation, p.Bio, p.JoinedOn}
}
func toPortalProject(p *store.BuildspaceProject) portalProject {
	links := p.Links
	if links == nil {
		links = []string{}
	}
	return portalProject{p.ID, p.Name, p.Description, p.Track, p.Stage, links, p.ArchivedAt != nil}
}
func mentorCard(b *store.Buildspace, id string) portalMentorCard {
	if m := b.Mentor(id); m != nil {
		topics := m.Topics
		if topics == nil {
			topics = []string{}
		}
		return portalMentorCard{m.ID, m.Name, m.Org, topics}
	}
	return portalMentorCard{ID: id, Topics: []string{}}
}

// visibleActivity lists a project's entries whose visibility is in allowed, without any
// Finance fields, with an author name when one is known.
func visibleActivity(b *store.Buildspace, projectID string, allowed ...string) []portalActivity {
	out := []portalActivity{}
	for _, a := range b.Activity {
		if a.ProjectID != projectID {
			continue
		}
		ok := false
		for _, v := range allowed {
			ok = ok || a.Visibility == v
		}
		if !ok {
			continue
		}
		author := a.AuthorName
		if author == nil {
			if a.Kind == "mentor" && a.MentorID != nil {
				if m := b.Mentor(*a.MentorID); m != nil {
					author = &m.Name
				}
			} else if a.PersonID != nil {
				if p := b.Person(*a.PersonID); p != nil {
					author = &p.Name
				}
			}
		}
		out = append(out, portalActivity{a.ID, a.ProjectID, a.PersonID, a.MentorID, a.Kind, a.Date, a.Body, a.Status, a.Visibility, author, a.CreatedAt})
	}
	return out
}

// weekStatus is a builder's check-in state for the current week.
func weekStatus(b *store.Buildspace, sched store.BuildspaceSchedule, p *store.BuildspacePerson) string {
	if p.JoinedOn > sched.Today {
		return "not-joined"
	}
	if b.WeeklyDone[p.ID] {
		return "done"
	}
	for _, a := range b.Activity {
		// Only entries dated through today count, like the officers' check-in signal; a
		// future-dated entry does not finish this week early.
		if a.Kind == "weekly" && a.PersonID != nil && *a.PersonID == p.ID && a.Date >= sched.Week.Start && a.Date <= sched.Week.Due && a.Date <= sched.Today {
			return "done"
		}
	}
	if sched.PastDue {
		return "past-due"
	}
	return "awaiting"
}

// builderContext loads everything a builder request needs, scoped to their account.
type builderContext struct {
	s       *portalSession
	b       store.Buildspace
	cohort  *store.LaunchpadCohort
	person  *store.BuildspacePerson
	project *store.BuildspaceProject
}

func loadBuilder(ctx context.Context, q *store.Queries, s *portalSession, preloaded ...*store.Buildspace) (*builderContext, error) {
	var b store.Buildspace
	if len(preloaded) > 0 {
		b = *preloaded[0]
	} else {
		var err error
		b, err = q.BuildspaceRoster(ctx)
		if err != nil {
			return nil, err
		}
	}
	bc := &builderContext{s: s, b: b}
	if s.account.CohortID == nil || bc.b.Cohort(*s.account.CohortID) == nil {
		return nil, notFoundErr("Cohort not found")
	}
	bc.cohort = bc.b.Cohort(*s.account.CohortID)
	if s.account.PersonID != nil {
		bc.person = bc.b.Person(*s.account.PersonID)
		if bc.person != nil {
			bc.project = bc.b.Project(bc.person.ProjectID)
		}
	}
	return bc, nil
}

// writable refuses builder writes when not onboarded or the cohort/project is archived.
func (bc *builderContext) writable() error {
	if bc.cohort.Status != "active" {
		return conflict("This cohort is archived; your dashboard is read-only")
	}
	if bc.person == nil || bc.project == nil {
		return conflict("Finish onboarding first")
	}
	if bc.person.ArchivedAt != nil {
		return fail(403, "archived", "Your Launchpad access is paused. Contact an officer.")
	}
	if bc.project.ArchivedAt != nil {
		return conflict("Your project is archived")
	}
	return nil
}

func (c *Ctx) portalBuilder() error {
	s, err := c.portal("builder")
	if err != nil {
		return err
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
	person := deref(s.account.PersonID)
	register, err := q.BuildspaceCohortPage(c.Context(), deref(s.account.CohortID), store.BuildspaceActivityPage{WeekStart: sched.Week.Start, WeekDue: sched.Week.Due, Cursor: activityCursor, PersonID: person, SkipActivity: s.account.PersonID == nil, Visibility: []string{"team"}})
	if err != nil {
		return err
	}
	bc, err := loadBuilder(c.Context(), q, s, &register)
	if err != nil {
		return err
	}
	b, co := &bc.b, bc.cohort
	out := map[string]any{
		"cohort":    map[string]any{"id": co.ID, "name": co.Name, "status": co.Status, "startsOn": co.StartsOn, "endsOn": co.EndsOn},
		"onboarded": bc.person != nil, "readOnly": co.Status != "active", "email": s.account.Email,
		"me": nil, "project": nil, "teammates": []portalPerson{}, "mentors": []portalMentorCard{}, "activity": []portalActivity{},
		"schedule": sched, "thisWeek": "not-joined", "joinable": []any{}, "activityNextCursor": b.ActivityNextCursor,
	}
	if bc.person == nil {
		type joinable struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Track       string   `json:"track"`
			Description string   `json:"description"`
			Builders    []string `json:"builders"`
		}
		list := []joinable{}
		for _, p := range b.Projects {
			if p.CohortID != co.ID || p.ArchivedAt != nil {
				continue
			}
			j := joinable{p.ID, p.Name, p.Track, p.Description, []string{}}
			for _, v := range b.People {
				if v.ProjectID == p.ID && v.ArchivedAt == nil {
					j.Builders = append(j.Builders, v.Name)
				}
			}
			list = append(list, j)
		}
		out["joinable"] = list
		writeJSON(c.W, 200, out)
		return nil
	}
	out["me"] = toPortalPerson(bc.person)
	out["thisWeek"] = weekStatus(b, sched, bc.person)
	if p := bc.project; p != nil {
		out["project"] = toPortalProject(p)
		mates := []portalPerson{}
		for i := range b.People {
			v := &b.People[i]
			if v.ProjectID == p.ID && v.ID != bc.person.ID && v.ArchivedAt == nil {
				mates = append(mates, toPortalPerson(v))
			}
		}
		out["teammates"] = mates
		mentors := []portalMentorCard{}
		for _, id := range p.MentorIDs {
			mentors = append(mentors, mentorCard(b, id))
		}
		out["mentors"] = mentors
		out["activity"] = visibleActivity(b, p.ID, "team")
	}
	writeJSON(c.W, 200, out)
	return nil
}

func readProfile(r *validate.Reader) (name, pronouns, designation, bio string) {
	name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 200}).V
	pronouns = r.Str("pronouns", validate.S{Req: true, Trim: true, Max: 40}).V
	designation = r.Str("designation", validate.S{Req: true, Trim: true, Max: 80}).V
	bio = r.Str("bio", validate.S{Req: true, Trim: true, Max: 500}).V
	return
}

type newProject struct {
	name, description, track string
	links                    []string
}

func readNewProject(r *validate.Reader) newProject {
	p := newProject{
		name:        r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 200}).V,
		description: r.Str("description", validate.S{Req: true, Max: 10000}).V,
		track:       r.Str("track", validate.S{Req: true, Enum: buildspaceTracks}).V,
		links:       r.Strings("links", true, 0, 10, nil, 2000),
	}
	checkLinks(r.Issues, p.links)
	if p.links == nil {
		p.links = []string{}
	}
	return p
}

func (c *Ctx) portalBuilderOnboarding() error {
	s, err := c.portal("builder")
	if err != nil {
		return err
	}
	var name, pronouns, designation, bio, mode, joinID string
	var np newProject
	if err := c.jsonBody(func(r *validate.Reader) {
		name, pronouns, designation, bio = readProfile(r)
		raw := r.Raw("project")
		if len(raw) == 0 || !json.Valid(raw) || raw[0] != '{' {
			r.Issues.Add("project", "Invalid input: expected object")
			return
		}
		sub := validate.FromJSON(raw)
		sub.Issues = r.Issues
		mode = sub.Str("mode", validate.S{Req: true, Enum: []string{"new", "join"}}).V
		switch mode {
		case "new":
			np = readNewProject(sub)
		case "join":
			joinID = sub.Str("projectId", validate.S{Req: true, Min: 1, Max: 64}).V
		}
	}); err != nil {
		return err
	}
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		if err := c.portalIn(ctx, q, s, "builder"); err != nil {
			return err
		}
		acc := s.account
		if acc.OnboardedAt != nil || acc.PersonID != nil {
			return conflict("You've already finished onboarding")
		}
		s.account = acc
		bc, err := loadBuilder(ctx, q, s)
		if err != nil {
			return err
		}
		if bc.cohort.Status != "active" {
			return conflict("This cohort is archived")
		}
		now := c.S.nowMs()
		var project store.BuildspaceProject
		created := mode == "new"
		if created {
			project = store.BuildspaceProject{ID: launchpadID("lp-p-"), Name: np.name, Description: np.description, Track: np.track, Stage: "Ideating",
				Links: np.links, MentorIDs: []string{}, UpdatedAt: now, CohortID: bc.cohort.ID}
			if err := q.SaveBuildspaceProject(ctx, project); err != nil {
				return err
			}
		} else {
			p := bc.b.Project(joinID)
			if p == nil || p.CohortID != bc.cohort.ID || p.ArchivedAt != nil {
				return badRequest("Project not found")
			}
			project = *p
		}
		clock, err := q.BuildspaceClock(ctx, c.S.now())
		if err != nil {
			return err
		}
		email := acc.Email
		person := store.BuildspacePerson{ID: launchpadID("lp-b-"), Name: name, ProjectID: project.ID, JoinedOn: clock.Today, CohortID: bc.cohort.ID,
			Designation: designation, Pronouns: pronouns, Bio: bio, Email: &email, DiscordID: acc.DiscordID}
		// A builder record may already carry this Discord ID (discord_id is unique):
		//   - from an archived cohort (a returning builder): that record lets go of it, as
		//     portalAccept already moved the account on;
		//   - added by an officer in this cohort with no portal account yet: link to it instead
		//     of creating a duplicate.
		// Anything else (another active cohort, an archived builder, a record that already has
		// an account) is left for an officer.
		var released []string
		linked := false
		for _, v := range bc.b.People {
			if acc.DiscordID == nil || v.DiscordID == nil || *v.DiscordID != *acc.DiscordID {
				continue
			}
			if co := bc.b.Cohort(v.CohortID); co != nil && co.Status == "archived" {
				old := v
				old.DiscordID = nil
				if err := q.SaveBuildspacePerson(ctx, old); err != nil {
					return err
				}
				released = append(released, v.ID)
				continue
			}
			if v.CohortID == bc.cohort.ID && v.ArchivedAt == nil {
				if _, err := q.LaunchpadAccountByPerson(ctx, v.ID); errors.Is(err, store.ErrNotFound) {
					// The same guard as an officer moving a builder: their updates belong to the
					// project they were written on, so linking to another project needs an officer.
					if v.ProjectID != project.ID {
						for _, a := range bc.b.Activity {
							if a.PersonID != nil && *a.PersonID == v.ID {
								name := "your current project"
								if old := bc.b.Project(v.ProjectID); old != nil {
									name = old.Name
								}
								return conflict("You already have updates on " + name + ". Choose that project, or ask an officer to move you.")
							}
						}
					}
					person.ID, person.JoinedOn, person.MemberID = v.ID, v.JoinedOn, v.MemberID
					linked = true
					continue
				} else if err != nil {
					return err
				}
			}
			return conflict("Your Discord account is already registered to a builder; ask an officer to link it")
		}
		// Link the member record when exactly one member has this UW email.
		if person.MemberID == nil {
			if ids, err := q.MemberIDsByUWEmail(ctx, acc.Email); err != nil {
				return err
			} else if len(ids) == 1 {
				person.MemberID = &ids[0]
			}
		}
		if err := q.SaveBuildspacePerson(ctx, person); err != nil {
			return err
		}
		acc.PersonID, acc.OnboardedAt, acc.Name, acc.UpdatedAt = &person.ID, &now, name, now
		if err := q.SaveLaunchpadAccount(ctx, acc); err != nil {
			return err
		}
		return c.portalAudit(ctx, q, acc.ID, "buildspace_launchpad.portal.onboarded", "buildspace_launchpad_person", person.ID, nil,
			map[string]any{"person": person, "projectId": project.ID, "createdProject": created, "linkedExisting": linked, "releasedDiscordFrom": released})
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

func (c *Ctx) portalBuilderProfile() error {
	s, err := c.portal("builder")
	if err != nil {
		return err
	}
	var name, pronouns, designation, bio string
	if err := c.jsonBody(func(r *validate.Reader) { name, pronouns, designation, bio = readProfile(r) }); err != nil {
		return err
	}
	var out portalPerson
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		if err := c.portalIn(ctx, q, s, "builder"); err != nil {
			return err
		}
		bc, err := loadBuilder(ctx, q, s)
		if err != nil {
			return err
		}
		if err := bc.writable(); err != nil {
			return err
		}
		before := *bc.person
		p := *bc.person
		p.Name, p.Pronouns, p.Designation, p.Bio = name, pronouns, designation, bio
		if err := q.SaveBuildspacePerson(ctx, p); err != nil {
			return err
		}
		// portalIn replaced the session's snapshot: SaveLaunchpadAccount writes the fresh row,
		// so it cannot undo a disable or relinking committed while the body was arriving.
		acc := s.account
		acc.Name, acc.UpdatedAt = name, c.S.nowMs()
		if err := q.SaveLaunchpadAccount(ctx, acc); err != nil {
			return err
		}
		out = toPortalPerson(&p)
		return c.portalAudit(ctx, q, acc.ID, "buildspace_launchpad.portal.profile_saved", "buildspace_launchpad_person", p.ID, toPortalPerson(&before), out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"me": out})
	return nil
}

func (c *Ctx) portalBuilderProject() error {
	s, err := c.portal("builder")
	if err != nil {
		return err
	}
	var np newProject
	var stage string
	if err := c.jsonBody(func(r *validate.Reader) {
		np = readNewProject(r)
		stage = r.Str("stage", validate.S{Req: true, Enum: buildspaceStages}).V
	}); err != nil {
		return err
	}
	var out portalProject
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		if err := c.portalIn(ctx, q, s, "builder"); err != nil {
			return err
		}
		bc, err := loadBuilder(ctx, q, s)
		if err != nil {
			return err
		}
		if err := bc.writable(); err != nil {
			return err
		}
		before := *bc.project
		p := *bc.project // mentors and cohort stay as they are
		p.Name, p.Description, p.Track, p.Stage, p.Links, p.UpdatedAt = np.name, np.description, np.track, stage, np.links, c.S.nowMs()
		if err := q.SaveBuildspaceProject(ctx, p); err != nil {
			return err
		}
		out = toPortalProject(&p)
		return c.portalAudit(ctx, q, s.account.ID, "buildspace_launchpad.portal.project_saved", "buildspace_launchpad_project", p.ID, toPortalProject(&before), out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"project": out})
	return nil
}

func (c *Ctx) portalBuilderActivity() error {
	s, err := c.portal("builder")
	if err != nil {
		return err
	}
	a := store.BuildspaceActivity{ID: auth.UUID(), CreatedAt: c.S.nowMs(), Source: "manual", Visibility: "team", AuthorAccountID: &s.account.ID}
	if err := c.jsonBody(func(r *validate.Reader) {
		a.Kind = r.Str("kind", validate.S{Req: true, Enum: []string{"weekly", "request", "absence"}}).V
		a.Date = r.Str("date", validate.S{Req: true, Format: "date"}).V
		a.Body = r.Str("body", validate.S{Req: true, Trim: true, Min: 1, Max: 10000}).V
		a.Status = r.Str("status", validate.S{Req: true, Enum: []string{"open", "on-track", "needs-help"}}).V
	}); err != nil {
		return err
	}
	return c.portalInsertActivity(s, "builder", a, func(q *store.Queries, b *store.Buildspace, a *store.BuildspaceActivity) error {
		bc, err := loadBuilder(c.Context(), q, s, b)
		if err != nil {
			return err
		}
		if err := bc.writable(); err != nil {
			return err
		}
		a.ProjectID, a.PersonID = bc.project.ID, &bc.person.ID
		return nil
	})
}

// portalInsertActivity revalidates, scopes, inserts and audits a portal entry, answering
// with its week.
func (c *Ctx) portalInsertActivity(s *portalSession, kind string, a store.BuildspaceActivity, scope func(q *store.Queries, b *store.Buildspace, a *store.BuildspaceActivity) error) error {
	var week store.BuildspaceWeek
	ctx := c.Context()
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		if err := c.portalIn(ctx, q, s, kind); err != nil {
			return err
		}
		b, err := q.BuildspaceRoster(ctx)
		if err != nil {
			return err
		}
		if err := scope(q, &b, &a); err != nil {
			return err
		}
		rules, err := q.BuildspaceWeekRules(ctx)
		if err != nil {
			return err
		}
		week = store.BuildspaceWeekOf(rules, a.Date)
		if err := q.InsertBuildspaceActivity(ctx, a); err != nil {
			return err
		}
		return c.portalAudit(ctx, q, *a.AuthorAccountID, "buildspace_launchpad.activity.created", "buildspace_launchpad_activity", a.ID, nil, a)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true, "week": week})
	return nil
}

// mentorProjects are the live projects (not archived, active cohort) this mentor is assigned to.
func mentorProjects(b *store.Buildspace, contactID string) []*store.BuildspaceProject {
	out := []*store.BuildspaceProject{}
	for i := range b.Projects {
		p := &b.Projects[i]
		if !b.ProjectLive(p) {
			continue
		}
		for _, id := range p.MentorIDs {
			if id == contactID {
				out = append(out, p)
			}
		}
	}
	return out
}

func (c *Ctx) portalMentor() error {
	s, err := c.portal("mentor")
	if err != nil {
		return err
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
	b, err := q.BuildspaceMentorPage(c.Context(), store.BuildspaceActivityPage{WeekStart: sched.Week.Start, WeekDue: sched.Week.Due, Cursor: activityCursor, MentorID: deref(s.account.ContactID), Visibility: []string{"team", "mentors"}})
	if err != nil {
		return err
	}
	contactID := *s.account.ContactID
	card := mentorCard(&b, contactID)
	if card.Name == "" {
		card.Name = s.account.Name
	}
	type builder struct {
		portalPerson
		ThisWeek string `json:"thisWeek"`
	}
	type project struct {
		portalProject
		CohortName string             `json:"cohortName"`
		Builders   []builder          `json:"builders"`
		CoMentors  []portalMentorCard `json:"coMentors"`
		Activity   []portalActivity   `json:"activity"`
		Flags      struct {
			Awaiting     int  `json:"awaiting"`
			NeedsHelp    bool `json:"needsHelp"`
			OpenRequests int  `json:"openRequests"`
		} `json:"flags"`
	}
	list := []project{}
	for _, p := range mentorProjects(&b, contactID) {
		v := project{portalProject: toPortalProject(p), Builders: []builder{}, CoMentors: []portalMentorCard{}}
		if co := b.Cohort(p.CohortID); co != nil {
			v.CohortName = co.Name
		}
		for i := range b.People {
			person := &b.People[i]
			if person.ProjectID != p.ID || person.ArchivedAt != nil {
				continue
			}
			st := weekStatus(&b, sched, person)
			if st == "awaiting" || st == "past-due" {
				v.Flags.Awaiting++
			}
			v.Builders = append(v.Builders, builder{toPortalPerson(person), st})
		}
		for _, id := range p.MentorIDs {
			if id != contactID {
				v.CoMentors = append(v.CoMentors, mentorCard(&b, id))
			}
		}
		v.Activity = visibleActivity(&b, p.ID, "team", "mentors")
		for _, a := range v.Activity {
			if a.Status == "needs-help" && a.Date >= sched.Week.Start && a.Date <= sched.Week.Due {
				v.Flags.NeedsHelp = true
			}
			if a.Status == "open" && (a.Kind == "request" || a.Kind == "purchase") {
				v.Flags.OpenRequests++
			}
		}
		list = append(list, v)
	}
	writeJSON(c.W, 200, map[string]any{"mentor": card, "projects": list, "schedule": sched, "activityNextCursor": b.ActivityNextCursor})
	return nil
}

func (c *Ctx) portalMentorActivity() error {
	s, err := c.portal("mentor")
	if err != nil {
		return err
	}
	a := store.BuildspaceActivity{ID: auth.UUID(), CreatedAt: c.S.nowMs(), Source: "manual", Kind: "mentor", AuthorAccountID: &s.account.ID}
	if err := c.jsonBody(func(r *validate.Reader) {
		a.ProjectID = r.Str("projectId", validate.S{Req: true, Min: 1, Max: 64}).V
		if v := r.Str("personId", validate.S{Max: 64, Null: true}).V; v != "" {
			a.PersonID = &v
		}
		a.Date = r.Str("date", validate.S{Req: true, Format: "date"}).V
		a.Body = r.Str("body", validate.S{Req: true, Trim: true, Min: 1, Max: 10000}).V
		a.Status = r.Str("status", validate.S{Req: true, Enum: []string{"on-track", "needs-help"}}).V
		a.Visibility = r.Str("visibility", validate.S{Req: true, Enum: []string{"team", "mentors"}}).V
	}); err != nil {
		return err
	}
	return c.portalInsertActivity(s, "mentor", a, func(q *store.Queries, b *store.Buildspace, a *store.BuildspaceActivity) error {
		contactID := *s.account.ContactID
		a.MentorID = &contactID
		assigned := false
		for _, p := range mentorProjects(b, contactID) {
			assigned = assigned || p.ID == a.ProjectID
		}
		if !assigned {
			return notFoundErr("Project not found")
		}
		if a.PersonID != nil {
			p := b.Person(*a.PersonID)
			if p == nil || p.ProjectID != a.ProjectID || p.ArchivedAt != nil {
				return badRequest("Builder must belong to the project")
			}
		}
		return nil
	})
}
