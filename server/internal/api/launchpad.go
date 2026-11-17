package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// Launchpad cohorts, portal-account invites, the mentor roster, auto-assignment and archiving
// (docs/launchpad-cohorts.md). Everything here is an officer API under /api/buildspace-launchpad.

// launchpadInviteTTL is how long a builder or mentor invite link stays valid.
const launchpadInviteTTL = 14 * 24 * time.Hour

// ── cohorts ──

func readCohortDates(r *validate.Reader, startReq bool) (name, starts validate.Field[string], ends validate.Field[string]) {
	name = r.Str("name", validate.S{Req: startReq, Trim: true, Min: 1, Max: 100})
	starts = r.Str("startsOn", validate.S{Req: startReq, Format: "date"})
	ends = r.Str("endsOn", validate.S{Null: true, Format: "date"})
	return
}

func (c *Ctx) launchpadCohortCreate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	now := c.S.nowMs()
	co := store.LaunchpadCohort{ID: auth.UUID(), Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := c.jsonBody(func(r *validate.Reader) {
		name, starts, ends := readCohortDates(r, true)
		co.Name, co.StartsOn = name.V, starts.V
		if ends.Has() {
			co.EndsOn = &ends.V
		}
	}); err != nil {
		return err
	}
	if co.EndsOn != nil && *co.EndsOn < co.StartsOn {
		return badRequest("The end date must be on or after the start date")
	}
	var out store.LaunchpadCohort
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if taken, err := q.LaunchpadCohortNameTaken(c.Context(), co.Name, ""); err != nil || taken {
			if err != nil {
				return err
			}
			return conflict("A cohort with that name already exists")
		}
		if err := q.SaveLaunchpadCohort(c.Context(), co); err != nil {
			return err
		}
		var err error
		if out, err = q.LaunchpadCohort(c.Context(), co.ID); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.cohort.created", "buildspace_launchpad_cohort", co.ID, nil, out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"cohort": out})
	return nil
}

func (c *Ctx) launchpadCohortUpdate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	var name, starts, ends, status validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		name, starts, ends = readCohortDates(r, false)
		status = r.Str("status", validate.S{Enum: []string{"active", "archived"}})
	}); err != nil {
		return err
	}
	id := c.R.PathValue("id")
	var out store.LaunchpadCohort
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		before, err := q.LaunchpadCohort(c.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			return notFoundErr("Cohort not found")
		} else if err != nil {
			return err
		}
		co := before
		if name.Set {
			co.Name = name.V
		}
		if starts.Set {
			co.StartsOn = starts.V
		}
		if ends.Set {
			co.EndsOn = nil
			if !ends.Null {
				co.EndsOn = &ends.V
			}
		}
		if status.Set {
			co.Status = status.V
		}
		if co.EndsOn != nil && *co.EndsOn < co.StartsOn {
			return badRequest("The end date must be on or after the start date")
		}
		if taken, err := q.LaunchpadCohortNameTaken(c.Context(), co.Name, id); err != nil {
			return err
		} else if taken {
			return conflict("A cohort with that name already exists")
		}
		co.UpdatedAt = c.S.nowMs()
		if err := q.SaveLaunchpadCohort(c.Context(), co); err != nil {
			return err
		}
		if out, err = q.LaunchpadCohort(c.Context(), id); err != nil {
			return err
		}
		action := "buildspace_launchpad.cohort.updated"
		if before.Status != co.Status {
			action = map[string]string{"archived": "buildspace_launchpad.cohort.archived", "active": "buildspace_launchpad.cohort.restored"}[co.Status]
		}
		return c.audit(c.Context(), q, action, "buildspace_launchpad_cohort", id, before, out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"cohort": out})
	return nil
}

