package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/files"
	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/notion"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

const csvMaxBytes = 5 * 1024 * 1024

func (c *Ctx) eventsList() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var when string
	var qtext, from, to validate.Field[string]
	var cursor string
	var limit int
	if err := c.query(func(r *validate.Reader) {
		cursor, limit = pageQuery(r)
		when = r.Str("when", validate.S{Enum: []string{"upcoming", "past", "all"}, HasDef: true, Default: "all"}).V
		qtext = r.Str("q", validate.S{Max: 100})
		from = r.Str("from", validate.S{Format: "date"})
		to = r.Str("to", validate.S{Format: "date"})
	}); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		return err
	}
	now := c.S.now()
	needle := strings.ToLower(qtext.V)
	var rows []store.Event
	for _, e := range cat.events {
		t, err := luma.ParseInstant(e.StartsAt)
		if err != nil {
			continue
		}
		switch when {
		case "past":
			if !t.Before(now) {
				continue
			}
		case "upcoming":
			if t.Before(now) {
				continue
			}
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		day := fiscal.PacificDate(t)
		if from.V != "" && day < from.V || to.V != "" && day > to.V {
			continue
		}
		rows = append(rows, e)
	}
	items, err := c.S.eventItems(ctx, q, cat, rows, "")
	if err != nil {
		return err
	}
	byID := map[string]store.Event{}
	for _, e := range rows {
		byID[e.ID] = e
	}
	p := paginate(items, func(it EventListItem) string { return eventKey(byID[it.ID]) }, cursor, limit)
	writeJSON(c.W, 200, p)
	return nil
}

type eventInput struct {
	Name            validate.Field[string]
	StartsAt        validate.Field[string]
	EndsAt          validate.Field[string]
	Location        validate.Field[string]
	URL             validate.Field[string]
	LumaID          validate.Field[string]
	HostUserID      validate.Field[string]
	Status          validate.Field[string]
	Description     validate.Field[string]
	Capacity        validate.Field[int64]
	HostSubteamID   validate.Field[string]
	LumaURL         validate.Field[string]
	DesignRequestID validate.Field[string]
}

func readEvent(r *validate.Reader, create bool) eventInput {
	in := eventInput{
		Name:            r.Str("name", validate.S{Req: create, Trim: true, Min: 1, Max: 200}),
		StartsAt:        r.Str("startsAt", validate.S{Req: create, Format: "datetime"}),
		EndsAt:          r.Str("endsAt", validate.S{Null: true, Format: "datetime"}),
		Location:        r.Str("location", validate.S{Null: true, Max: 200}),
		URL:             r.Str("url", validate.S{Null: true, Format: "url"}),
		LumaID:          r.Str("lumaId", validate.S{Null: true, Max: 100}),
		HostUserID:      r.Str("hostUserId", validate.S{Null: true, Min: 1, Max: 64}),
		Status:          r.Str("status", validate.S{Enum: store.EventStatuses, HasDef: create, Default: "draft"}),
		Description:     r.Str("description", validate.S{Null: true, Max: 5000}),
		Capacity:        r.Int("capacity", validate.I{Null: true, Min: validate.Lo(1), Max: validate.Lo(100000)}),
		HostSubteamID:   r.Str("hostSubteamId", validate.S{Null: true, Min: 1, Max: 64}),
		LumaURL:         r.Str("lumaUrl", validate.S{Null: true, Max: 300}),
		DesignRequestID: r.Str("designRequestId", validate.S{Null: true, Min: 1, Max: 64}),
	}
	if in.LumaURL.Has() {
		if norm, err := luma.NormalizeEventURL(in.LumaURL.V); err != nil {
			r.Issues.Add("lumaUrl", err.Error())
		} else {
			in.LumaURL.V = norm
		}
	}
	return in
}

func endsBeforeStarts(starts, ends string) bool {
	a, err1 := luma.ParseInstant(starts)
	b, err2 := luma.ParseInstant(ends)
	return err1 == nil && err2 == nil && b.Before(a)
}

