package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// OfficerDTO is one row of the Officers directory. Phone is present only for director+
// viewers (and on your own profile); birthday is month-day and visible to every officer.
type OfficerDTO struct {
	UserSummary
	DiscordID       string      `json:"discordId"`
	Role            string      `json:"role"`
	IsOwner         bool        `json:"isOwner"`
	Active          bool        `json:"active"`
	Title           *string     `json:"title"`
	Subteam         *SubteamDTO `json:"subteam"`
	PreferredName   *string     `json:"preferredName"`
	Pronouns        *string     `json:"pronouns"`
	Birthday        *string     `json:"birthday"`
	PersonalEmail   *string     `json:"personalEmail"`
	SchoolEmail     *string     `json:"schoolEmail"`
	GradDate        *string     `json:"gradDate"`
	StartTerm       *string     `json:"startTerm"`
	Team            *string     `json:"team"`
	LinkedIn        *string     `json:"linkedin"`
	Phone           *string     `json:"phone,omitempty"`
	ProfileComplete bool        `json:"profileComplete"`
	Onboarding      string      `json:"onboarding"` // none | pending | done
	LastLoginAt     *string     `json:"lastLoginAt,omitempty"`
}

func officerDTO(u *store.User, p *store.Profile, people *peopleIndex, inv *store.Invite, privileged bool) OfficerDTO {
	o := OfficerDTO{UserSummary: userSummary(u), DiscordID: u.DiscordID, Role: u.Role, IsOwner: u.IsOwner, Active: u.Active, Title: u.Title, Onboarding: "none"}
	if inv != nil {
		o.Onboarding = "pending"
		if inv.OnboardedAt != nil {
			o.Onboarding = "done"
		}
	}
	if p != nil {
		o.ProfileComplete = true
		o.Subteam = people.subteam(p.SubteamID)
		o.PreferredName, o.Pronouns, o.PersonalEmail, o.SchoolEmail = p.PreferredName, p.Pronouns, p.PersonalEmail, p.SchoolEmail
		o.GradDate, o.StartTerm, o.LinkedIn, o.Team = p.GradDate, p.StartTerm, p.LinkedIn, p.Team
		b := p.Birthday
		o.Birthday = &b
		if privileged {
			ph := p.Phone
			o.Phone = &ph
		}
	} else if inv != nil {
		o.Subteam = people.subteam(inv.SubteamID)
	}
	if privileged {
		o.LastLoginAt = isoPtr(u.LastLoginAt)
	}
	return o
}

func (c *Ctx) officerInvites(ctx context.Context, q *store.Queries) (map[string]*store.Invite, error) {
	rows, err := q.Invites(ctx, 100000)
	if err != nil {
		return nil, err
	}
	out := map[string]*store.Invite{}
	for i := range rows {
		if rows[i].UserID != nil {
			if _, seen := out[*rows[i].UserID]; !seen {
				out[*rows[i].UserID] = &rows[i]
			}
		}
	}
	return out, nil
}

func (c *Ctx) officersList() error {
	me, err := c.officer()
	if err != nil {
		return err
	}
	privileged := auth.HasRole(me.Role, auth.RoleDirector)
	ctx := c.Context()
	q := c.S.db.Read()
	people, err := loadPeople(ctx, q)
	if err != nil {
		return err
	}
	profiles, err := q.Profiles(ctx)
	if err != nil {
		return err
	}
	invites, err := c.officerInvites(ctx, q)
	if err != nil {
		return err
	}
	users, err := q.Users(ctx)
	if err != nil {
		return err
	}
	out := []OfficerDTO{}
	for i := range users {
		u := &users[i]
		// Officers see the active team; directors also see deactivated accounts.
		if !u.Active && !privileged {
			continue
		}
		var p *store.Profile
		if v, ok := profiles[u.ID]; ok {
			p = &v
		}
		out = append(out, officerDTO(u, p, people, invites[u.ID], privileged))
	}
	teams, err := q.Subteams(ctx)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"officers": out, "subteams": teams, "canManage": privileged, "canChangeRoles": auth.HasRole(me.Role, auth.RoleLeadership)})
	return nil
}