func (c *Ctx) launchpadCohortDelete() error {
	if _, err := c.director(); err != nil {
		return err
	}
	id := c.R.PathValue("id")
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		before, err := q.LaunchpadCohort(c.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			return notFoundErr("Cohort not found")
		} else if err != nil {
			return err
		}
		empty, err := q.LaunchpadCohortEmpty(c.Context(), id)
		if err != nil {
			return err
		}
		if !empty {
			return conflict("Only an empty cohort can be deleted; archive it instead")
		}
		if err := q.DeleteLaunchpadCohort(c.Context(), id); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.cohort.deleted", "buildspace_launchpad_cohort", id, before, nil)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

// ── invites and accounts ──

type launchpadLink struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
	URL       string `json:"url"`
}

// issued is an invite token handed out in this request: only its hash is stored.
type issued struct {
	account store.LaunchpadAccount
	token   string
	cohort  string // cohort name for builders
}

// reissue gives the account a fresh token and expiry and clears the delivery record.
func reissue(a *store.LaunchpadAccount, now int64) string {
	token := auth.RandomToken(32)
	h := auth.SHA256Hex(token)
	exp := now + launchpadInviteTTL.Milliseconds()
	a.InviteTokenHash, a.InviteExpiresAt, a.InviteSentAt, a.InviteError = &h, &exp, nil, nil
	a.UpdatedAt = now
	a.MarkExpired(now)
	return token
}

func (c *Ctx) launchpadJoinURL(token string) string {
	return c.S.link("/launchpad/join?token="+token, c.R)
}

// agentInvite refuses what an MCP agent may not do with an invite: an invite link is a
// sign-up credential, so through an agent invites go by email only and links never come back.
func (c *Ctx) agentInvite(method string) error {
	if c.Agent == "" {
		return nil
	}
	if method != "email" {
		return badRequest("Through an agent, invites go by email only (method \"email\"): an invite link is a sign-up credential. Copy links from the dashboard instead.")
	}
	if c.S.cfg.Mailer == nil {
		return conflict("Invite email is not configured on this dashboard; send the invite from the dashboard instead")
	}
	return nil
}

// deliver emails each invite in the background (when the mailer is configured and the method
// asks for email) and returns copyable links (when the method asks for them, or there is no
// mailer). Delivery outcomes land on the account, only if its token is still the one sent.
func (c *Ctx) deliver(list []issued, method string) []launchpadLink {
	links := []launchpadLink{}
	mailer := c.S.cfg.Mailer
	var mails []issued
	for _, it := range list {
		if (method != "email" || mailer == nil) && c.Agent == "" { // never hand a link to an agent
			links = append(links, launchpadLink{AccountID: it.account.ID, Email: it.account.Email, URL: c.launchpadJoinURL(it.token)})
		}
		if method != "link" && mailer != nil {
			mails = append(mails, it)
		}
	}
	if len(mails) == 0 {
		return links
	}
	type msg struct {
		id, hash string
		m        calendar.Message
	}
	out := make([]msg, 0, len(mails))
	for _, it := range mails {
		out = append(out, msg{it.account.ID, *it.account.InviteTokenHash, launchpadInviteMessage(it, c.launchpadJoinURL(it.token))})
	}
	s := c.S
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ctx := context.Background()
		for _, m := range out {
			err := mailer.Send(ctx, m.m)
			var sent *int64
			var msgErr *string
			if err != nil {
				e := err.Error()
				if len(e) > 300 {
					e = e[:300]
				}
				msgErr = &e
				s.log.Warn("launchpad invite email failed", "account", m.id, "error", err)
			} else {
				now := s.nowMs()
				sent = &now
			}
			if werr := s.db.Write(ctx, func(q *store.Queries) error {
				return q.RecordLaunchpadInviteDelivery(ctx, m.id, m.hash, sent, msgErr)
			}); werr != nil {
				s.log.Error("record launchpad invite delivery", "error", werr)
			}
		}
	}()
	return links
}

