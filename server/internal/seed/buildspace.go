package seed

import (
	"context"
	"fmt"
	"time"

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
	mentors := []struct{ id, name, org, topics string }{{"bs-mentor-maya", "Maya Chen", "Microsoft", "[\"Product engineering\",\"Accessibility\"]"}, {"bs-mentor-jordan", "Jordan Ellis", "UW CoMotion", "[\"Prototyping\",\"Hardware\"]"}, {"bs-mentor-amara", "Amara Okafor", "Independent designer", "[\"Storytelling\",\"Creative coding\"]"}}
	for _, m := range mentors {
		if err := insert(ctx, q, "contacts", "id,name,org,role,type,topics,email,linkedin,notes,created_at,updated_at", m.id, m.name, m.org, "Buildspace mentor", "mentor", m.topics, "", "", "", now, now); err != nil {
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
		p.UpdatedAt = now
		if err := q.SaveBuildspaceProject(ctx, p); err != nil {
			return err
		}
	}
	names := []string{"Alex Kim", "Priya Shah", "Sam Rivera", "Noor Hassan", "Ava Brooks", "Leo Park", "Jamie Wu", "Morgan Lee", "Riley Chen", "Zoe Patel", "Eli Nguyen", "Casey Tran"}
	projectIDs := []string{"bs-moss", "bs-moss", "bs-sprout", "bs-sprout", "bs-afterimage", "bs-afterimage", "bs-wayfinder", "bs-wayfinder", "bs-pantry", "bs-pantry", "bs-common", "bs-common"}
	loc, _ := time.LoadLocation("America/Los_Angeles")
	today := time.Now().In(loc)
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7).Format("2006-01-02")
	for i, name := range names {
		id := fmt.Sprintf("bs-person-%02d", i+1)
		discord := fmt.Sprintf("1000000000000000%02d", i+1)
		if err := q.SaveBuildspacePerson(ctx, store.BuildspacePerson{ID: id, Name: name, DiscordID: &discord, ProjectID: projectIDs[i], JoinedOn: "2026-10-05"}); err != nil {
			return err
		}
		if i%3 != 0 {
			if err := q.InsertBuildspaceActivity(ctx, store.BuildspaceActivity{ID: id + "-weekly", ProjectID: projectIDs[i], PersonID: &id, Kind: "weekly", Date: week, Body: []string{"Shipped the first prototype and got feedback from three students. Next: simplify the first-run experience.", "Made progress on the core interaction. Pairing with my partner this week to test the tricky edge cases."}[i%2], Status: "on-track", Source: "discord", CreatedAt: now}); err != nil {
				return err
			}
		}
	}
	activities := []store.BuildspaceActivity{
		{ID: "bs-a1", ProjectID: "bs-moss", Kind: "mentor", MentorID: &mentors[0].id, Date: week, Body: "Keep the matching flow to three questions. Bring a clickable prototype to our next session.", Status: "on-track"},
		{ID: "bs-a2", ProjectID: "bs-wayfinder", Kind: "request", Date: week, Body: "Need access to an XR headset for user testing before demo night.", Status: "open"},
		{ID: "bs-a3", ProjectID: "bs-sprout", Kind: "purchase", Date: week, Body: "Starter kits for the soil sensor prototype.", Status: "resolved", PurchaseID: ptr("req-0002")},
		{ID: "bs-a4", ProjectID: "bs-afterimage", Kind: "purchase", Date: week, Body: "Printing for the demo night display.", Status: "open", PurchaseID: ptr("req-0005")},
		{ID: "bs-a5", ProjectID: "bs-wayfinder", PersonID: ptr("bs-person-07"), Kind: "absence", Date: week, Body: "Away for a family commitment; partner will share this week's update.", Status: "resolved"},
		{ID: "bs-a6", ProjectID: "bs-common", Kind: "request", Date: week, Body: "Looking for a quiet room to record the first listening session.", Status: "open"},
	}
	for _, a := range activities {
		a.Source = "manual"
		a.CreatedAt = now
		if err := q.InsertBuildspaceActivity(ctx, a); err != nil {
			return err
		}
	}
	return nil
}
func ptr(s string) *string { return &s }