func (c *Ctx) loadOfficer(ctx context.Context, q *store.Queries, id string, privileged bool) (*OfficerDTO, error) {
	u, err := q.UserByID(ctx, id)
	if err != nil {
		return nil, notFoundErr("Officer not found")
	}
	if !u.Active && !privileged {
		return nil, notFoundErr("Officer not found")
	}
	p, err := q.ProfileByUser(ctx, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	people, err := loadPeople(ctx, q)
	if err != nil {
		return nil, err
	}
	inv, err := q.InviteForUser(ctx, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	o := officerDTO(u, p, people, inv, privileged)
	return &o, nil
}

func (c *Ctx) officerDetail() error {
	me, err := c.officer()
	if err != nil {
		return err
	}
	o, err := c.loadOfficer(c.Context(), c.S.db.Read(), c.R.PathValue("id"), auth.HasRole(me.Role, auth.RoleDirector))
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"officer": o})
	return nil
}

// officerUpdate lets director+ change someone's subteam and title. Role and active go
// through PATCH /api/admin/users/:id, which holds the leadership and owner rules.
func (c *Ctx) officerUpdate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	var subteam, title, startTerm validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		subteam = r.Str("subteamId", validate.S{Min: 1, Max: 64})
		title = r.Str("title", validate.S{Null: true, Trim: true, Max: 100})
		startTerm = r.Str("startTerm", validate.S{Null: true, Trim: true, Max: 40})
	}); err != nil {
		return err
	}
	ctx := c.Context()
	var out *OfficerDTO
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		before, err := c.loadOfficer(ctx, q, c.R.PathValue("id"), true)
		if err != nil {
			return err
		}
		if before.Role == auth.RoleLeadership {
			if _, err := c.requireLeadershipIn(q); err != nil {
				return err
			}
		}
		if subteam.Set {
			ok, err := q.SubteamExists(ctx, subteam.V)
			if err != nil {
				return err
			}
			if !ok {
				issues := &validate.Issues{}
				issues.Add("subteamId", "Subteam not found")
				return validationError(issues)
			}
			if !before.ProfileComplete {
				return conflict(before.Name + " hasn't filled in their profile yet, so there's no subteam to change.")
			}
		}
		if title.Set {
			if err := q.UpdateUser(ctx, before.ID, store.UserPatch{Title: optOf(title)}, c.S.nowMs()); err != nil {
				return err
			}
		}
		patch := store.ProfilePatch{SubteamID: optOf(subteam)}
		if before.ProfileComplete {
			patch.Title = optOf(title)
		}
		if startTerm.Set {
			// Only admins reach this handler; officers never edit their own start term.
			if !before.ProfileComplete {
				return conflict(before.Name + " hasn't filled in their profile yet, so there's no start term to change.")
			}
			var term *string
			if startTerm.V != "" {
				term = &startTerm.V
			}
			if err := q.SetStartTerm(ctx, before.ID, term); err != nil {
				return err
			}
		}
		if err := q.PatchProfile(ctx, before.ID, patch); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		out, err = c.loadOfficer(ctx, q, before.ID, true)
		if err != nil {
			return err
		}
		// Only the editable fields: the full DTO carries phone, which stays out of the log.
		snap := func(o *OfficerDTO) map[string]any {
			var team *string
			if o.Subteam != nil {
				team = &o.Subteam.ID
			}
			return map[string]any{"subteamId": team, "title": o.Title, "startTerm": o.StartTerm}
		}
		return c.audit(ctx, q, "officer.updated", "user", before.ID, snap(before), snap(out))
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"officer": out})
	return nil
}

// ─── my profile ───

// MyProfile is the signed-in officer's own profile, phone included.
type MyProfile struct {
	Subteam              *SubteamDTO `json:"subteam"`
	Phone                string      `json:"phone"`
	Birthday             string      `json:"birthday"`
	PreferredName        *string     `json:"preferredName"`
	Pronouns             *string     `json:"pronouns"`
	PersonalEmail        *string     `json:"personalEmail"`
	SchoolEmail          *string     `json:"schoolEmail"`
	GradDate             *string     `json:"gradDate"`
	Title                *string     `json:"title"`
	StartTerm            *string     `json:"startTerm"`
	Team                 *string     `json:"team"`
	LinkedIn             *string     `json:"linkedin"`
	CalendarDeliveryMode string      `json:"calendarDeliveryMode"`
}

func myProfile(p *store.Profile, people *peopleIndex) *MyProfile {
	if p == nil {
		return nil
	}
	return &MyProfile{Subteam: people.subteam(p.SubteamID), Phone: p.Phone, Birthday: p.Birthday, PreferredName: p.PreferredName, Pronouns: p.Pronouns,
		PersonalEmail: p.PersonalEmail, SchoolEmail: p.SchoolEmail, GradDate: p.GradDate, Title: p.Title, StartTerm: p.StartTerm, Team: p.Team, LinkedIn: p.LinkedIn,
		CalendarDeliveryMode: p.DeliveryMode}
}