func launchpadInviteMessage(it issued, link string) calendar.Message {
	greeting := "Hi,"
	if n := strings.TrimSpace(it.account.Name); n != "" {
		greeting = "Hi " + n + ","
	}
	var subject, role string
	if it.account.Kind == "mentor" {
		subject = "You're invited to mentor on CSEED Buildspace + Launchpad"
		role = "as a mentor. You'll see the projects you're assigned, their weekly updates, and can leave notes for the team."
	} else {
		subject = "You're invited to " + it.cohort + " on CSEED Buildspace + Launchpad"
		role = "as a builder in " + it.cohort + ". Sign in with Discord, tell us about yourself, and start or join a project."
	}
	text := fmt.Sprintf("%s\n\nYou've been invited to CSEED Buildspace + Launchpad %s\n\nAccept your invite:\n%s\n\nThis link is just for you and expires in 14 days. If you weren't expecting it, you can ignore this email.\n", greeting, role, link)
	return calendar.Message{To: it.account.Email, Subject: subject, Text: text}
}

func (c *Ctx) launchpadInvite() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	type row struct{ email, name string }
	var rows []row
	var method string
	if err := c.jsonBody(func(r *validate.Reader) {
		r.Objects("invites", true, 200, func(i int, sub *validate.Reader) {
			e := strings.ToLower(sub.Str("email", validate.S{Req: true, Trim: true, Max: 254}).V)
			if !validate.IsEmail(e) {
				sub.Issues.Add(fmt.Sprintf("invites.%d.email", i), "Invalid email")
			}
			rows = append(rows, row{e, sub.Str("name", validate.S{Trim: true, Max: 200}).V})
		})
		method = r.Str("method", validate.S{Enum: []string{"email", "link", "both"}, HasDef: true, Default: "email"}).V
	}); err != nil {
		return err
	}
	if len(rows) == 0 {
		return badRequest("Add at least one email")
	}
	if err := c.agentInvite(method); err != nil {
		return err
	}
	type skip struct {
		Email  string `json:"email"`
		Reason string `json:"reason"`
	}
	skipped := []skip{}
	var list []issued
	cohortID := c.R.PathValue("id")
	now := c.S.nowMs()
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		co, err := q.LaunchpadCohort(c.Context(), cohortID)
		if errors.Is(err, store.ErrNotFound) {
			return notFoundErr("Cohort not found")
		} else if err != nil {
			return err
		}
		if co.Status != "active" {
			return conflict("This cohort is archived")
		}
		seen := map[string]bool{}
		for _, r := range rows {
			if seen[r.email] {
				skipped = append(skipped, skip{r.email, "duplicate"})
				continue
			}
			seen[r.email] = true
			a, err := q.LaunchpadBuilderAccountByEmail(c.Context(), cohortID, r.email)
			action := "buildspace_launchpad.account.reissued"
			switch {
			case err == nil && a.Status == "active":
				skipped = append(skipped, skip{r.email, "already_active"})
				continue
			case err == nil && a.Status == "disabled":
				skipped = append(skipped, skip{r.email, "disabled"})
				continue
			case err == nil:
				if r.name != "" {
					a.Name = r.name
				}
			case errors.Is(err, store.ErrNotFound):
				action = "buildspace_launchpad.account.invited"
				a = store.LaunchpadAccount{ID: auth.UUID(), Kind: "builder", Email: r.email, Name: r.name, Status: "invited", CohortID: &cohortID, CreatedAt: now}
			default:
				return err
			}
			a.InvitedBy = &u.ID
			token := reissue(&a, now)
			if err := q.SaveLaunchpadAccount(c.Context(), a); err != nil {
				return err
			}
			if err := c.audit(c.Context(), q, action, "buildspace_launchpad_account", a.ID, nil, map[string]any{"kind": "builder", "email": a.Email, "cohortId": cohortID, "method": method}); err != nil {
				return err
			}
			list = append(list, issued{a, token, co.Name})
		}
		return nil
	})
	if err != nil {
		return err
	}
	accounts := make([]store.LaunchpadAccount, len(list))
	for i := range list {
		accounts[i] = list[i].account
	}
	links := c.deliver(list, method)
	writeJSON(c.W, 200, map[string]any{"accounts": accounts, "links": links, "skipped": skipped, "emailConfigured": c.S.cfg.Mailer != nil})
	return nil
}