// refsOK checks the host user, the host subteam, and that no other event links the Luma page.
func (c *Ctx) refsOK(ctx context.Context, w *store.Queries, cat *catalog, in eventInput, eventID string) error {
	if in.HostUserID.Has() {
		if u := cat.users[in.HostUserID.V]; u == nil || !u.Active {
			return badRequest("Invalid host")
		}
	}
	if in.HostSubteamID.Has() && cat.teamByID[in.HostSubteamID.V] == nil {
		return badRequest("Invalid host subteam")
	}
	if in.LumaURL.Has() {
		other, err := w.EventByLumaURL(ctx, in.LumaURL.V)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if other != nil && other.ID != eventID {
			return conflict("That Luma page is already linked to " + other.Name)
		}
	}
	return nil
}

func ptrIf(f validate.Field[string]) *string {
	if !f.Has() {
		return nil
	}
	v := f.V
	return &v
}

func intPtrIf(f validate.Field[int64]) *int64 {
	if !f.Set || f.Null {
		return nil
	}
	v := f.V
	return &v
}

func notionEvent(e *store.Event) notion.Event {
	return notion.Event{ID: e.ID, Name: e.Name, StartsAt: e.StartsAt, EndsAt: e.EndsAt, Location: e.Location, Description: e.Description, LumaURL: e.LumaURL}
}

func (c *Ctx) eventCreate() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var in eventInput
	if err := c.jsonBody(func(r *validate.Reader) { in = readEvent(r, true) }); err != nil {
		return err
	}
	ctx := c.Context()
	id := auth.UUID()
	var out EventListItem
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		if err := c.refsOK(ctx, w, cat, in, id); err != nil {
			return err
		}
		if in.EndsAt.Has() && endsBeforeStarts(in.StartsAt.V, in.EndsAt.V) {
			return badRequest("Event ends before it starts")
		}
		now := store.Ms(c.S.nowMs())
		e := store.Event{ID: id, Name: in.Name.V, StartsAt: in.StartsAt.V, EndsAt: ptrIf(in.EndsAt), Location: ptrIf(in.Location), URL: ptrIf(in.URL),
			LumaID: ptrIf(in.LumaID), HostUserID: ptrIf(in.HostUserID), Status: in.Status.V, Description: ptrIf(in.Description), Capacity: intPtrIf(in.Capacity),
			HostSubteamID: ptrIf(in.HostSubteamID), LumaURL: ptrIf(in.LumaURL), DesignRequestID: ptrIf(in.DesignRequestID), CreatedAt: now, UpdatedAt: now}
		// TODO(notion): the stub only hands back a placeholder link. A real client must not
		// call Notion inside this transaction; queue the draft and store the link afterwards.
		if page, err := c.S.cfg.Notion.DraftEventPage(ctx, notionEvent(&e)); err != nil {
			c.S.log.Warn("notion draft failed", "event", id, "err", err)
		} else if page.URL != "" {
			u := page.URL
			e.NotionURL = &u
		}
		if err := w.InsertEvent(ctx, e); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "event.created", "event", id, nil, eventSnapshot(&e)); err != nil {
			return err
		}
		items, err := c.S.eventItems(ctx, w, cat, []store.Event{e}, id)
		if err != nil {
			return err
		}
		out = items[0]
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"event": out})
	return nil
}