func (c *Ctx) profileResponse(ctx context.Context, q *store.Queries, userID string) (map[string]any, error) {
	p, err := q.ProfileByUser(ctx, userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	people, err := loadPeople(ctx, q)
	if err != nil {
		return nil, err
	}
	teams, err := q.Subteams(ctx)
	if err != nil {
		return nil, err
	}
	// Invitees and anyone with a profile already have a subteam; others pick one once.
	subteamLocked := p != nil
	if inv, err := q.InviteForUser(ctx, userID); err == nil && p == nil {
		subteamLocked = true
		p0 := people.subteam(inv.SubteamID)
		return map[string]any{"profile": nil, "subteams": teams, "subteamLocked": subteamLocked, "defaultSubteam": p0}, nil
	}
	return map[string]any{"profile": myProfile(p, people), "subteams": teams, "subteamLocked": subteamLocked, "defaultSubteam": nil}, nil
}

func (c *Ctx) profileGet() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	resp, err := c.profileResponse(c.Context(), c.S.db.Read(), u.ID)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, resp)
	return nil
}

var (
	phoneCharsRE = regexp.MustCompile(`^[+0-9 ().\-]+$`)
	monthDayRE   = regexp.MustCompile(`^(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$`)
	gradDateRE   = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
)

// IsMonthDay accepts MM-DD for a real calendar day, Feb 29 included.
func isMonthDay(s string) bool {
	if !monthDayRE.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", "2000-"+s) // 2000 is a leap year
	return err == nil
}

func isLinkedIn(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "linkedin.com" || strings.HasSuffix(h, ".linkedin.com")
}