// accountAction loads an account in a write transaction and checks who may manage it:
// directors for mentor accounts (and for the director-only actions), officers otherwise.
func (c *Ctx) accountAction(directorOnly bool, fn func(q *store.Queries, a *store.LaunchpadAccount) error) error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	return c.S.db.Write(c.Context(), func(q *store.Queries) error {
		a, err := q.LaunchpadAccount(c.Context(), c.R.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return notFoundErr("Account not found")
		} else if err != nil {
			return err
		}
		if (directorOnly || a.Kind == "mentor") && !auth.HasRole(u.Role, auth.RoleDirector) {
			return forbidden("Requires director role")
		}
		return fn(q, &a)
	})
}

// cohortName returns the account's cohort name (builders), refusing archived cohorts.
func cohortForInvite(ctx context.Context, q *store.Queries, a *store.LaunchpadAccount) (string, error) {
	if a.CohortID == nil {
		return "", nil
	}
	co, err := q.LaunchpadCohort(ctx, *a.CohortID)
	if err != nil {
		return "", err
	}
	if co.Status != "active" {
		return "", conflict("This cohort is archived")
	}
	return co.Name, nil
}

func (c *Ctx) launchpadAccountResend() error {
	if err := c.agentInvite("email"); err != nil {
		return err
	}
	var it issued
	err := c.accountAction(false, func(q *store.Queries, a *store.LaunchpadAccount) error {
		if a.Status != "invited" {
			return conflict("Only pending invites can be resent")
		}
		name, err := cohortForInvite(c.Context(), q, a)
		if err != nil {
			return err
		}
		a.InvitedBy = &c.User.ID
		token := reissue(a, c.S.nowMs())
		if err := q.SaveLaunchpadAccount(c.Context(), *a); err != nil {
			return err
		}
		it = issued{*a, token, name}
		return c.audit(c.Context(), q, "buildspace_launchpad.account.reissued", "buildspace_launchpad_account", a.ID, nil, map[string]any{"kind": a.Kind, "email": a.Email})
	})
	if err != nil {
		return err
	}
	// A resend always hands back the link as well, so it can be shared another way.
	c.deliver([]issued{it}, "email")
	var link any
	if c.Agent == "" {
		link = launchpadLink{AccountID: it.account.ID, Email: it.account.Email, URL: c.launchpadJoinURL(it.token)}
	}
	writeJSON(c.W, 200, map[string]any{"account": it.account, "link": link})
	return nil
}

func (c *Ctx) launchpadAccountSetStatus(disable bool) error {
	var out store.LaunchpadAccount
	err := c.accountAction(true, func(q *store.Queries, a *store.LaunchpadAccount) error {
		before := *a
		if disable {
			if a.Status == "disabled" {
				return conflict("Account is already disabled")
			}
			a.Status = "disabled"
		} else {
			if a.Status != "disabled" {
				return conflict("Account is not disabled")
			}
			// Someone who never accepted goes back to a pending invite (resend to share a link).
			a.Status = "active"
			if a.DiscordID == nil {
				a.Status = "invited"
			}
		}
		a.UpdatedAt = c.S.nowMs()
		if err := q.SaveLaunchpadAccount(c.Context(), *a); err != nil {
			return err
		}
		if err := q.DeleteLaunchpadSessionsFor(c.Context(), a.ID); err != nil {
			return err
		}
		a.MarkExpired(c.S.nowMs())
		out = *a
		action := "buildspace_launchpad.account.enabled"
		if disable {
			action = "buildspace_launchpad.account.disabled"
		}
		return c.audit(c.Context(), q, action, "buildspace_launchpad_account", a.ID, map[string]any{"status": before.Status}, map[string]any{"status": a.Status})
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"account": out, "link": nil})
	return nil
}
func (c *Ctx) launchpadAccountDisable() error { return c.launchpadAccountSetStatus(true) }
func (c *Ctx) launchpadAccountEnable() error  { return c.launchpadAccountSetStatus(false) }

