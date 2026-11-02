package api

import (
	"slices"
	"sort"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// V0.4 Design page: the events being planned and whether each still needs a design.
// The indicator derives from events.design_need plus the event's linked design requests:
//   - any linked request still open      → requested (with those requests' stages)
//   - linked requests, all delivered     → done
//   - no requests, design_need not_needed → not-needed
//   - otherwise (unknown or needed)      → needs-design

// designHorizon is how far ahead the Design page looks for events being planned.
const designHorizon = 42 * 24 * time.Hour

const skipDesignDenied = "Only a director or the event's host can mark an event as not needing design"

// DesignIndicator values (DesignIndicator in src/core/contracts.ts).
const (
	indicatorNeeds     = "needs-design"
	indicatorRequested = "requested"
	indicatorNotNeeded = "not-needed"
	indicatorDone      = "done"
)

type DesignEventRequest struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Stage    string       `json:"stage"`
	Assignee *UserSummary `json:"assignee"`
}

type DesignUpcomingEvent struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	StartsAt    string               `json:"startsAt"`
	Status      string               `json:"status"`
	Host        *UserSummary         `json:"host"`
	HostSubteam *SubteamBrief        `json:"hostSubteam"`
	DesignNeed  string               `json:"designNeed"`
	Indicator   string               `json:"indicator"`
	Requests    []DesignEventRequest `json:"requests"`
	// CanSetNotNeeded: director+ or the event's host may rule the event out of design.
	CanSetNotNeeded bool `json:"canSetNotNeeded"`
}

// canSkipDesign is who may set design_need to not_needed: director+ or the event's host.
func canSkipDesign(u *store.User, hostID *string) bool {
	return u != nil && (auth.HasRole(u.Role, auth.RoleDirector) || (hostID != nil && *hostID == u.ID))
}

func designIndicator(need string, reqs []DesignEventRequest) string {
	if len(reqs) > 0 {
		for _, r := range reqs {
			if r.Stage != "delivered" {
				return indicatorRequested
			}
		}
		return indicatorDone
	}
	if need == "not_needed" {
		return indicatorNotNeeded
	}
	return indicatorNeeds
}

// upcomingEvent builds the rail entry for e from the requests linked to it.
func (d *designCtx) upcomingEvent(e *store.Event, linked []store.DesignRequest) DesignUpcomingEvent {
	it := eventItem(d.cat, *e, nil)
	out := DesignUpcomingEvent{ID: e.ID, Name: e.Name, StartsAt: e.StartsAt, Status: e.Status, Host: it.Host, HostSubteam: it.HostSubteam,
		DesignNeed: e.DesignNeed, Requests: []DesignEventRequest{}, CanSetNotNeeded: canSkipDesign(d.viewer, e.HostUserID)}
	// Open work first, then most recently created.
	sort.SliceStable(linked, func(i, j int) bool {
		a, b := linked[i].Stage == "delivered", linked[j].Stage == "delivered"
		if a != b {
			return !a
		}
		return linked[i].CreatedAt > linked[j].CreatedAt
	})
	for _, r := range linked {
		er := DesignEventRequest{ID: r.ID, Title: r.Title, Stage: r.Stage}
		if r.AssigneeID != nil {
			if u := d.cat.users[*r.AssigneeID]; u != nil {
				s := userSummary(u)
				er.Assignee = &s
			}
		}
		out.Requests = append(out.Requests, er)
	}
	out.Indicator = designIndicator(e.DesignNeed, out.Requests)
	return out
}

// upcomingEvents lists draft and published events starting between now and the horizon,
// soonest first.
func (d *designCtx) upcomingEvents(now time.Time, rows []store.DesignRequest) []DesignUpcomingEvent {
	byEvent := map[string][]store.DesignRequest{}
	for _, r := range rows {
		if r.EventID != nil {
			byEvent[*r.EventID] = append(byEvent[*r.EventID], r)
		}
	}
	type dated struct {
		t time.Time
		e *store.Event
	}
	var picked []dated
	for i := range d.cat.events {
		e := &d.cat.events[i]
		if e.Status != "draft" && e.Status != "published" {
			continue
		}
		t, err := luma.ParseInstant(e.StartsAt)
		if err != nil || t.Before(now) || t.After(now.Add(designHorizon)) {
			continue
		}
		picked = append(picked, dated{t, e})
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].t.Before(picked[j].t) })
	out := make([]DesignUpcomingEvent, 0, len(picked))
	for _, p := range picked {
		out = append(out, d.upcomingEvent(p.e, byEvent[p.e.ID]))
	}
	return out
}

// eventDesignNeed sets whether an event needs design (PUT /api/events/{id}/design-need).
// Any officer may say an event needs design; not needed is director+ or the host's call.
func (c *Ctx) eventDesignNeed() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var need string
	if err := c.jsonBody(func(r *validate.Reader) {
		need = r.Str("designNeed", validate.S{Req: true, Enum: store.DesignNeeds}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	var out DesignUpcomingEvent
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		e, err := w.EventByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		viewer, err := w.UserByID(ctx, u.ID)
		if err != nil || !viewer.Active {
			return forbidden("Account inactive")
		}
		if need == "not_needed" && e.DesignNeed != "not_needed" && !canSkipDesign(viewer, e.HostUserID) {
			return forbidden(skipDesignDenied)
		}
		if e.DesignNeed != need {
			before := eventSnapshot(e)
			if err := w.UpdateEvent(ctx, id, store.EventPatch{DesignNeed: store.Opt[string]{V: need, Set: true}}, c.S.nowMs()); err != nil {
				return err
			}
			if e, err = w.EventByID(ctx, id); err != nil {
				return err
			}
			if err := c.audit(ctx, w, "event.design_need", "event", id, before, eventSnapshot(e)); err != nil {
				return err
			}
		}
		dc, err := loadDesignCtx(ctx, w, viewer)
		if err != nil {
			return err
		}
		rows, err := w.DesignRequests(ctx)
		if err != nil {
			return err
		}
		rows = slices.DeleteFunc(rows, func(r store.DesignRequest) bool { return r.EventID == nil || *r.EventID != id })
		out = dc.upcomingEvent(e, rows)
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"event": out})
	return nil
}
