package api

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

type calendarWidgetOptions struct {
	View         string   `json:"view"`
	Sources      []string `json:"sources"`
	ColourBy     string   `json:"colourBy"`
	ShowWeekends bool     `json:"showWeekends"`
}

func defaultCalendarOptions(role string) calendarWidgetOptions {
	return calendarWidgetOptions{"auto", store.DefaultCalendarSourceIDs(role), "source", true}
}
func (c *Ctx) calendarWidgetGet() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	raw, err := c.S.db.Read().CalendarWidgetSettings(c.Context(), u.ID)
	if err != nil {
		return err
	}
	out := defaultCalendarOptions(u.Role)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return err
		}
	}
	writeJSON(c.W, 200, out)
	return nil
}
func (c *Ctx) calendarWidgetPut() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	out := defaultCalendarOptions(u.Role)
	if err := c.jsonBody(func(r *validate.Reader) {
		out.View = r.Str("view", validate.S{Req: true, Enum: []string{"auto", "agenda", "week", "month"}}).V
		out.ColourBy = r.Str("colourBy", validate.S{Req: true, Enum: []string{"source", "type"}}).V
		// Booleans are required for replacement settings.
		b := r.BoolField("showWeekends")
		out.ShowWeekends = b.V
		if !b.Set {
			r.Issues.Add("showWeekends", "Required")
		}
		if r.Has("sources") {
			out.Sources = r.Strings("sources", true, 0, 32, store.CalendarSourceIDs(), 64)
			if out.Sources == nil {
				out.Sources = []string{}
			}
		} else {
			// Legacy clients may still send the original two source flags.
			events, design := r.BoolField("events"), r.BoolField("design")
			if !events.Set || !design.Set {
				r.Issues.Add("sources", "Choose sources")
			}
			out.Sources = []string{}
			if events.V {
				out.Sources = append(out.Sources, "events")
			}
			if design.V {
				out.Sources = append(out.Sources, "design")
			}
		}
	}); err != nil {
		return err
	}
	raw, _ := json.Marshal(out)
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		before, err := q.CalendarWidgetSettings(c.Context(), u.ID)
		if err != nil {
			return err
		}
		if err := q.SaveCalendarWidgetSettings(c.Context(), u.ID, string(raw), c.S.nowMs()); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "calendar_widget.updated", "user", u.ID, before, out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, out)
	return nil
}

// Dates are interpreted on the officer's selected wall clock; the end is exclusive.
// AddDate, rather than 24-hour arithmetic, preserves DST boundaries.
func (c *Ctx) calendarFeed() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	zone, err := c.S.db.Read().EffectiveTimezone(c.Context(), u.ID)
	if err != nil {
		return err
	}
	query := c.R.URL.Query()
	start, e1 := time.ParseInLocation("2006-01-02", query.Get("start"), zone)
	end, e2 := time.ParseInLocation("2006-01-02", query.Get("end"), zone)
	if e1 != nil || e2 != nil || !end.After(start) || end.After(start.AddDate(0, 0, 93)) {
		return badRequest("Choose a date range of 1–93 days (YYYY-MM-DD, exclusive end)")
	}
	sources := query.Get("sources")
	selected := map[string]bool{}
	known := map[string]bool{}
	for _, source := range store.CalendarSources {
		known[source.ID] = true
	}
	if !query.Has("sources") {
		for _, id := range store.DefaultCalendarSourceIDs(u.Role) {
			selected[id] = true
		}
	} else if sources != "" && sources != "none" {
		for _, id := range strings.Split(sources, ",") {
			if !known[id] {
				return badRequest("Unknown calendar source")
			}
			selected[id] = true
		}
	}
	items, err := c.S.db.Read().CalendarItems(c.Context(), store.CalendarRange{
		Start: start.UTC().Format(time.RFC3339), End: end.UTC().Format(time.RFC3339),
		FirstDate: query.Get("start"), EndDate: query.Get("end"), UserID: u.ID,
	}, selected)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].StartsAt != nil {
			t, err := time.Parse(time.RFC3339, *items[i].StartsAt)
			if err != nil {
				return err
			}
			items[i].Date = t.In(zone).Format("2006-01-02")
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date < items[j].Date
		}
		a, b := "", ""
		if items[i].StartsAt != nil {
			a = *items[i].StartsAt
		}
		if items[j].StartsAt != nil {
			b = *items[j].StartsAt
		}
		if a != "" && b != "" {
			ta, _ := time.Parse(time.RFC3339, a)
			tb, _ := time.Parse(time.RFC3339, b)
			if !ta.Equal(tb) {
				return ta.Before(tb)
			}
		} else if a != b {
			return a < b
		}
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		return items[i].ID < items[j].ID
	})
	c.W.Header().Set("Cache-Control", "private, no-store")
	writeJSON(c.W, 200, map[string]any{"items": items, "timezone": zone.String(), "sources": store.CalendarSources})
	return nil
}