func (c *Ctx) launchpadAccountRevoke() error {
	err := c.accountAction(false, func(q *store.Queries, a *store.LaunchpadAccount) error {
		if a.Status != "invited" {
			return conflict("Only pending invites can be revoked; disable an active account instead")
		}
		if err := q.DeleteLaunchpadAccount(c.Context(), a.ID); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.account.revoked", "buildspace_launchpad_account", a.ID, map[string]any{"kind": a.Kind, "email": a.Email}, nil)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

// ── mentor roster ──

func (c *Ctx) launchpadMentorInvite() error {
	u, err := c.director()
	if err != nil {
		return err
	}
	var email, method string
	if err := c.jsonBody(func(r *validate.Reader) {
		email = strings.ToLower(r.Str("email", validate.S{Trim: true, Max: 254}).V)
		if email != "" && !validate.IsEmail(email) {
			r.Issues.Add("email", "Invalid email")
		}
		method = r.Str("method", validate.S{Enum: []string{"email", "link", "both"}, HasDef: true, Default: "email"}).V
	}); err != nil {
		return err
	}
	if err := c.agentInvite(method); err != nil {
		return err
	}
	contactID := c.R.PathValue("contactId")
	var it issued
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		m := b.Mentor(contactID)
		if m == nil || !m.IsMentorType() {
			return notFoundErr("Mentor contact not found")
		}
		if email == "" {
			email = strings.ToLower(strings.TrimSpace(m.Email))
		}
		if email == "" || !validate.IsEmail(email) {
			return badRequest("Add an email for this mentor")
		}
		now := c.S.nowMs()
		a, err := q.LaunchpadAccountByContact(c.Context(), contactID)
		action := "buildspace_launchpad.account.reissued"
		switch {
		case err == nil && a.Status == "active":
			return conflict("This mentor already has an active account")
		case err == nil && a.Status == "disabled":
			return conflict("This mentor's account is disabled; enable it instead")
		case err == nil:
		case errors.Is(err, store.ErrNotFound):
			action = "buildspace_launchpad.account.invited"
			a = store.LaunchpadAccount{ID: auth.UUID(), Kind: "mentor", Status: "invited", ContactID: &contactID, CreatedAt: now}
		default:
			return err
		}
		a.Email, a.Name, a.InvitedBy = email, m.Name, &u.ID
		token := reissue(&a, now)
		if err := q.SaveLaunchpadAccount(c.Context(), a); err != nil {
			return err
		}
		it = issued{account: a, token: token}
		return c.audit(c.Context(), q, action, "buildspace_launchpad_account", a.ID, nil, map[string]any{"kind": "mentor", "email": email, "contactId": contactID, "method": method})
	})
	if err != nil {
		return err
	}
	var link any
	if l := c.deliver([]issued{it}, method); len(l) > 0 {
		link = l[0]
	}
	writeJSON(c.W, 200, map[string]any{"account": it.account, "link": link})
	return nil
}

func (c *Ctx) launchpadMentorSettings() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var available validate.Field[bool]
	var max validate.Field[int64]
	if err := c.jsonBody(func(r *validate.Reader) {
		available = r.BoolField("available")
		if !available.Set {
			r.Issues.Add("available", "Invalid input: expected boolean, received undefined")
		}
		max = r.Int("maxProjects", validate.I{Req: true, Null: true, Min: validate.Lo(1), Max: validate.Lo(50)})
	}); err != nil {
		return err
	}
	contactID := c.R.PathValue("contactId")
	var out store.BuildspaceMentor
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		m := b.Mentor(contactID)
		if m == nil {
			return notFoundErr("Mentor contact not found")
		}
		before := map[string]any{"available": m.Available, "maxProjects": m.MaxProjects}
		var cap *int
		if max.Has() {
			v := int(max.V)
			cap = &v
		}
		if err := q.SaveLaunchpadMentorSettings(c.Context(), contactID, available.V, cap, c.S.nowMs()); err != nil {
			return err
		}
		if b, err = q.Buildspace(c.Context()); err != nil {
			return err
		}
		b.MarkExpired(c.S.nowMs())
		out = *b.Mentor(contactID)
		return c.audit(c.Context(), q, "buildspace_launchpad.mentor.settings_saved", "contact", contactID, before, map[string]any{"available": out.Available, "maxProjects": out.MaxProjects})
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"mentor": out})
	return nil
}

