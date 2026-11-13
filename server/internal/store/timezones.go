package store

import (
	"context"
	"errors"
	"time"
	_ "time/tzdata"
)

const DefaultTimezone = "America/Los_Angeles"

const DefaultWebsiteURL = "https://cseed.co"
const DefaultNotionURL = "https://app.notion.com/p/cseed/cseed-8c884a7f9dbe4bffa58718820e00cc10"
const DefaultDriveURL = "https://drive.google.com/drive/folders/0ABwfOblCw9EvUk9PVA?dmr=1&ec=wgc-drive-hero-goto"

type ClubSettings struct {
	WebsiteURL   string `json:"websiteUrl"`
	NotionURL    string `json:"notionUrl"`
	DriveURL     string `json:"driveUrl"`
	ClubTimezone string `json:"clubTimezone"`
	Location     string `json:"location"`
}

// Blank settings use the club-supplied defaults; leadership can replace each URL.
func (s ClubSettings) WithDefaultLinks() ClubSettings {
	if s.WebsiteURL == "" {
		s.WebsiteURL = DefaultWebsiteURL
	}
	if s.NotionURL == "" {
		s.NotionURL = DefaultNotionURL
	}
	if s.DriveURL == "" {
		s.DriveURL = DefaultDriveURL
	}
	return s
}

type TimezonePref struct {
	Mode string `json:"timezoneMode"`
	Zone string `json:"timezone"`
}

func (q *Queries) ClubSettings(ctx context.Context) (ClubSettings, error) {
	var s ClubSettings
	err := q.q.QueryRowContext(ctx, `SELECT club_timezone,location,website_url,notion_url,drive_url FROM club_settings WHERE id=1`).Scan(&s.ClubTimezone, &s.Location, &s.WebsiteURL, &s.NotionURL, &s.DriveURL)
	return s.WithDefaultLinks(), err
}
func (q *Queries) SaveClubSettings(ctx context.Context, s ClubSettings) error {
	return q.Exec(ctx, `UPDATE club_settings SET club_timezone=?,location=?,website_url=?,notion_url=?,drive_url=? WHERE id=1`, s.ClubTimezone, s.Location, s.WebsiteURL, s.NotionURL, s.DriveURL)
}
func (q *Queries) TimezonePref(ctx context.Context, userID string) (TimezonePref, error) {
	p := TimezonePref{Mode: "club"}
	err := q.q.QueryRowContext(ctx, `SELECT timezone_mode,timezone FROM user_prefs WHERE user_id=?`, userID).Scan(&p.Mode, &p.Zone)
	if errors.Is(notFound(err), ErrNotFound) {
		return p, nil
	}
	return p, err
}
func (q *Queries) SaveTimezonePref(ctx context.Context, userID string, p TimezonePref, now int64) error {
	return q.Exec(ctx, `INSERT INTO user_prefs(user_id,widget_layout,updated_at,timezone_mode,timezone) VALUES(?,'[]',?,?,?) ON CONFLICT(user_id) DO UPDATE SET timezone_mode=excluded.timezone_mode,timezone=excluded.timezone,updated_at=excluded.updated_at`, userID, now, p.Mode, p.Zone)
}

// EffectiveTimezone falls back to the club location until an automatic zone is detected.
func (q *Queries) EffectiveTimezone(ctx context.Context, userID string) (*time.Location, error) {
	s, err := q.ClubSettings(ctx)
	if err != nil {
		return nil, err
	}
	p, err := q.TimezonePref(ctx, userID)
	if err != nil {
		return nil, err
	}
	zone := s.ClubTimezone
	if p.Mode != "club" && p.Zone != "" {
		zone = p.Zone
	}
	return time.LoadLocation(zone)
}
