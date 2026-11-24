package seed

import (
	"context"
	"fmt"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// Buildspace demo records are inserted only by the explicit seed command, never by migration.
func seedBuildspace(ctx context.Context, q *store.Queries) error {
	b, err := q.Buildspace(ctx)
	if err != nil {
		return err
	}
	if len(b.Projects) > 0 {
		return nil
	}
	// Cohorts: the current intake, plus an archived earlier one (read-only history).
	for _, co := range []store.LaunchpadCohort{
		{ID: "cohort-fall-2026", Name: "Fall 2026", StartsOn: "2026-10-05", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "cohort-spring-2026", Name: "Spring 2026", StartsOn: "2026-03-30", EndsOn: ptr("2026-06-05"), Status: "active", CreatedAt: now - 1, UpdatedAt: now},
	} {
		if err := q.SaveLaunchpadCohort(ctx, co); err != nil {
			return err
		}
	}
	mentors := []struct{ id, name, org, topics, email string }{{"bs-mentor-maya", "Maya Chen", "Microsoft", "[\"Product engineering\",\"Accessibility\"]", "maya.chen@example.org"}, {"bs-mentor-jordan", "Jordan Ellis", "UW CoMotion", "[\"Prototyping\",\"Hardware\"]", "jordan.ellis@example.org"}, {"bs-mentor-amara", "Amara Okafor", "Independent designer", "[\"Storytelling\",\"Creative coding\"]", "amara@example.org"}}
	for _, m := range mentors {
		if err := insert(ctx, q, "contacts", "id,name,org,role,type,topics,email,linkedin,notes,created_at,updated_at", m.id, m.name, m.org, "Buildspace + Launchpad mentor", "mentor", m.topics, m.email, "", "", now, now); err != nil {
			return err
		}
	}
	// Roster settings: Maya caps at three projects, Amara at two; Jordan has no cap.
	for _, ms := range []struct {
		id  string
		cap *int
	}{{"bs-mentor-maya", intPtr(3)}, {"bs-mentor-jordan", nil}, {"bs-mentor-amara", intPtr(2)}} {
		if err := q.SaveLaunchpadMentorSettings(ctx, ms.id, true, ms.cap, now); err != nil {
			return err
		}
	}
	projects := []store.BuildspaceProject{
		{ID: "bs-moss", Name: "Moss", Description: "A calmer way to find study partners. Matching students by working style, with accessibility built in from day one.", Track: "Software", Stage: "Building", Links: []string{"https://github.com/topics/study-buddy"}, MentorIDs: []string{"bs-mentor-maya"}},
		{ID: "bs-sprout", Name: "Sprout sensor", Description: "A low-cost soil monitor that helps the campus garden use less water. First outdoor prototype is in testing.", Track: "Hardware", Stage: "Demo-ready", Links: []string{}, MentorIDs: []string{"bs-mentor-jordan", "bs-mentor-maya"}},
		{ID: "bs-afterimage", Name: "Afterimage", Description: "An interactive zine collecting the small stories of a changing campus. Print meets generative art.", Track: "Creative", Stage: "Building", Links: []string{}, MentorIDs: []string{"bs-mentor-amara"}},
		{ID: "bs-wayfinder", Name: "Wayfinder XR", Description: "Rehearse your first day on campus in an immersive, low-pressure environment. Looking for help testing headset navigation.", Track: "Extended Virtual Reality (XR)", Stage: "At risk", Links: []string{}, MentorIDs: []string{}},
		{ID: "bs-pantry", Name: "Open pantry", Description: "A community map of free food and mutual aid resources, kept current by students.", Track: "Software", Stage: "Shipped", Links: []string{}, MentorIDs: []string{"bs-mentor-maya"}},
		{ID: "bs-common", Name: "Common ground", Description: "A participatory sound installation about the places we share. Exploring the first listening session.", Track: "Creative", Stage: "Ideating", Links: []string{}, MentorIDs: []string{"bs-mentor-amara"}},
	}
	for _, p := range projects {
		p.UpdatedAt, p.CohortID = now, "cohort-fall-2026"
		if err := q.SaveBuildspaceProject(ctx, p); err != nil {
			return err
		}
	}
	names := []string{"Alex Kim", "Priya Shah", "Sam Rivera", "Noor Hassan", "Ava Brooks", "Leo Park", "Jamie Wu", "Morgan Lee", "Riley Chen", "Zoe Patel", "Eli Nguyen", "Casey Tran"}
	projectIDs := []string{"bs-moss", "bs-moss", "bs-sprout", "bs-sprout", "bs-afterimage", "bs-afterimage", "bs-wayfinder", "bs-wayfinder", "bs-pantry", "bs-pantry", "bs-common", "bs-common"}
	// Demo entries land on the first day of the current check-in week (club time zone,
	// configured due day), so they age naturally like real check-ins.
	clock, err := q.BuildspaceClock(ctx, time.Now())
	if err != nil {
		return err
	}
	rules, err := q.BuildspaceWeekRules(ctx)
	if err != nil {
		return err
	}
	week := store.BuildspaceWeekOf(rules, clock.Today).Start
	for i, name := range names {
		id := fmt.Sprintf("bs-person-%02d", i+1)
		discord := fmt.Sprintf("1000000000000000%02d", i+1)
		if err := q.SaveBuildspacePerson(ctx, store.BuildspacePerson{ID: id, Name: name, DiscordID: &discord, ProjectID: projectIDs[i], JoinedOn: "2026-10-05", CohortID: "cohort-fall-2026",
			Designation: []string{"Founder", "Engineering lead", "Design lead", "Hardware lead"}[i%4]}); err != nil {
			return err
		}
		if i%3 != 0 {
			if err := q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: id + "-weekly", ProjectID: projectIDs[i], PersonID: &id, Kind: "weekly", Date: week, Body: []string{"Shipped the first prototype and got feedback from three students. Next: simplify the first-run experience.", "Made progress on the core interaction. Pairing with my partner this week to test the tricky edge cases."}[i%2], Status: "on-track", Source: "discord", Visibility: "team", CreatedAt: now}); err != nil {
				return err
			}
		}
	}
	activities := []store.BuildspaceActivity{
		{ID: "bs-a1", ProjectID: "bs-moss", Kind: "mentor", MentorID: &mentors[0].id, Date: week, Body: "Keep the matching flow to three questions. Bring a clickable prototype to our next session.", Status: "on-track", Visibility: "team"},
		{ID: "bs-a2", ProjectID: "bs-wayfinder", Kind: "request", Date: week, Body: "Need access to an XR headset for user testing before demo night.", Status: "open"},
		{ID: "bs-a3", ProjectID: "bs-sprout", Kind: "purchase", Date: week, Body: "Starter kits for the soil sensor prototype.", Status: "resolved", PurchaseID: ptr("req-0002")},
		{ID: "bs-a4", ProjectID: "bs-afterimage", Kind: "purchase", Date: week, Body: "Printing for the demo night display.", Status: "open", PurchaseID: ptr("req-0005")},
		{ID: "bs-a5", ProjectID: "bs-wayfinder", PersonID: ptr("bs-person-07"), Kind: "absence", Date: week, Body: "Away for a family commitment; partner will share this week's update.", Status: "resolved", Visibility: "team"},
		{ID: "bs-a6", ProjectID: "bs-common", Kind: "request", Date: week, Body: "Looking for a quiet room to record the first listening session.", Status: "open"},
	}
	for _, a := range activities {
		a.Source = "manual"
		a.CreatedAt = now
		if err := q.InsertBuildspaceActivity(ctx, a); err != nil {
			return err
		}
	}
	return seedLaunchpad(ctx, q)
}

