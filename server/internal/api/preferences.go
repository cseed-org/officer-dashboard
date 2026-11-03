package api

import (
	"encoding/json"
	"errors"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// Keep defaults aligned with the web widget registry (web/src/ui/widgets/registry.ts). Unknown
// IDs remain valid so later feature Panes can register widgets without a database migration.
// Sizes are W×H Home grid blocks; the Links default uses a compact two-block footprint.
var defaultWidgets = []store.WidgetPlacement{
	{ID: "links", Size: "2x1"},
	{ID: "kpi-upcoming", Size: "1x1"},
	{ID: "kpi-checkin", Size: "1x1"},
	{ID: "kpi-reflection", Size: "1x1"},
	{ID: "kpi-budget", Size: "1x1"},
	{ID: "kpi-pending", Size: "1x1"},
	{ID: "web-analytics", Size: "1x1"},
	{ID: "calendar", Size: "4x2"},
	{ID: "recent-events", Size: "2x2"},
	{ID: "budget", Size: "2x2"},
	{ID: "my-action", Size: "2x1"},
	{ID: "activity", Size: "2x1"},
}

// widgetSizes lists V0.4 block sizes, then the V0.3 S/M/L sizes. Layouts saved before V0.4 keep
// S/M/L until Home automatically saves the converted layout on load; an old open tab
// can still save.
var widgetSizes = []string{"1x1", "2x1", "1x2", "2x2", "4x1", "4x2", "S", "M", "L"}

func (c *Ctx) prefsGet() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	return c.writePrefs(u.ID)
}
func (c *Ctx) writePrefs(userID string) error {
	q, ctx := c.S.db.Read(), c.Context()
	raw, err := q.WidgetLayout(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		b, _ := json.Marshal(defaultWidgets)
		raw = string(b)
	} else if err != nil {
		return err
	}
	pref, err := q.TimezonePref(ctx, userID)
	if err != nil {
		return err
	}
	zone, err := q.EffectiveTimezone(ctx, userID)
	if err != nil {
		return err
	}
	tutorialSeen, err := q.DashboardTutorialSeen(ctx, userID)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"dashboardTutorialSeen": tutorialSeen, "widgetLayout": json.RawMessage(raw), "timezoneMode": pref.Mode, "timezone": pref.Zone, "effectiveTimezone": zone.String()})
	return nil
}
func (c *Ctx) prefsPut() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	layout := []store.WidgetPlacement{}
	seen := map[string]bool{}
	var hasLayout bool
	var tutorialSeen validate.Field[bool]
	var mode, zone validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		tutorialSeen = r.BoolField("dashboardTutorialSeen")
		hasLayout = r.Has("widgetLayout")
		r.Objects("widgetLayout", false, 50, func(_ int, sub *validate.Reader) {
			id := sub.Str("id", validate.S{Req: true, Min: 1, Max: 64}).V
			size := sub.Str("size", validate.S{Req: true, Enum: widgetSizes}).V
			if seen[id] {
				r.Issues.Add("widgetLayout", "Widget IDs must be unique")
			}
			seen[id] = true
			layout = append(layout, store.WidgetPlacement{ID: id, Size: size})
		})
		mode = r.Str("timezoneMode", validate.S{Enum: []string{"auto", "club", "manual"}})
		zone = r.Str("timezone", validate.S{Max: 100})
		if zone.Set && zone.V != "" && !validTimezone(zone.V) {
			r.Issues.Add("timezone", "Choose an IANA time zone")
		}
		if mode.Set && mode.V == "manual" && (!zone.Set || zone.V == "") {
			r.Issues.Add("timezone", "Choose a manual time zone")
		}
		if zone.Set && !mode.Set {
			r.Issues.Add("timezoneMode", "Specify the time zone mode")
		}
		if !hasLayout && !mode.Set && !tutorialSeen.Set {
			r.Issues.AddForm("No preferences supplied")
		}
	}); err != nil {
		return err
	}
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		before, e := q.WidgetLayout(ctx, u.ID)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return e
		}
		prior, e := q.TimezonePref(ctx, u.ID)
		if e != nil {
			return e
		}
		// A timezone-only update must retain the registry's default widget layout.
		if hasLayout || before == "" {
			widgets := layout
			if !hasLayout {
				widgets = defaultWidgets
			}
			raw, _ := json.Marshal(widgets)
			if err := q.SaveWidgetLayout(ctx, u.ID, string(raw), c.S.nowMs()); err != nil {
				return err
			}
		}
		if tutorialSeen.Set {
			if err := q.SaveDashboardTutorialSeen(ctx, u.ID, tutorialSeen.V); err != nil {
				return err
			}
		}
		after := prior
		if mode.Set {
			after = store.TimezonePref{Mode: mode.V, Zone: zone.V}
			if err := q.SaveTimezonePref(ctx, u.ID, after, c.S.nowMs()); err != nil {
				return err
			}
		}
		saved, err := q.WidgetLayout(ctx, u.ID)
		if err != nil {
			return err
		}
		var priorLayout any
		if before != "" {
			priorLayout = json.RawMessage(before)
		}
		return c.audit(ctx, q, "prefs.updated", "user_prefs", u.ID, map[string]any{"widgetLayout": priorLayout, "timezone": prior}, map[string]any{"widgetLayout": json.RawMessage(saved), "timezone": after})
	})
	if err != nil {
		return err
	}
	return c.writePrefs(u.ID)
}
func (c *Ctx) calendarEmailsList() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	emails, err := c.S.db.Read().CalendarEmails(c.Context(), u.ID)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"emails": emails})
	return nil
}
func (c *Ctx) calendarEmailCreate() error { return c.calendarEmailMutate(true, false) }
func (c *Ctx) calendarEmailUpdate() error { return c.calendarEmailMutate(false, false) }
func (c *Ctx) calendarEmailDelete() error { return c.calendarEmailMutate(false, true) }
func (c *Ctx) calendarEmailMutate(create, remove bool) error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var email validate.Field[string]
	var active validate.Field[bool]
	if !remove {
		if err := c.jsonBody(func(r *validate.Reader) {
			email = r.Str("email", validate.S{Req: create, Trim: true, Format: "email", Max: 254})
			active = r.BoolField("active")
		}); err != nil {
			return err
		}
	}
	ctx := c.Context()
	var out store.CalendarEmail
	wasActive := false
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		emails, err := q.CalendarEmails(ctx, u.ID)
		if err != nil {
			return err
		}
		var before *store.CalendarEmail
		if create {
			if len(emails) >= 5 {
				return conflict("You can save at most 5 calendar emails")
			}
			out = store.CalendarEmail{ID: auth.UUID(), Active: len(emails) == 0}
		} else {
			for i := range emails {
				if emails[i].ID == c.R.PathValue("id") {
					before = &emails[i]
					out = emails[i]
					break
				}
			}
			if before == nil {
				return notFoundErr("Calendar email not found")
			}
			wasActive = before.Active
		}
		if email.Set {
			out.Email = strings.ToLower(email.V)
		}
		if active.Set {
			out.Active = active.V
		}
		// A nonempty collection always has one active email. Switch by selecting
		// another email, rather than leaving the calendar picker without a choice.
		if before != nil && before.Active && !out.Active && !remove {
			return badRequest("Select another calendar email to make active")
		}
		action := "calendar_email.updated"
		if remove {
			if err := q.DeleteCalendarEmail(ctx, u.ID, out.ID); err != nil {
				return err
			}
			if before.Active {
				for _, e := range emails {
					if e.ID != out.ID {
						e.Active = true
						if err := q.SaveCalendarEmail(ctx, u.ID, e, false); err != nil {
							return err
						}
						break
					}
				}
			}
			action = "calendar_email.deleted"
		} else {
			if len(emails) == 0 {
				out.Active = true
			}
			if err := q.SaveCalendarEmail(ctx, u.ID, out, create); err != nil {
				return err
			}
			if create {
				action = "calendar_email.created"
			}
		}
		var after any = out
		if remove {
			after = nil
		}
		// Include the whole set: selecting or deleting an active email also changes
		// another row, and that change belongs in the same audit record.
		updated, err := q.CalendarEmails(ctx, u.ID)
		if err != nil {
			return err
		}
		return c.audit(ctx, q, action, "calendar_email", out.ID, map[string]any{"emails": emails}, map[string]any{"email": after, "emails": updated})
	})
	if err != nil {
		return err
	}
	// A new active address should receive the upcoming events it hasn't had yet.
	if (remove && wasActive) || out.Active {
		c.S.backfillCalendar(u.ID)
	}
	if remove {
		writeJSON(c.W, 200, map[string]any{"ok": true})
	} else {
		writeJSON(c.W, 200, map[string]any{"email": out})
	}
	return nil
}