// ── auto-assign ──

type assignment struct {
	ProjectID string `json:"projectId"`
	MentorID  string `json:"mentorId"`
}
type assignLoad struct {
	ID         string `json:"id"`
	LoadBefore int    `json:"loadBefore"`
	LoadAfter  int    `json:"loadAfter"`
	HasAccount bool   `json:"hasAccount"`
}

// autoAssignCandidates are the cohort's projects that are not archived, not shipped, and
// have no mentor.
func autoAssignCandidates(b *store.Buildspace, cohortID string) []*store.BuildspaceProject {
	out := []*store.BuildspaceProject{}
	for i := range b.Projects {
		p := &b.Projects[i]
		if p.CohortID == cohortID && p.ArchivedAt == nil && p.Stage != "Shipped" && len(p.MentorIDs) == 0 {
			out = append(out, p)
		}
	}
	return out
}

func hasCapacity(m *store.BuildspaceMentor, load int) bool {
	return m.MaxProjects == nil || load < *m.MaxProjects
}

func topicMatches(m *store.BuildspaceMentor, track string) bool {
	t := strings.ToLower(track)
	for _, topic := range m.Topics {
		tp := strings.ToLower(strings.TrimSpace(topic))
		if tp != "" && (strings.Contains(tp, t) || strings.Contains(t, tp)) {
			return true
		}
	}
	return false
}

func cryptoIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(v.Int64())
}

// planAutoAssign spreads the cohort's unmentored projects over eligible mentors: shuffled
// projects each go to the least-loaded mentor with capacity, preferring a mentor whose topics
// mention the project's track, then at random. Load counts live projects in active cohorts.
func planAutoAssign(b *store.Buildspace, cohortID string, intn func(int) int) ([]assignment, []string, []assignLoad) {
	var mentors []*store.BuildspaceMentor
	for i := range b.Mentors {
		m := &b.Mentors[i]
		if m.IsMentorType() && m.Available && hasCapacity(m, m.Load) {
			mentors = append(mentors, m)
		}
	}
	load := map[string]int{}
	for _, m := range mentors {
		load[m.ID] = m.Load
	}
	projects := autoAssignCandidates(b, cohortID)
	for i := len(projects) - 1; i > 0; i-- {
		j := intn(i + 1)
		projects[i], projects[j] = projects[j], projects[i]
	}
	assignments, unassigned := []assignment{}, []string{}
	for _, p := range projects {
		var best []*store.BuildspaceMentor
		for _, m := range mentors {
			if !hasCapacity(m, load[m.ID]) {
				continue
			}
			switch {
			case len(best) == 0 || load[m.ID] < load[best[0].ID]:
				best = []*store.BuildspaceMentor{m}
			case load[m.ID] == load[best[0].ID]:
				best = append(best, m)
			}
		}
		if len(best) == 0 {
			unassigned = append(unassigned, p.ID)
			continue
		}
		var matched []*store.BuildspaceMentor
		for _, m := range best {
			if topicMatches(m, p.Track) {
				matched = append(matched, m)
			}
		}
		if len(matched) > 0 {
			best = matched
		}
		pick := best[intn(len(best))]
		load[pick.ID]++
		assignments = append(assignments, assignment{p.ID, pick.ID})
	}
	loads := []assignLoad{}
	for _, m := range mentors {
		loads = append(loads, assignLoad{m.ID, m.Load, load[m.ID], m.Account != nil && m.Account.Status == "active"})
	}
	return assignments, unassigned, loads
}

// liveCohort reads the register and the path's cohort, refusing a missing or archived one.
func liveCohort(ctx context.Context, q *store.Queries, id string) (store.Buildspace, error) {
	b, err := q.Buildspace(ctx)
	if err != nil {
		return b, err
	}
	co := b.Cohort(id)
	if co == nil {
		return b, notFoundErr("Cohort not found")
	}
	if co.Status != "active" {
		return b, conflict("This cohort is archived")
	}
	return b, nil
}