func (c *Ctx) eventUpdate() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var in eventInput
	if err := c.jsonBody(func(r *validate.Reader) { in = readEvent(r, false) }); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	var out EventListItem
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		e, err := w.EventByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		if err := c.refsOK(ctx, w, cat, in, id); err != nil {
			return err
		}
		startsAt, endsAt := e.StartsAt, e.EndsAt
		if in.StartsAt.Has() {
			startsAt = in.StartsAt.V
		}
		if in.EndsAt.Set {
			endsAt = ptrIf(in.EndsAt)
		}
		if endsAt != nil && endsBeforeStarts(startsAt, *endsAt) {
			return badRequest("Event ends before it starts")
		}
		before := eventSnapshot(e)
		patch := store.EventPatch{Name: optOf(in.Name), StartsAt: optOf(in.StartsAt), EndsAt: optOf(in.EndsAt), Location: optOf(in.Location),
			URL: optOf(in.URL), LumaID: optOf(in.LumaID), HostUserID: optOf(in.HostUserID), Status: optOf(in.Status), Description: optOf(in.Description),
			Capacity: optOf(in.Capacity), HostSubteamID: optOf(in.HostSubteamID), LumaURL: optOf(in.LumaURL), DesignRequestID: optOf(in.DesignRequestID)}
		if err := w.UpdateEvent(ctx, id, patch, c.S.nowMs()); err != nil {
			return err
		}
		updated, err := w.EventByID(ctx, id)
		if err != nil {
			return err
		}
		if err := c.audit(ctx, w, "event.updated", "event", id, before, eventSnapshot(updated)); err != nil {
			return err
		}
		items, err := c.S.eventItems(ctx, w, cat, []store.Event{*updated}, id)
		if err != nil {
			return err
		}
		out = items[0]
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"event": out})
	return nil
}

func (c *Ctx) eventDelete() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		e, err := w.EventByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		n, err := w.EventRequestCount(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return conflict(fmt.Sprintf("%s has %d purchase request%s. Move them to another event before deleting it.", e.Name, n, map[bool]string{true: "", false: "s"}[n == 1]))
		}
		before := eventSnapshot(e)
		if err := w.DeleteEvent(ctx, id); err != nil {
			return err
		}
		return c.audit(ctx, w, "event.deleted", "event", id, before, nil)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

func (c *Ctx) eventDetail() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	e, err := q.EventByID(ctx, c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		return err
	}
	items, err := c.S.eventItems(ctx, q, cat, []store.Event{*e}, e.ID)
	if err != nil {
		return err
	}
	reqs, err := q.RequestsByEvent(ctx, e.ID)
	if err != nil {
		return err
	}
	list := []RequestListItem{}
	var spent int64
	for i := range reqs {
		it, err := requestItem(&reqs[i], cat)
		if err != nil {
			return err
		}
		list = append(list, it)
		if reqs[i].ActualTotalCents != nil {
			spent += *reqs[i].ActualTotalCents
		}
	}
	writeJSON(c.W, 200, map[string]any{
		"event": EventDetail{EventListItem: items[0], URL: e.URL, Description: e.Description, DesignRequestID: e.DesignRequestID, UpdatedAt: isoMs(int64(e.UpdatedAt)),
			Notion: NotionLink{URL: e.NotionURL, Placeholder: e.NotionURL == nil || notion.IsPlaceholder(*e.NotionURL)}},
		"requests":   list,
		"spentCents": spent,
	})
	return nil
}

// ─── cover image ───

const coverMaxBytes = 5 * 1024 * 1024

var coverMIME = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

func (c *Ctx) eventCoverPut() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	if _, err := c.S.db.Read().EventByID(ctx, id); err != nil {
		return notFoundErr("Record not found")
	}
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, coverMaxBytes+1<<20)
	if err := c.R.ParseMultipartForm(8 << 20); err != nil {
		return badRequest("Expected multipart/form-data with an image file (maximum 5 MB)")
	}
	fhs := c.R.MultipartForm.File["file"]
	if len(fhs) == 0 {
		return badRequest("Image file required")
	}
	fh := fhs[0]
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(fh.Header.Get("Content-Type"), ";", 2)[0]))
	if _, ok := coverMIME[mime]; !ok {
		return badRequest("Cover must be a JPG, PNG or WebP image")
	}
	if fh.Size == 0 || fh.Size > coverMaxBytes {
		return badRequest("Cover must be at most 5 MB")
	}
	f, err := fh.Open()
	if err != nil {
		return badRequest("Image file required")
	}
	body, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return badRequest("Image file required")
	}
	if err := files.CheckContent(mime, body); err != nil {
		return err
	}
	name := strings.TrimSpace(fh.Filename)
	if name == "" || len(name) > 200 {
		name = "cover" + coverMIME[mime]
	}
	var out EventListItem
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		e, err := w.EventByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		now := c.S.nowMs()
		if err := w.PutEventCover(ctx, store.Cover{EventID: id, MimeType: mime, Bytes: body, Filename: name, UpdatedAt: now}); err != nil {
			return err
		}
		if err := w.UpdateEvent(ctx, id, store.EventPatch{}, now); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "event.cover_set", "event", id, map[string]any{"hasCover": e.CoverUpdatedAt != nil},
			map[string]any{"hasCover": true, "mimeType": mime, "sizeBytes": len(body), "filename": name}); err != nil {
			return err
		}
		updated, err := w.EventByID(ctx, id)
		if err != nil {
			return err
		}
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		items, err := c.S.eventItems(ctx, w, cat, []store.Event{*updated}, id)
		if err != nil {
			return err
		}
		out = items[0]
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"event": out})
	return nil
}