func (c *Ctx) profilePut() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var p store.Profile
	var subteam, mode validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		phone := r.Str("phone", validate.S{Req: true, Trim: true, Min: 1, Max: 32})
		if phone.Has() {
			digits := 0
			for _, ch := range phone.V {
				if ch >= '0' && ch <= '9' {
					digits++
				}
			}
			if !phoneCharsRE.MatchString(phone.V) || digits < 7 || digits > 15 {
				r.Issues.Add("phone", "Enter a phone number with at least 7 digits")
			}
		}
		p.Phone = phone.V
		bday := r.Str("birthday", validate.S{Req: true, Trim: true})
		if bday.Has() && !isMonthDay(bday.V) {
			r.Issues.Add("birthday", "Enter a month and day, like 04-21")
		}
		p.Birthday = bday.V
		opt := func(key string, s validate.S) *string {
			s.Null, s.Trim = true, true
			if s.Max == 0 {
				s.Max = 100
			}
			f := r.Str(key, s)
			if !f.Has() || f.V == "" {
				return nil
			}
			return &f.V
		}
		p.PreferredName = opt("preferredName", validate.S{Max: 60})
		p.Pronouns = opt("pronouns", validate.S{Max: 40})
		p.PersonalEmail = opt("personalEmail", validate.S{Format: "email", Max: 254})
		p.SchoolEmail = opt("schoolEmail", validate.S{Format: "email", Max: 254})
		p.GradDate = opt("gradDate", validate.S{Max: 7})
		if p.GradDate != nil && !gradDateRE.MatchString(*p.GradDate) {
			r.Issues.Add("gradDate", "Enter a month, like 2028-05")
		}
		p.Title = opt("title", validate.S{Max: 100})
		p.Team = opt("team", validate.S{Max: 60})
		p.LinkedIn = opt("linkedin", validate.S{Max: 300})
		if p.LinkedIn != nil && !isLinkedIn(*p.LinkedIn) {
			r.Issues.Add("linkedin", "Use your https://linkedin.com/in/… address")
		}
		subteam = r.Str("subteamId", validate.S{Min: 1, Max: 64})
		mode = r.Str("calendarDeliveryMode", validate.S{Enum: []string{"ics", "google"}})
	}); err != nil {
		return err
	}
	if p.PersonalEmail != nil {
		v := strings.ToLower(*p.PersonalEmail)
		p.PersonalEmail = &v
	}
	if p.SchoolEmail != nil {
		v := strings.ToLower(*p.SchoolEmail)
		p.SchoolEmail = &v
	}
	p.UserID = u.ID
	ctx := c.Context()
	var resp map[string]any
	created := false
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		before, err := q.ProfileByUser(ctx, u.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		created = before == nil
		inv, invErr := q.InviteForUser(ctx, u.ID)
		onboarding := invErr == nil && inv.OnboardedAt == nil
		switch {
		case onboarding:
			// During setup the invite holds the subteam; an inviter's fix moves it.
			if subteam.Set && subteam.V != inv.SubteamID {
				return forbidden("Your subteam was set by your invite. Ask your inviter to change it.")
			}
			p.SubteamID, p.DeliveryMode = inv.SubteamID, "ics"
			if before != nil {
				p.DeliveryMode = before.DeliveryMode
			}
		case before != nil:
			if subteam.Set && subteam.V != before.SubteamID {
				return forbidden("Ask a director to change your subteam")
			}
			p.SubteamID, p.DeliveryMode = before.SubteamID, before.DeliveryMode
		default:
			if invErr == nil {
				if subteam.Set && subteam.V != inv.SubteamID {
					return forbidden("Your subteam was set by your invite. Ask your inviter to change it.")
				}
				p.SubteamID = inv.SubteamID
			} else if subteam.Set {
				ok, err := q.SubteamExists(ctx, subteam.V)
				if err != nil {
					return err
				}
				if !ok {
					issues := &validate.Issues{}
					issues.Add("subteamId", "Subteam not found")
					return validationError(issues)
				}
				p.SubteamID = subteam.V
			} else {
				issues := &validate.Issues{}
				issues.Add("subteamId", "Choose your subteam")
				return validationError(issues)
			}
			p.DeliveryMode = "ics"
		}
		if mode.Set {
			if mode.V == "google" {
				if _, err := q.GoogleTokenFor(ctx, u.ID); err != nil {
					return conflict("Connect Google Calendar before choosing Google sync")
				}
			}
			p.DeliveryMode = mode.V
		}
		// The start term is never typed by the officer: a new profile gets the current term, and
		// an existing one keeps its value (admins change it from the officer view).
		if before != nil {
			p.StartTerm = before.StartTerm
		} else {
			t := currentTerm(c.S.now())
			p.StartTerm = &t
		}
		if err := q.SaveProfile(ctx, p); err != nil {
			return err
		}
		// The profile title is the officer's display title too.
		if err := q.UpdateUser(ctx, u.ID, store.UserPatch{Title: store.Opt[string]{Set: true, Null: p.Title == nil, V: deref(p.Title)}}, c.S.nowMs()); err != nil {
			return err
		}
		if err := c.audit(ctx, q, "profile.updated", "officer_profile", u.ID, redactProfile(before), redactProfile(&p)); err != nil {
			return err
		}
		resp, err = c.profileResponse(ctx, q, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	if created {
		c.S.backfillCalendar(u.ID) // they just became a calendar recipient
	}
	writeJSON(c.W, 200, resp)
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// redactProfile keeps phone out of the audit log, which every director can read in full.
func redactProfile(p *store.Profile) any {
	if p == nil {
		return nil
	}
	cp := *p
	if cp.Phone != "" {
		cp.Phone = "[redacted]"
	}
	return cp
}

// ─── onboarding ───

// OnboardingVersion is the current intro + intake flow. Accounts below it (every existing
// account when it shipped, and every new one) go through the flow once; see docs/onboarding.md.
const OnboardingVersion = 9

// currentTerm names the academic term a date falls in, like "Fall 2026".
func currentTerm(t time.Time) string {
	switch m := t.UTC().Month(); {
	case m <= time.May:
		return fmt.Sprintf("Spring %d", t.UTC().Year())
	case m <= time.August:
		return fmt.Sprintf("Summer %d", t.UTC().Year())
	default:
		return fmt.Sprintf("Fall %d", t.UTC().Year())
	}
}

// OnboardingState is present on /api/me while the account still has the intro + intake ahead.
type OnboardingState struct {
	Step      int          `json:"step"`
	Name      string       `json:"name"`
	Subteam   *SubteamDTO  `json:"subteam"`
	InvitedBy *UserSummary `json:"invitedBy"`
	// InviterDiscordID lets the client open a DM with the inviter for corrections.
	InviterDiscordID *string `json:"inviterDiscordId"`
	HasProfile       bool    `json:"hasProfile"`
	CorrectionsByDM  bool    `json:"correctionsByDm"`
	// CorrectionPending: they reported a problem with these details and the inviter
	// hasn't edited the invite since.
	CorrectionPending bool `json:"correctionPending"`
	// Returning: an existing account seeing the flow once (it has no unfinished invite).
	Returning bool `json:"returning"`
	// StartTerm is the term the profile has (or will get), shown read-only.
	StartTerm string `json:"startTerm"`
}

func (s *Server) onboardingState(ctx context.Context, q *store.Queries, userID string) (*OnboardingState, error) {
	version, step, err := q.OnboardingProgress(ctx, userID)
	if err != nil {
		return nil, err
	}
	inv, err := q.InviteForUser(ctx, userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	invited := err == nil
	invPending := invited && inv.OnboardedAt == nil
	if version >= OnboardingVersion && !invPending {
		return nil, nil
	}
	people, err := loadPeople(ctx, q)
	if err != nil {
		return nil, err
	}
	if step < 1 || step > 5 {
		step = 1
	}
	st := &OnboardingState{Step: step, CorrectionsByDM: s.cfg.DM != nil, Returning: !invPending, StartTerm: currentTerm(s.now())}
	if u, ok := people.users[userID]; ok {
		st.Name = u.Name
	}
	if invited {
		st.Subteam = people.subteam(inv.SubteamID)
		if u, ok := people.users[inv.InvitedBy]; ok {
			sum := userSummary(u)
			st.InvitedBy = &sum
			d := u.DiscordID
			st.InviterDiscordID = &d
		}
		latest, err := q.LatestAuditAction(ctx, inv.ID, "onboarding.correction", "invite.updated")
		if err != nil {
			return nil, err
		}
		st.CorrectionPending = latest == "onboarding.correction"
	}
	if p, err := q.ProfileByUser(ctx, userID); err == nil {
		st.HasProfile = true
		st.Subteam = people.subteam(p.SubteamID)
		if p.StartTerm != nil {
			st.StartTerm = *p.StartTerm
		}
	}
	return st, nil
}

// onboardingInvite is the unfinished invite, for corrections (only invitees send them).
func (c *Ctx) onboardingInvite(ctx context.Context, q *store.Queries) (*store.Invite, error) {
	inv, err := q.InviteForUser(ctx, c.User.ID)
	if err != nil || inv.OnboardedAt != nil {
		return nil, conflict("You've already finished setup")
	}
	return inv, nil
}

// onboardingSave records the step to resume at ("Finish later" and each step change).
func (c *Ctx) onboardingSave() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var step int64
	if err := c.jsonBody(func(r *validate.Reader) {
		step = r.Int("step", validate.I{Req: true, Min: validate.Lo(1), Max: validate.Lo(5)}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	var st *OnboardingState
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		cur, err := c.S.onboardingState(ctx, q, c.User.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return conflict("You've already finished setup")
		}
		if err := q.SetUserOnboardingStep(ctx, c.User.ID, int(step)); err != nil {
			return err
		}
		st, err = c.S.onboardingState(ctx, q, c.User.ID)
		return err
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"onboarding": st})
	return nil
}

// onboardingComplete finishes setup once the required profile is saved. Repeating it
// after success is harmless.
func (c *Ctx) onboardingComplete() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		version, _, err := q.OnboardingProgress(ctx, u.ID)
		if err != nil {
			return err
		}
		inv, invErr := q.InviteForUser(ctx, u.ID)
		invPending := invErr == nil && inv.OnboardedAt == nil
		if version >= OnboardingVersion && !invPending {
			return nil
		}
		if _, err := q.ProfileByUser(ctx, u.ID); err != nil {
			return conflict("Add your phone and birthday before finishing")
		}
		if err := q.SetOnboardingVersion(ctx, u.ID, OnboardingVersion); err != nil {
			return err
		}
		if invPending {
			if err := q.MarkOnboarded(ctx, inv.ID, c.S.nowMs()); err != nil {
				return err
			}
			return c.audit(ctx, q, "onboarding.completed", "invite", inv.ID, nil, map[string]any{"userId": u.ID})
		}
		return c.audit(ctx, q, "onboarding.completed", "user", u.ID, nil, map[string]any{"version": OnboardingVersion})
	})
	if err != nil {
		return err
	}
	c.S.backfillCalendar(u.ID)
	writeJSON(c.W, 200, map[string]any{"user": currentUser(u), "onboarding": nil})
	return nil
}

