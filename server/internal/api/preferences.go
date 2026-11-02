package api

import (
	"encoding/json"
	"errors"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// Keep defaults aligned with the web widget registry. Unknown IDs remain valid so
// later feature Panes can register widgets without a database migration.
var defaultWidgets = []store.WidgetPlacement{{ID: "kpis", Size: "L"}, {ID: "my-action", Size: "M"}, {ID: "recent-events", Size: "M"}, {ID: "budget", Size: "M"}, {ID: "activity", Size: "M"}, {ID: "web-analytics", Size: "L"}}

func (c *Ctx) prefsGet() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	raw, err := c.S.db.Read().WidgetLayout(c.Context(), u.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(c.W, 200, map[string]any{"widgetLayout": defaultWidgets})
		return nil
	}
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"widgetLayout": json.RawMessage(raw)})
	return nil
}
func (c *Ctx) prefsPut() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	layout := []store.WidgetPlacement{}
	seen := map[string]bool{}
	if err := c.jsonBody(func(r *validate.Reader) {
		r.Objects("widgetLayout", true, 50, func(_ int, sub *validate.Reader) {
			id := sub.Str("id", validate.S{Req: true, Min: 1, Max: 64}).V
			size := sub.Str("size", validate.S{Req: true, Enum: []string{"S", "M", "L"}}).V
			if seen[id] {
				r.Issues.Add("widgetLayout", "Widget IDs must be unique")
			}
			seen[id] = true
			layout = append(layout, store.WidgetPlacement{ID: id, Size: size})
		})
	}); err != nil {
		return err
	}
	raw, _ := json.Marshal(layout)
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		before, e := q.WidgetLayout(ctx, u.ID)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return e
		}
		if e := q.SaveWidgetLayout(ctx, u.ID, string(raw), c.S.nowMs()); e != nil {
			return e
		}
		var prior any
		if before != "" {
			prior = json.RawMessage(before)
		}
		return c.audit(ctx, q, "prefs.updated", "user_prefs", u.ID, prior, layout)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"widgetLayout": layout})
	return nil
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
	if remove {
		writeJSON(c.W, 200, map[string]any{"ok": true})
	} else {
		writeJSON(c.W, 200, map[string]any{"email": out})
	}
	return nil
}