func (c *Ctx) eventCoverDelete() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	var out EventListItem
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		if _, err := w.EventByID(ctx, id); err != nil {
			return notFoundErr("Record not found")
		}
		had, err := w.DeleteEventCover(ctx, id)
		if err != nil {
			return err
		}
		if had {
			if err := w.UpdateEvent(ctx, id, store.EventPatch{}, c.S.nowMs()); err != nil {
				return err
			}
			if err := c.audit(ctx, w, "event.cover_removed", "event", id, map[string]any{"hasCover": true}, map[string]any{"hasCover": false}); err != nil {
				return err
			}
		}
		updated, err := w.EventByID(ctx, id)
		if err != nil {
			return err
		}
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		items, err := c.S.eventItems(ctx, w, cat, []store.Event{*updated}, id)
		if err != nil {
			return err
		}
		out = items[0]
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"event": out})
	return nil
}

func (c *Ctx) eventCoverGet() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	cov, err := c.S.db.Read().EventCover(c.Context(), c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	h := c.W.Header()
	h.Set("Content-Type", cov.MimeType)
	h.Set("Content-Length", fmt.Sprint(len(cov.Bytes)))
	h.Set("Content-Disposition", "inline; filename*=UTF-8''"+encodeRFC5987(cov.Filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	c.W.WriteHeader(200)
	_, _ = c.W.Write(cov.Bytes)
	return nil
}

func (c *Ctx) eventMetrics() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	e, err := q.EventByID(ctx, c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	guests, err := q.GuestsForChart(ctx, e.ID)
	if err != nil {
		return err
	}
	times, err := q.ReflectionTimes(ctx, e.ID)
	if err != nil {
		return err
	}
	counts, err := q.EventCounts(ctx, e.ID)
	if err != nil {
		return err
	}
	start, _ := luma.ParseInstant(e.StartsAt)
	registrations, responses := map[string]int{}, map[string]int{}
	for _, g := range guests {
		if g.RegisteredAt != nil && *g.RegisteredAt <= start.UnixMilli() && luma.IsRegistered(g.ApprovalStatus, g.CheckedIn) {
			registrations[fiscal.PacificDate(time.UnixMilli(*g.RegisteredAt))]++
		}
	}
	for _, t := range times {
		responses[fiscal.PacificDate(time.UnixMilli(t))]++
	}
	type regDay struct {
		Date       string `json:"date"`
		Cumulative int    `json:"cumulative"`
	}
	type respDay struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	regDays, respDays := []regDay{}, []respDay{}
	for _, d := range sortedKeys(registrations) {
		cum := 0
		if len(regDays) > 0 {
			cum = regDays[len(regDays)-1].Cumulative
		}
		regDays = append(regDays, regDay{d, cum + registrations[d]})
	}
	for _, d := range sortedKeys(responses) {
		respDays = append(respDays, respDay{d, responses[d]})
	}
	writeJSON(c.W, 200, map[string]any{"eventId": e.ID, "totals": eventCounts(counts[e.ID]), "registrationsByDay": regDays, "reflectionsByDay": respDays})
	return nil
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ─── imports ───

// errRollback aborts a write transaction on purpose (dry runs) after the result is captured.
var errRollback = errors.New("rollback")

type importResult struct {
	EventID string      `json:"eventId"`
	Kind    *string     `json:"kind,omitempty"`
	DryRun  *bool       `json:"dryRun,omitempty"`
	Rows    int         `json:"rows"`
	Created int         `json:"created"`
	Updated int         `json:"updated"`
	Skipped int         `json:"skipped"`
	Errors  []rowError  `json:"errors"`
	Metrics EventCounts `json:"metrics"`
}

type rowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

func msOf(s *string) *int64 {
	if s == nil {
		return nil
	}
	t, err := luma.ParseInstant(*s)
	if err != nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

// applyImport upserts guests and reflections inside the caller's transaction and returns
// the created/updated counts (guests and reflections together, as v1 reported them).
func (c *Ctx) applyImport(ctx context.Context, w *store.Queries, eventID string, guests []store.GuestIn, reflections []store.ReflectionIn) (created, updated int, err error) {
	now := c.S.nowMs()
	if len(guests) > 0 {
		old, err := w.ExistingGuests(ctx, eventID)
		if err != nil {
			return 0, 0, err
		}
		for i := range guests {
			if id, ok := old[guests[i].Email]; ok {
				guests[i].ID = id
				updated++
			} else {
				guests[i].ID = auth.UUID()
				created++
			}
		}
		if err := w.UpsertGuests(ctx, eventID, guests, now); err != nil {
			return 0, 0, err
		}
	}
	if len(reflections) > 0 {
		old, err := w.ExistingReflections(ctx, eventID)
		if err != nil {
			return 0, 0, err
		}
		for i := range reflections {
			if id, ok := old[reflections[i].Email]; ok {
				reflections[i].ID = id
				updated++
			} else {
				reflections[i].ID = auth.UUID()
				created++
			}
		}
		if err := w.UpsertReflections(ctx, eventID, reflections, now); err != nil {
			return 0, 0, err
		}
	}
	return created, updated, nil
}

func (c *Ctx) eventsImport() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	r := c.R
	r.Body = http.MaxBytesReader(c.W, r.Body, csvMaxBytes+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return badRequest("Expected multipart/form-data with a CSV file")
	}
	rd := validate.FromValues(r.MultipartForm.Value)
	kind := rd.Str("kind", validate.S{Req: true, Enum: []string{"luma_guests", "reflections"}})
	eventID := rd.Str("eventId", validate.S{Min: 1, Max: 64})
	lumaEventID := rd.Str("lumaEventId", validate.S{})
	eventName := rd.Str("eventName", validate.S{})
	startsAt := rd.Str("eventStartsAt", validate.S{Format: "datetime"})
	dry := rd.Str("dryRun", validate.S{Enum: []string{"true", "false"}, HasDef: true, Default: "false"})
	if rd.Issues.Any() {
		return validationError(rd.Issues)
	}
	dryRun := dry.V == "true"
	var text string
	fhs := r.MultipartForm.File["file"]
	if len(fhs) == 0 || fhs[0].Size > csvMaxBytes {
		return badRequest("CSV file required (maximum 5 MB)")
	}
	f, err := fhs[0].Open()
	if err != nil {
		return badRequest("CSV file required (maximum 5 MB)")
	}
	raw, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return badRequest("CSV file required (maximum 5 MB)")
	}
	text = string(raw)

	var guestRows []luma.Guest
	var reflRows []luma.Reflection
	var errs []rowError
	total := 0
	if kind.V == "luma_guests" {
		res, perr := luma.ParseGuests(text)
		if perr != nil {
			return fail(422, "import_failed", perr.Error())
		}
		guestRows, total = res.Rows, res.TotalRows
		for _, e := range res.Errors {
			errs = append(errs, rowError(e))
		}
	} else {
		res, perr := luma.ParseReflections(text)
		if perr != nil {
			return fail(422, "import_failed", perr.Error())
		}
		reflRows, total = res.Rows, res.TotalRows
		for _, e := range res.Errors {
			errs = append(errs, rowError(e))
		}
	}
	if errs == nil {
		errs = []rowError{}
	}
	ctx := c.Context()
	var result importResult
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		var existing *store.Event
		var qerr error
		switch {
		case eventID.V != "":
			existing, qerr = w.EventByID(ctx, eventID.V)
		case lumaEventID.V != "":
			existing, qerr = w.EventByLumaID(ctx, lumaEventID.V)
		}
		if qerr != nil && !errors.Is(qerr, store.ErrNotFound) {
			return qerr
		}
		if eventID.V != "" && existing == nil {
			return notFoundErr("Event not found")
		}
		if existing == nil && (lumaEventID.V == "" || eventName.V == "" || startsAt.V == "") {
			return badRequest("Supply eventId or lumaEventId, eventName and eventStartsAt")
		}
		id := ""
		if existing != nil {
			id = existing.ID
		} else {
			id = auth.UUID()
			lid, name := lumaEventID.V, eventName.V
			now := store.Ms(c.S.nowMs())
			if err := w.InsertEvent(ctx, store.Event{ID: id, LumaID: &lid, Name: name, StartsAt: startsAt.V, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		// Duplicate emails collapse to the last row, keeping first-seen order.
		gm := map[string]store.GuestIn{}
		var gorder []string
		for _, g := range guestRows {
			if _, ok := gm[g.Email]; !ok {
				gorder = append(gorder, g.Email)
			}
			gm[g.Email] = store.GuestIn{Email: g.Email, Name: g.Name, LumaGuestID: g.LumaGuestID, ApprovalStatus: g.ApprovalStatus, RegisteredAt: msOf(g.RegisteredAt), CheckedInAt: msOf(g.CheckedInAt)}
		}
		guests := make([]store.GuestIn, 0, len(gorder))
		for _, e := range gorder {
			guests = append(guests, gm[e])
		}
		rm := map[string]store.ReflectionIn{}
		var rorder []string
		for _, r := range reflRows {
			if _, ok := rm[r.Email]; !ok {
				rorder = append(rorder, r.Email)
			}
			t, _ := luma.ParseInstant(r.SubmittedAt)
			rm[r.Email] = store.ReflectionIn{Email: r.Email, SubmittedAt: t.UnixMilli()}
		}
		refls := make([]store.ReflectionIn, 0, len(rorder))
		for _, e := range rorder {
			refls = append(refls, rm[e])
		}
		created, updated, err := c.applyImport(ctx, w, id, guests, refls)
		if err != nil {
			return err
		}
		if !dryRun {
			if err := c.audit(ctx, w, "event.imported", "event", id, nil, map[string]any{"kind": kind.V, "created": created, "updated": updated, "errors": errs}); err != nil {
				return err
			}
		}
		counts, err := w.EventCounts(ctx, id)
		if err != nil {
			return err
		}
		k, d := kind.V, dryRun
		result = importResult{EventID: id, Kind: &k, DryRun: &d, Rows: total, Created: created, Updated: updated, Skipped: total - created - updated, Errors: errs, Metrics: eventCounts(counts[id])}
		if dryRun {
			return errRollback
		}
		return nil
	})
	if err != nil && !errors.Is(err, errRollback) {
		return err
	}
	writeJSON(c.W, 200, result)
	return nil
}

// ingestLuma is the service-token import: the event plus its guests as JSON.
func (c *Ctx) ingestLuma() error {
	tok, err := c.authenticateToken("ingest:luma")
	if err != nil {
		return err
	}
	type guestIn struct {
		Email          string
		Name           validate.Field[string]
		LumaGuestID    validate.Field[string]
		ApprovalStatus validate.Field[string]
		RegisteredAt   validate.Field[string]
		CheckedInAt    validate.Field[string]
	}
	var ev eventInput
	var guests []guestIn
	err = c.jsonBody(func(r *validate.Reader) {
		// The nested `event` object has its own required fields.
		raw := r.Raw("event")
		if raw == nil {
			r.Issues.Add("event", "Invalid input: expected object, received undefined")
		} else {
			sub := validate.FromJSON(raw)
			sub.Issues = r.Issues
			ev.LumaID = sub.Str("lumaId", validate.S{Req: true, Min: 1})
			ev.Name = sub.Str("name", validate.S{Req: true, Min: 1})
			ev.StartsAt = sub.Str("startsAt", validate.S{Req: true, Format: "datetime"})
			ev.EndsAt = sub.Str("endsAt", validate.S{Null: true, Format: "datetime"})
			ev.Location = sub.Str("location", validate.S{Null: true})
			ev.URL = sub.Str("url", validate.S{Null: true, Format: "url"})
		}
		r.Objects("guests", true, 5000, func(_ int, sub *validate.Reader) {
			e := sub.Str("email", validate.S{Req: true, Format: "email"})
			guests = append(guests, guestIn{
				Email:          strings.ToLower(e.V),
				Name:           sub.Str("name", validate.S{Null: true}),
				LumaGuestID:    sub.Str("lumaGuestId", validate.S{Null: true}),
				ApprovalStatus: sub.Str("approvalStatus", validate.S{Null: true}),
				RegisteredAt:   sub.Str("registeredAt", validate.S{Null: true, Format: "datetime"}),
				CheckedInAt:    sub.Str("checkedInAt", validate.S{Null: true, Format: "datetime"}),
			})
		})
	})
	if err != nil {
		return err
	}
	ctx := c.Context()
	var result importResult
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		existing, qerr := w.EventByLumaID(ctx, ev.LumaID.V)
		if qerr != nil && !errors.Is(qerr, store.ErrNotFound) {
			return qerr
		}
		now := c.S.nowMs()
		var id string
		if existing != nil {
			id = existing.ID
			patch := store.EventPatch{Name: optOf(ev.Name), StartsAt: optOf(ev.StartsAt), EndsAt: optOf(ev.EndsAt), Location: optOf(ev.Location), URL: optOf(ev.URL)}
			if err := w.UpdateEvent(ctx, id, patch, now); err != nil {
				return err
			}
		} else {
			id = auth.UUID()
			lid := ev.LumaID.V
			e := store.Event{ID: id, LumaID: &lid, Name: ev.Name.V, StartsAt: ev.StartsAt.V, EndsAt: ptrIf(ev.EndsAt), Location: ptrIf(ev.Location), URL: ptrIf(ev.URL), CreatedAt: store.Ms(now), UpdatedAt: store.Ms(now)}
			if err := w.InsertEvent(ctx, e); err != nil {
				return err
			}
		}
		gm := map[string]store.GuestIn{}
		var order []string
		for _, g := range guests {
			var clear []string
			for name, f := range map[string]validate.Field[string]{"name": g.Name, "lumaGuestId": g.LumaGuestID, "approvalStatus": g.ApprovalStatus, "registeredAt": g.RegisteredAt, "checkedInAt": g.CheckedInAt} {
				if f.Null {
					clear = append(clear, name)
				}
			}
			sort.Strings(clear)
			if _, ok := gm[g.Email]; !ok {
				order = append(order, g.Email)
			}
			gm[g.Email] = store.GuestIn{Email: g.Email, Name: ptrIf(g.Name), LumaGuestID: ptrIf(g.LumaGuestID), ApprovalStatus: ptrIf(g.ApprovalStatus),
				RegisteredAt: msOf(ptrIf(g.RegisteredAt)), CheckedInAt: msOf(ptrIf(g.CheckedInAt)), Clear: clear}
		}
		list := make([]store.GuestIn, 0, len(order))
		for _, e := range order {
			list = append(list, gm[e])
		}
		created, updated, err := c.applyImport(ctx, w, id, list, nil)
		if err != nil {
			return err
		}
		if err := w.TouchToken(ctx, tok.ID, now); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "event.imported", "event", id, nil, map[string]any{"kind": "luma_guests", "created": created, "updated": updated, "errors": []rowError{}}); err != nil {
			return err
		}
		counts, err := w.EventCounts(ctx, id)
		if err != nil {
			return err
		}
		total := len(guests)
		result = importResult{EventID: id, Rows: total, Created: created, Updated: updated, Skipped: total - created - updated, Errors: []rowError{}, Metrics: eventCounts(counts[id])}
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, result)
	return nil
}
