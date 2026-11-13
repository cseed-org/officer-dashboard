package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"

	"officerdashboard/internal/auth"
)

// CalendarItem retains date-only deadlines separately from event instants.
type CalendarItem struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Date     string  `json:"date"`
	StartsAt *string `json:"startsAt"`
	EndsAt   *string `json:"endsAt"`
	Location *string `json:"location"`
	Href     string  `json:"href"`
	LumaURL  *string `json:"lumaUrl"`
	// Detail is an optional one-line summary for the item's popover (for example
	// "$45.00 · approved"). Loaders must only put viewer-visible data here.
	Detail *string `json:"detail"`
}

// CalendarRange carries both UTC boundaries and local dates. Loaders must scope
// personal data to UserID and return only items overlapping this exclusive range.
type CalendarRange struct {
	Start, End, FirstDate, EndDate, UserID string
}

// CalendarSource is the extension point for new dated sources. Register loaders here
// at startup; metadata drives the widget's source controls without a UI source switch.
type CalendarSource struct {
	DisabledByDefault bool `json:"-"`
	// DefaultRole, when set, turns the source on by default only for accounts holding
	// at least this role (auth.HasRole); everyone else can still opt in.
	DefaultRole string                                                                 `json:"-"`
	ID          string                                                                 `json:"id"`
	Label       string                                                                 `json:"label"`
	Icon        string                                                                 `json:"icon"`
	Hue         string                                                                 `json:"hue"`
	Load        func(context.Context, *Queries, CalendarRange) ([]CalendarItem, error) `json:"-"`
}

var CalendarSources = []CalendarSource{
	{ID: "events", Label: "Club events", Icon: "calendar", Hue: "blue", Load: loadCalendarEvents},
	{ID: "design", Label: "Design deadlines", Icon: "pen", Hue: "amber", Load: loadCalendarDesign},
	{ID: "purchase-deadlines", Label: "Purchase deadlines", Icon: "wallet", Hue: "green", DefaultRole: auth.RoleDirector, Load: loadPurchaseDeadlines},
	{ID: "buildspace-absences", Label: "Buildspace + Launchpad absences", Icon: "user", Hue: "coral", DisabledByDefault: true, Load: loadBuildspaceAbsences},
	{ID: "buildspace-weekly", Label: "Buildspace + Launchpad weekly check-ins", Icon: "check", Hue: "teal", DisabledByDefault: true, Load: loadBuildspaceWeeklyDue},
	{ID: "buildspace-mentor", Label: "Buildspace + Launchpad mentor check-ins", Icon: "user", Hue: "violet", DisabledByDefault: true, Load: loadBuildspaceMentorCheckIns},
}

// DefaultCalendarSourceIDs lists the sources a new account with this role sees.
func DefaultCalendarSourceIDs(role string) []string {
	ids := []string{}
	for _, source := range CalendarSources {
		if !source.DisabledByDefault && (source.DefaultRole == "" || auth.HasRole(role, source.DefaultRole)) {
			ids = append(ids, source.ID)
		}
	}
	return ids
}

func CalendarSourceIDs() []string {
	ids := make([]string, 0, len(CalendarSources))
	for _, source := range CalendarSources {
		ids = append(ids, source.ID)
	}
	return ids
}

func (q *Queries) CalendarItems(ctx context.Context, window CalendarRange, selected map[string]bool) ([]CalendarItem, error) {
	out := []CalendarItem{}
	for _, source := range CalendarSources {
		if !selected[source.ID] {
			continue
		}
		items, err := source.Load(ctx, q, window)
		if err != nil {
			return nil, err
		}
		for i := range items {
			items[i].Source = source.ID
		}
		out = append(out, items...)
	}
	return out, nil
}
func loadCalendarEvents(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,name,starts_at,ends_at,luma_url,status,location FROM events
 WHERE julianday(starts_at)<julianday(?) AND
 (julianday(starts_at)>=julianday(?) OR julianday(ends_at)>julianday(?))
 ORDER BY julianday(starts_at),id`, window.End, window.Start, window.Start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarItem{}
	for rows.Next() {
		var v CalendarItem
		if err := rows.Scan(&v.ID, &v.Title, &v.StartsAt, &v.EndsAt, &v.LumaURL, &v.Type, &v.Location); err != nil {
			return nil, err
		}
		v.Href = "/events?id=" + url.QueryEscape(v.ID)
		out = append(out, v)
	}
	return out, rows.Err()
}
func loadCalendarDesign(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,title,due_date,type FROM design_requests WHERE due_date>=? AND due_date<? AND stage<>'delivered' ORDER BY due_date,id`, window.FirstDate, window.EndDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarItem{}
	for rows.Next() {
		var v CalendarItem
		if err := rows.Scan(&v.ID, &v.Title, &v.Date, &v.Type); err != nil {
			return nil, err
		}
		v.Href = "/design?id=" + url.QueryEscape(v.ID)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (q *Queries) CalendarWidgetSettings(ctx context.Context, userID string) (string, error) {
	var raw string
	err := q.q.QueryRowContext(ctx, `SELECT settings FROM calendar_widget_settings WHERE user_id=?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return raw, err
}
func (q *Queries) SaveCalendarWidgetSettings(ctx context.Context, userID, raw string, now int64) error {
	return q.Exec(ctx, `INSERT INTO calendar_widget_settings(user_id,settings,updated_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET settings=excluded.settings,updated_at=excluded.updated_at`, userID, raw, now)
}