func (c *Ctx) launchpadAutoAssignPreview() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	b, err := liveCohort(c.Context(), c.S.db.Read(), c.R.PathValue("id"))
	if err != nil {
		return err
	}
	a, u, m := planAutoAssign(&b, c.R.PathValue("id"), cryptoIntn)
	writeJSON(c.W, 200, map[string]any{"assignments": a, "unassigned": u, "mentors": m})
	return nil
}

func (c *Ctx) launchpadAutoAssign() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var pairs []assignment
	if err := c.jsonBody(func(r *validate.Reader) {
		r.Objects("assignments", true, 500, func(i int, sub *validate.Reader) {
			pairs = append(pairs, assignment{
				sub.Str("projectId", validate.S{Req: true, Min: 1, Max: 64}).V,
				sub.Str("mentorId", validate.S{Req: true, Min: 1, Max: 64}).V,
			})
		})
	}); err != nil {
		return err
	}
	if len(pairs) == 0 {
		return badRequest("Nothing to assign")
	}
	cohortID := c.R.PathValue("id")
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := liveCohort(c.Context(), q, cohortID)
		if err != nil {
			return err
		}
		stale := conflict("The plan is out of date; preview again")
		seen, added := map[string]bool{}, map[string]int{}
		for _, a := range pairs {
			p := b.Project(a.ProjectID)
			if p == nil || p.CohortID != cohortID || p.ArchivedAt != nil || p.Stage == "Shipped" || len(p.MentorIDs) > 0 || seen[p.ID] {
				return stale
			}
			seen[p.ID] = true
			m := b.Mentor(a.MentorID)
			if m == nil || !m.IsMentorType() || !m.Available {
				return stale
			}
			added[m.ID]++
			if m.MaxProjects != nil && m.Load+added[m.ID] > *m.MaxProjects {
				return stale
			}
		}
		now := c.S.nowMs()
		for _, a := range pairs {
			if err := q.AddBuildspaceMentor(c.Context(), a.ProjectID, a.MentorID, now); err != nil {
				return err
			}
			if err := c.audit(c.Context(), q, "buildspace_launchpad.project.mentor_auto_assigned", "buildspace_launchpad_project", a.ProjectID, nil, a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true, "assigned": len(pairs)})
	return nil
}

// ── archive, restore, delete ──

// registerWrite runs fn with the whole register in a write transaction.
func (c *Ctx) registerWrite(directorOnly bool, fn func(q *store.Queries, b *store.Buildspace) error) error {
	var err error
	if directorOnly {
		_, err = c.director()
	} else {
		_, err = c.officer()
	}
	if err != nil {
		return err
	}
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		b, err := q.Buildspace(c.Context())
		if err != nil {
			return err
		}
		return fn(q, &b)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

func activeCohort(b *store.Buildspace, id string) error {
	if co := b.Cohort(id); co == nil || co.Status != "active" {
		return conflict("This cohort is archived")
	}
	return nil
}

func (c *Ctx) launchpadProjectArchive() error {
	return c.registerWrite(false, func(q *store.Queries, b *store.Buildspace) error {
		p := b.Project(c.R.PathValue("id"))
		if p == nil {
			return notFoundErr("Project not found")
		}
		if err := activeCohort(b, p.CohortID); err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return nil
		}
		now := c.S.nowMs()
		if err := q.SetBuildspaceProjectArchived(c.Context(), p.ID, &now); err != nil {
			return err
		}
		// Its builders are archived with it (same timestamp, so a restore brings back exactly them).
		people := []string{}
		for _, v := range b.People {
			if v.ProjectID == p.ID && v.ArchivedAt == nil {
				if err := q.SetBuildspacePersonArchived(c.Context(), v.ID, &now); err != nil {
					return err
				}
				people = append(people, v.ID)
			}
		}
		if err := q.DeleteLaunchpadSessionsForProject(c.Context(), p.ID); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.project.archived", "buildspace_launchpad_project", p.ID, nil, map[string]any{"archivedAt": now, "people": people})
	})
}

