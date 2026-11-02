package api

import (
	"errors"
	"net/url"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
	"strings"
	"time"
	"unicode/utf8"
)

func (c *Ctx) contactsList() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var text, kind, topic string
	if err := c.query(func(r *validate.Reader) {
		text = r.Str("q", validate.S{Trim: true, Max: 100}).V
		kind = r.Str("type", validate.S{Enum: []string{"speaker", "mentor", "both"}}).V
		topic = r.Str("topic", validate.S{Trim: true, Max: 100}).V
	}); err != nil {
		return err
	}
	rows, err := c.S.db.Read().Contacts(c.Context())
	if err != nil {
		return err
	}
	items := []store.Contact{}
	for _, v := range rows {
		if kind != "" && v.Type != kind && v.Type != "both" {
			continue
		}
		if topic != "" {
			found := false
			for _, t := range v.Topics {
				if store.Fold(t) == store.Fold(topic) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if text != "" && !strings.Contains(store.Fold(strings.Join([]string{v.Name, v.Org, v.Role, v.Email, strings.Join(v.Topics, " ")}, " ")), store.Fold(text)) {
			continue
		}
		items = append(items, v)
	}
	writeJSON(c.W, 200, map[string]any{"items": items})
	return nil
}
func (c *Ctx) contactDetail() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	v, err := c.S.db.Read().ContactByID(c.Context(), c.R.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		return notFoundErr("Contact not found")
	}
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"contact": v})
	return nil
}
func (c *Ctx) contactCreate() error { return c.contactSave(true) }
func (c *Ctx) contactUpdate() error { return c.contactSave(false) }
func (c *Ctx) contactSave(create bool) error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var name, org, role, kind, email, linkedin, notes validate.Field[string]
	var topics, eventIDs []string
	var topicsSet, eventsSet bool
	if err := c.jsonBody(func(r *validate.Reader) {
		name = r.Str("name", validate.S{Req: create, Trim: true, Min: 1, Max: 200})
		org = r.Str("org", validate.S{Trim: true, Max: 200})
		role = r.Str("role", validate.S{Trim: true, Max: 200})
		kind = r.Str("type", validate.S{Req: create, Enum: []string{"speaker", "mentor", "both"}})
		email = r.Str("email", validate.S{Trim: true, Max: 254})
		linkedin = r.Str("linkedin", validate.S{Trim: true, Max: 2000})
		notes = r.Str("notes", validate.S{Max: 10000})
		if email.V != "" {
			if !validate.IsEmail(email.V) {
				r.Issues.Add("email", "Invalid email")
			}
		}
		if linkedin.V != "" {
			u, err := url.Parse(linkedin.V)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				r.Issues.Add("linkedin", "Expected an http or https URL")
			}
		}
		topicsSet = r.Raw("topics") != nil
		eventsSet = r.Raw("eventIds") != nil
		topics = r.Strings("topics", false, 0, 30, nil, 0)
		eventIDs = r.Strings("eventIds", false, 0, 100, nil, 64)
		for _, t := range topics {
			if utf8.RuneCountInString(strings.TrimSpace(t)) > 100 {
				r.Issues.Add("topics", "Topics must be at most 100 characters")
			}
			if strings.TrimSpace(t) == "" {
				r.Issues.Add("topics", "Topics cannot be blank")
			}
		}
	}); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	if create {
		id = auth.UUID()
	}
	var out *store.Contact
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		v := store.Contact{ID: id, Topics: []string{}, CreatedAt: c.S.nowMs()}
		var before any
		if !create {
			old, err := q.ContactByID(ctx, id)
			if errors.Is(err, store.ErrNotFound) {
				return notFoundErr("Contact not found")
			}
			if err != nil {
				return err
			}
			v = *old
			before = *old
		}
		for _, x := range []struct {
			f validate.Field[string]
			p *string
		}{{name, &v.Name}, {org, &v.Org}, {role, &v.Role}, {kind, &v.Type}, {email, &v.Email}, {linkedin, &v.LinkedIn}, {notes, &v.Notes}} {
			if x.f.Set {
				*x.p = x.f.V
			}
		}
		if topicsSet {
			v.Topics = []string{}
			seen := map[string]bool{}
			for _, t := range topics {
				t = strings.TrimSpace(t)
				if !seen[store.Fold(t)] {
					v.Topics = append(v.Topics, t)
					seen[store.Fold(t)] = true
				}
			}
		}
		v.UpdatedAt = c.S.nowMs()
		if eventsSet {
			seen := map[string]bool{}
			for _, eid := range eventIDs {
				if seen[eid] {
					return badRequest("Duplicate event")
				}
				seen[eid] = true
				if _, err := q.EventByID(ctx, eid); errors.Is(err, store.ErrNotFound) {
					return badRequest("Event not found")
				} else if err != nil {
					return err
				}
			}
		}
		if err := q.SaveContact(ctx, v); err != nil {
			return err
		}
		if eventsSet {
			if err := q.SetContactEvents(ctx, id, eventIDs); err != nil {
				return err
			}
		}
		var err error
		out, err = q.ContactByID(ctx, id)
		if err != nil {
			return err
		}
		action := "contact.updated"
		if create {
			action = "contact.created"
		}
		return c.audit(ctx, q, action, "contact", id, before, out)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"contact": out})
	return nil
}
func (c *Ctx) contactDelete() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		v, err := q.ContactByID(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return notFoundErr("Contact not found")
		}
		if err != nil {
			return err
		}
		if err := q.DeleteContact(ctx, id); err != nil {
			return err
		}
		return c.audit(ctx, q, "contact.deleted", "contact", id, v, nil)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}
func (c *Ctx) contactOutreachCreate() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var o store.ContactOutreach
	if err := c.jsonBody(func(r *validate.Reader) {
		o.Date = r.Str("date", validate.S{Req: true, Format: "datetime"}).V
		o.Channel = r.Str("channel", validate.S{Req: true, Enum: []string{"email", "linkedin", "discord", "phone", "in_person"}}).V
		o.Outcome = r.Str("outcome", validate.S{Req: true, Enum: []string{"awaiting_reply", "interested", "confirmed", "declined", "follow_up"}}).V
		o.Note = r.Str("note", validate.S{Max: 10000}).V
	}); err != nil {
		return err
	}
	// Normalize offsets so lexical ordering reflects the actual instant, not input timezone.
	t, err := time.Parse(time.RFC3339Nano, o.Date)
	if err != nil {
		return badRequest("Invalid outreach date")
	}
	o.Date = t.UTC().Format("2006-01-02T15:04:05.000000000Z")
	o.ID = auth.UUID()
	o.OfficerID = u.ID
	o.OfficerName = u.Name
	o.CreatedAt = c.S.nowMs()
	ctx := c.Context()
	id := c.R.PathValue("id")
	var out *store.Contact
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		if _, err := q.ContactByID(ctx, id); errors.Is(err, store.ErrNotFound) {
			return notFoundErr("Contact not found")
		} else if err != nil {
			return err
		}
		if err := q.InsertContactOutreach(ctx, id, o); err != nil {
			return err
		}
		if err := c.audit(ctx, q, "contact.outreach_logged", "contact", id, nil, o); err != nil {
			return err
		}
		var err error
		out, err = q.ContactByID(ctx, id)
		return err
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"contact": out})
	return nil
}