// onboardingCorrection sends "my name/subteam is wrong" to the inviter over Discord.
//
// The bot DMs a real person, so it allows one correction a minute and five delivered DMs
// per invite. The slot is reserved (an onboarding.correction audit row) inside one write
// transaction before the DM goes out, so parallel requests can't all pass the check. All
// reservations count toward the five unless their result shows no DM was delivered
// (failed, not_configured, skipped), so a DM still in flight holds its slot.
func (c *Ctx) onboardingCorrection() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var message string
	if err := c.jsonBody(func(r *validate.Reader) {
		message = r.Str("message", validate.S{Req: true, Trim: true, Min: 1, Max: 500}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	var inv *store.Invite
	var inviter *store.User
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		var err error
		if inv, err = c.onboardingInvite(ctx, q); err != nil {
			return err
		}
		_, recent, err := q.AuditCountSince(ctx, "onboarding.correction", inv.ID, c.S.nowMs()-60_000)
		if err != nil {
			return err
		}
		// Every reservation counts unless its result says no DM was delivered, so a DM
		// still in flight (or one whose result write failed) holds its slot.
		reserved, _, err := q.AuditCountSince(ctx, "onboarding.correction", inv.ID, 0)
		if err != nil {
			return err
		}
		undelivered := 0
		for _, a := range []string{"onboarding.correction.failed", "onboarding.correction.not_configured", "onboarding.correction.skipped"} {
			n, _, err := q.AuditCountSince(ctx, a, inv.ID, 0)
			if err != nil {
				return err
			}
			undelivered += n
		}
		if reserved-undelivered >= 5 {
			return conflict("You've sent five corrections already. Message your inviter on Discord directly.")
		}
		if recent > 0 {
			return conflict("Give your inviter a minute before sending another correction.")
		}
		if inviter, err = q.UserByID(ctx, inv.InvitedBy); err != nil {
			return notFoundErr("Your inviter's account is gone. Ask leadership on Discord.")
		}
		return c.audit(ctx, q, "onboarding.correction", "invite", inv.ID, nil, map[string]any{"message": message})
	})
	if err != nil {
		return err
	}
	dm := DMResult{Status: "not_configured"}
	switch {
	case !inviter.Active:
		msg := inviter.Name + " no longer has a dashboard account. Ask leadership on Discord to fix your details."
		dm = DMResult{Status: "skipped", Message: &msg}
	case c.S.cfg.DM != nil:
		content := fmt.Sprintf("%s (Discord %s) is setting up their CSEED dashboard account and says their invite details need a fix:\n\n> %s\n\nYou invited them as \"%s\". Update it on the Officers page.",
			neutralizeDM(u.Name), u.DiscordID, strings.ReplaceAll(neutralizeDM(message), "\n", "\n> "), neutralizeDM(inv.Name))
		// Bounded well under the one-minute cooldown.
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.S.cfg.DM.SendDM(sendCtx, inviter.DiscordID, content)
		cancel()
		if err != nil {
			msg := "Discord didn't deliver it. Message " + inviter.Name + " on Discord directly."
			var de *notify.DMError
			if errors.As(err, &de) && de.Code == 50007 {
				msg = inviter.Name + " doesn't accept DMs from the bot. Message them on Discord directly."
			}
			dm = DMResult{Status: "failed", Message: &msg}
		} else {
			at := isoMs(c.S.nowMs())
			dm = DMResult{Status: "sent", SentAt: &at}
		}
	}
	wctx := context.WithoutCancel(ctx)
	err = c.S.db.Write(wctx, func(w *store.Queries) error {
		return c.audit(wctx, w, "onboarding.correction."+dm.Status, "invite", inv.ID, nil, map[string]any{"dm": dm.Status})
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"dm": dm, "inviter": userSummary(inviter), "inviterDiscordId": inviter.DiscordID})
	return nil
}

var (
	dmSchemeRE   = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*)://`)
	dmMarkdownRE = regexp.MustCompile("([\\\\*_~`|>\\[\\]()#<])")
)

// neutralizeDM makes relayed text inert in Discord: URL schemes are broken so nothing
// links or embeds, markdown characters are escaped, and @ mentions can't ping.
func neutralizeDM(s string) string {
	s = dmSchemeRE.ReplaceAllString(s, "$1[:]//")
	s = dmMarkdownRE.ReplaceAllString(s, `\$1`)
	return strings.ReplaceAll(s, "@", "@\u200b")
}