// SeededInviteToken is the demo invite link token of a pending builder invite (seed data only).
const SeededInviteToken = "demo-launchpad-invite-sky"

// seedLaunchpad adds the archived spring cohort, portal accounts (an onboarded builder, an
// active mentor) and two pending builder invites. Demo Discord IDs never match real accounts.
func seedLaunchpad(ctx context.Context, q *store.Queries) error {
	lumen := store.BuildspaceProject{ID: "bs-lumen", Name: "Lumen", Description: "Study-room occupancy lights from the spring cohort.", Track: "Hardware", Stage: "Shipped",
		Links: []string{}, MentorIDs: []string{"bs-mentor-jordan"}, UpdatedAt: now, CohortID: "cohort-spring-2026"}
	if err := q.SaveBuildspaceProject(ctx, lumen); err != nil {
		return err
	}
	if err := q.SaveBuildspacePerson(ctx, store.BuildspacePerson{ID: "bs-person-13", Name: "Rowan Diaz", ProjectID: "bs-lumen", JoinedOn: "2026-03-30", CohortID: "cohort-spring-2026", Designation: "Founder"}); err != nil {
		return err
	}
	if err := q.Exec(ctx, `UPDATE buildspace_launchpad_cohorts SET status='archived' WHERE id='cohort-spring-2026'`); err != nil {
		return err
	}
	if err := q.Exec(ctx, `UPDATE buildspace_launchpad_people SET email='alex.kim@example.org', pronouns='they/them', bio='Building calmer study tools.' WHERE id='bs-person-01'`); err != nil {
		return err
	}
	fall := "cohort-fall-2026"
	expires := now + 365*24*3600*1000 // demo invites stay usable for local development
	accounts := []store.LaunchpadAccount{
		{ID: "lp-account-alex", Kind: "builder", Email: "alex.kim@example.org", Name: "Alex Kim", Status: "active", CohortID: &fall, PersonID: ptr("bs-person-01"),
			DiscordID: ptr("100000000000000001"), AcceptedAt: &now, OnboardedAt: &now, CreatedAt: now, UpdatedAt: now},
		{ID: "lp-account-maya", Kind: "mentor", Email: "maya.chen@example.org", Name: "Maya Chen", Status: "active", ContactID: ptr("bs-mentor-maya"),
			DiscordID: ptr("200000000000000001"), AcceptedAt: &now, CreatedAt: now, UpdatedAt: now},
		{ID: "lp-invite-sky", Kind: "builder", Email: "sky.morales@example.org", Name: "Sky Morales", Status: "invited", CohortID: &fall,
			InviteTokenHash: ptr(auth.SHA256Hex(SeededInviteToken)), InviteExpiresAt: &expires, CreatedAt: now, UpdatedAt: now},
		{ID: "lp-invite-dev", Kind: "builder", Email: "dev.patel@example.org", Name: "", Status: "invited", CohortID: &fall,
			InviteTokenHash: ptr(auth.SHA256Hex("demo-launchpad-invite-dev")), InviteExpiresAt: &expires, InviteError: ptr("SMTP is not configured"), CreatedAt: now, UpdatedAt: now},
	}
	for _, a := range accounts {
		if err := q.SaveLaunchpadAccount(ctx, a); err != nil {
			return err
		}
	}
	return nil
}
func ptr(s string) *string { return &s }
func intPtr(n int) *int    { return &n }