func (c *Ctx) launchpadProjectRestore() error {
	return c.registerWrite(false, func(q *store.Queries, b *store.Buildspace) error {
		p := b.Project(c.R.PathValue("id"))
		if p == nil {
			return notFoundErr("Project not found")
		}
		if err := activeCohort(b, p.CohortID); err != nil {
			return err
		}
		if p.ArchivedAt == nil {
			return nil
		}
		if err := q.SetBuildspaceProjectArchived(c.Context(), p.ID, nil); err != nil {
			return err
		}
		people := []string{}
		for _, v := range b.People {
			if v.ProjectID == p.ID && v.ArchivedAt != nil && *v.ArchivedAt == *p.ArchivedAt {
				if err := q.SetBuildspacePersonArchived(c.Context(), v.ID, nil); err != nil {
					return err
				}
				people = append(people, v.ID)
			}
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.project.restored", "buildspace_launchpad_project", p.ID, nil, map[string]any{"people": people})
	})
}

func (c *Ctx) launchpadProjectDelete() error {
	return c.registerWrite(true, func(q *store.Queries, b *store.Buildspace) error {
		p := b.Project(c.R.PathValue("id"))
		if p == nil {
			return notFoundErr("Project not found")
		}
		for _, v := range b.People {
			if v.ProjectID == p.ID {
				return conflict("This project still has builders; delete or archive it instead")
			}
		}
		for _, a := range b.Activity {
			if a.ProjectID == p.ID {
				return conflict("This project has history; archive it instead")
			}
		}
		if err := q.DeleteBuildspaceProject(c.Context(), p.ID); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.project.deleted", "buildspace_launchpad_project", p.ID, *p, nil)
	})
}

func (c *Ctx) launchpadPersonArchive() error {
	return c.registerWrite(false, func(q *store.Queries, b *store.Buildspace) error {
		p := b.Person(c.R.PathValue("id"))
		if p == nil {
			return notFoundErr("Builder not found")
		}
		if err := activeCohort(b, p.CohortID); err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return nil
		}
		now := c.S.nowMs()
		if err := q.SetBuildspacePersonArchived(c.Context(), p.ID, &now); err != nil {
			return err
		}
		if p.AccountID != nil {
			if err := q.DeleteLaunchpadSessionsFor(c.Context(), *p.AccountID); err != nil {
				return err
			}
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.person.archived", "buildspace_launchpad_person", p.ID, nil, map[string]any{"archivedAt": now})
	})
}

func (c *Ctx) launchpadPersonRestore() error {
	return c.registerWrite(false, func(q *store.Queries, b *store.Buildspace) error {
		p := b.Person(c.R.PathValue("id"))
		if p == nil {
			return notFoundErr("Builder not found")
		}
		if err := activeCohort(b, p.CohortID); err != nil {
			return err
		}
		if p.ArchivedAt == nil {
			return nil
		}
		if pr := b.Project(p.ProjectID); pr != nil && pr.ArchivedAt != nil {
			return conflict("Restore their project first")
		}
		if err := q.SetBuildspacePersonArchived(c.Context(), p.ID, nil); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.person.restored", "buildspace_launchpad_person", p.ID, nil, nil)
	})
}

func (c *Ctx) launchpadPersonDelete() error {
	return c.registerWrite(true, func(q *store.Queries, b *store.Buildspace) error {
		p := b.Person(c.R.PathValue("id"))
		if p == nil {
			return notFoundErr("Builder not found")
		}
		for _, a := range b.Activity {
			if a.PersonID != nil && *a.PersonID == p.ID {
				return conflict("This builder has history; archive them instead")
			}
		}
		if p.AccountID != nil {
			if err := q.DeleteLaunchpadAccount(c.Context(), *p.AccountID); err != nil {
				return err
			}
		}
		if err := q.DeleteBuildspacePerson(c.Context(), p.ID); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "buildspace_launchpad.person.deleted", "buildspace_launchpad_person", p.ID, *p, nil)
	})
}
