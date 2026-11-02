package store

import (
	"context"
	"errors"
	"time"
	_ "time/tzdata"
)

const DefaultTimezone = "America/Los_Angeles"

type ClubSettings struct {
	ClubTimezone string `json:"clubTimezone"`
	Location     string `json:"location"`
}
type TimezonePref struct {
	Mode string `json:"timezoneMode"`
	Zone string `json:"timezone"`
}

func (q *Queries) ClubSettings(ctx context.Context) (ClubSettings, error) {
	var s ClubSettings
	err := q.q.QueryRowContext(ctx, `SELECT club_timezone,location FROM club_settings WHERE id=1`).Scan(&s.ClubTimezone, &s.Location)
	return s, err
}
func (q *Queries) SaveClubSettings(ctx context.Context, s ClubSettings) error {
	return q.Exec(ctx, `UPDATE club_settings SET club_timezone=?,location=? WHERE id=1`, s.ClubTimezone, s.Location)
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
