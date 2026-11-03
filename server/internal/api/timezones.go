package api

import (
	"context"
	"net/url"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
	"strings"
	"time"
)

func validTimezone(zone string) bool {
	if zone == "Local" || (zone != "UTC" && !strings.Contains(zone, "/")) {
		return false
	}
	_, err := time.LoadLocation(zone)
	return err == nil
}
func (c *Ctx) clubSettingsGet() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	s, err := c.S.db.Read().ClubSettings(c.Context())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, s)
	return nil
}
func (c *Ctx) clubSettingsPut() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	var s store.ClubSettings
	var website, notion, drive validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		website = r.Str("websiteUrl", validate.S{Max: 2048, Trim: true})
		notion = r.Str("notionUrl", validate.S{Max: 2048, Trim: true})
		drive = r.Str("driveUrl", validate.S{Max: 2048, Trim: true})
		for key, field := range map[string]validate.Field[string]{"websiteUrl": website, "notionUrl": notion, "driveUrl": drive} {
			if field.V != "" {
				u, err := url.Parse(field.V)
				if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
					r.Issues.Add(key, "Use an http or https URL")
				}
			}
		}
		s.ClubTimezone = r.Str("clubTimezone", validate.S{Req: true, Max: 100}).V
		s.Location = r.Str("location", validate.S{Req: true, Trim: true, Min: 1, Max: 200}).V
		if !validTimezone(s.ClubTimezone) {
			r.Issues.Add("clubTimezone", "Choose an IANA time zone")
		}
	}); err != nil {
		return err
	}
	ctx := c.Context()
	if err := c.S.db.Write(ctx, func(q *store.Queries) error {
		if _, err := c.requireLeadershipIn(q); err != nil {
			return err
		}
		prior, err := q.ClubSettings(ctx)
		if err != nil {
			return err
		}
		s.WebsiteURL, s.NotionURL, s.DriveURL = prior.WebsiteURL, prior.NotionURL, prior.DriveURL
		if website.Set {
			s.WebsiteURL = website.V
		}
		if notion.Set {
			s.NotionURL = notion.V
		}
		if drive.Set {
			s.DriveURL = drive.V
		}
		if err = q.SaveClubSettings(ctx, s); err != nil {
			return err
		}
		return c.audit(ctx, q, "club_settings.updated", "club_settings", "1", prior, s)
	}); err != nil {
		return err
	}
	writeJSON(c.W, 200, s)
	return nil
}

// Delivery and shared drafts use the same persisted zone as the dashboard.
func (s *Server) effectiveZone(ctx context.Context, userID string) *time.Location {
	zone, err := s.db.Read().EffectiveTimezone(ctx, userID)
	if err == nil {
		return zone
	}
	zone, _ = time.LoadLocation(store.DefaultTimezone)
	return zone
}
