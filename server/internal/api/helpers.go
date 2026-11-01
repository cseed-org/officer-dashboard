package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"officerdashboard/internal/accounting"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/purchases"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// catalog is the small reference data (people, chart of accounts, funds, optionally events)
// that list and detail views join against in memory.
type catalog struct {
	users     map[string]*store.User
	accounts  []accounting.Account
	acctByID  map[string]*accounting.Account
	funds     []accounting.Fund
	fundByID  map[string]*accounting.Fund
	events    []store.Event
	eventByID map[string]*store.Event
}

func loadCatalog(ctx context.Context, q *store.Queries, withEvents bool) (*catalog, error) {
	users, err := q.Users(ctx)
	if err != nil {
		return nil, err
	}
	c := &catalog{users: map[string]*store.User{}, acctByID: map[string]*accounting.Account{}, fundByID: map[string]*accounting.Fund{}, eventByID: map[string]*store.Event{}}
	for i := range users {
		c.users[users[i].ID] = &users[i]
	}
	if c.accounts, err = q.Accounts(ctx); err != nil {
		return nil, err
	}
	for i := range c.accounts {
		c.acctByID[c.accounts[i].ID] = &c.accounts[i]
	}
	if c.funds, err = q.Funds(ctx); err != nil {
		return nil, err
	}
	for i := range c.funds {
		c.fundByID[c.funds[i].ID] = &c.funds[i]
	}
	if withEvents {
		if c.events, err = q.Events(ctx); err != nil {
			return nil, err
		}
		for i := range c.events {
			c.eventByID[c.events[i].ID] = &c.events[i]
		}
	}
	return c, nil
}

func (c *catalog) user(id string) (*store.User, error) {
	u := c.users[id]
	if u == nil {
		return nil, notFoundErr("Record not found")
	}
	return u, nil
}

// audit appends an audit row attributing the change to the signed-in user or service token.
func (c *Ctx) audit(ctx context.Context, q *store.Queries, action, entityType, entityID string, before, after any) error {
	a := store.AuditEntry{ID: auth.UUID(), Action: action, EntityType: entityType, BeforeJSON: snapshot(before), AfterJSON: snapshot(after), CreatedAt: store.Ms(c.S.nowMs())}
	if entityID != "" {
		a.EntityID = &entityID
	}
	if c.User != nil {
		a.ActorUserID = &c.User.ID
	}
	if c.Token != nil {
		a.ActorToken = &c.Token.ID
	}
	return q.InsertAudit(ctx, a)
}

// paginate sorts descending by key, applies the cursor and the limit. The cursor is the
// key of the last item of the previous page, so it stays stable when newer rows arrive.
func paginate[T any](items []T, key func(T) string, cursor string, limit int) page[T] {
	sorted := append([]T(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return key(sorted[i]) > key(sorted[j]) })
	if cursor != "" {
		kept := sorted[:0:0]
		for _, it := range sorted {
			if key(it) < cursor {
				kept = append(kept, it)
			}
		}
		sorted = kept
	}
	p := page[T]{Items: []T{}}
	if len(sorted) > limit {
		last := key(sorted[limit-1])
		p.NextCursor = &last
		sorted = sorted[:limit]
	}
	p.Items = append(p.Items, sorted...)
	return p
}

// parseCursor splits "<iso>|<id>" into (ms, id).
func parseCursor(cursor string) (int64, string, error) {
	i := strings.IndexByte(cursor, '|')
	if i < 0 {
		return 0, "", badRequest("Invalid cursor")
	}
	t, err := luma.ParseInstant(cursor[:i])
	if err != nil {
		return 0, "", badRequest("Invalid cursor")
	}
	return t.UnixMilli(), cursor[i+1:], nil
}

func cursorKey(ms int64, id string) string { return isoMs(ms) + "|" + id }

// pageQuery reads ?cursor=&limit= (limit 1–200, default 50).
func pageQuery(r *validate.Reader) (cursor string, limit int) {
	cur := r.Str("cursor", validate.S{})
	lim := r.Int("limit", validate.I{Min: validate.Lo(1), Max: validate.Lo(200), HasDef: true, Def: 50})
	return cur.V, int(lim.V)
}

func (c *Ctx) query(fn func(r *validate.Reader)) error {
	r := validate.FromValues(c.R.URL.Query())
	fn(r)
	if r.Issues.Any() {
		return validationError(r.Issues)
	}
	return nil
}

// jsonBody reads and parses a JSON object body (up to 1 MB), then lets fn pull fields.
func (c *Ctx) jsonBody(fn func(r *validate.Reader)) error {
	raw, err := readBody(c, 1<<20)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 || !json.Valid(raw) {
		return badRequest("Expected a JSON body")
	}
	r := validate.FromJSON(raw)
	fn(r)
	if r.Issues.Any() {
		return validationError(r.Issues)
	}
	return nil
}

func readBody(c *Ctx, max int64) ([]byte, error) {
	b, err := io.ReadAll(http.MaxBytesReader(c.W, c.R.Body, max))
	if err != nil {
		return nil, badRequest("Request body too large")
	}
	return b, nil
}

// eventsByStart orders events newest first by start instant.
func eventKey(e store.Event) string {
	t, err := luma.ParseInstant(e.StartsAt)
	if err != nil {
		return e.StartsAt + "|" + e.ID
	}
	return isoMs(t.UnixMilli()) + "|" + e.ID
}

func (s *Server) eventItems(ctx context.Context, q *store.Queries, cat *catalog, events []store.Event, eventID string) ([]EventListItem, error) {
	counts, err := q.EventCounts(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]EventListItem, 0, len(events))
	for _, e := range events {
		out = append(out, eventItem(cat, e, counts[e.ID]))
	}
	return out, nil
}

func eventItem(cat *catalog, e store.Event, counts *store.Counts) EventListItem {
	item := EventListItem{ID: e.ID, LumaID: e.LumaID, Name: e.Name, StartsAt: e.StartsAt, Location: e.Location, Metrics: eventCounts(counts)}
	if e.HostUserID != nil {
		if u := cat.users[*e.HostUserID]; u != nil {
			s := userSummary(u)
			item.Host = &s
		}
	}
	return item
}

func requestItem(r *store.Request, cat *catalog) (RequestListItem, error) {
	req, err := cat.user(r.RequesterID)
	if err != nil {
		return RequestListItem{}, err
	}
	acct, fund := cat.acctByID[r.ExpenseAccountID], cat.fundByID[r.FundID]
	if acct == nil || fund == nil {
		return RequestListItem{}, notFoundErr("Record not found")
	}
	return RequestListItem{
		ID: r.ID, Number: r.Number, Title: r.Title, Vendor: r.Vendor, Type: r.Type, Status: r.Status, TotalCents: r.TotalCents,
		Requester: userSummary(req), ExpenseAccount: acct.Brief(), Fund: fund.Brief(),
		EventID: r.EventID, SubmittedAt: isoPtr(r.SubmittedAt), UpdatedAt: isoMs(int64(r.UpdatedAt)),
	}, nil
}

// requestCore builds the viewer-independent request detail.
func requestCore(ctx context.Context, q *store.Queries, cat *catalog, r *store.Request) (RequestCore, error) {
	item, err := requestItem(r, cat)
	if err != nil {
		return RequestCore{}, err
	}
	approvals, err := q.Approvals(ctx, r.ID)
	if err != nil {
		return RequestCore{}, err
	}
	attachments, err := q.RequestAttachments(ctx, r.ID)
	if err != nil {
		return RequestCore{}, err
	}
	entries, err := q.PostedEntryIDs(ctx, r.ID)
	if err != nil {
		return RequestCore{}, err
	}
	held, err := q.HeldJob(ctx, r.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return RequestCore{}, err
	}
	core := RequestCore{
		RequestListItem: item, ItemURL: r.ItemURL, Quantity: r.Quantity, UnitCostCents: r.UnitCostCents, TaxShipCents: r.TaxShipCents,
		ActualTotalCents: r.ActualTotalCents, FunctionalClass: r.FunctionalClass, Justification: r.Justification, NeededBy: r.NeededBy,
		DecidedAt: isoPtr(r.DecidedAt), CreatedAt: isoMs(int64(r.CreatedAt)),
		Approvals: []ApprovalDTO{}, Attachments: []AttachmentDTO{}, JournalEntryIDs: entries,
	}
	if r.EventID != nil {
		if e := cat.eventByID[*r.EventID]; e != nil {
			core.Event = &EventRef{ID: e.ID, Name: e.Name}
		}
	}
	for _, a := range approvals {
		d, err := cat.user(a.DirectorID)
		if err != nil {
			return RequestCore{}, err
		}
		core.Approvals = append(core.Approvals, ApprovalDTO{ID: a.ID, Director: userSummary(d), Decision: a.Decision, Comment: a.Comment, CreatedAt: isoMs(int64(a.CreatedAt))})
	}
	for i := range attachments {
		dto, err := attachmentDTO(&attachments[i], cat)
		if err != nil {
			return RequestCore{}, err
		}
		core.Attachments = append(core.Attachments, dto)
	}
	if held != nil {
		var res struct {
			Actual int64 `json:"actualTotalCents"`
		}
		if held.ResultJSON != nil {
			_ = json.Unmarshal([]byte(*held.ResultJSON), &res)
		}
		reason := "Held for director review"
		if held.LastError != nil {
			reason = *held.LastError
		}
		core.SettlementHold = &SettlementHold{AttemptedCents: res.Actual, ApprovedCents: r.TotalCents, Reason: reason, HeldAt: isoMs(int64(held.UpdatedAt))}
	}
	return core, nil
}

func attachmentDTO(a *store.Attachment, cat *catalog) (AttachmentDTO, error) {
	up, err := cat.user(a.UploadedBy)
	if err != nil {
		return AttachmentDTO{}, err
	}
	url := "/api/attachments/" + a.ID
	if a.WebViewLink != nil {
		url = *a.WebViewLink
	}
	return AttachmentDTO{ID: a.ID, Kind: a.Kind, Filename: a.Filename, MimeType: a.MimeType, SizeBytes: a.SizeBytes, Storage: a.Storage, URL: &url, UploadedBy: userSummary(up), CreatedAt: isoMs(int64(a.CreatedAt))}, nil
}

// requestDetail is what GET /api/requests/:id and every action endpoint return.
func requestDetail(ctx context.Context, q *store.Queries, cat *catalog, r *store.Request, viewer *store.User) (RequestDetail, error) {
	core, err := requestCore(ctx, q, cat, r)
	if err != nil {
		return RequestDetail{}, err
	}
	d := RequestDetail{RequestCore: core, AllowedTransitions: []string{}}
	d.CanEdit = viewer != nil && viewer.ID == r.RequesterID && r.Status == "draft"
	for _, to := range purchases.NextStatuses(r.Type, r.Status) {
		ok := false
		switch {
		case viewer == nil:
		case to == "submitted":
			ok = viewer.ID == r.RequesterID
		case to == "approved":
			ok = viewer.Role == auth.RoleDirector && viewer.ID != r.RequesterID
		default:
			ok = viewer.Role == auth.RoleDirector
		}
		if ok {
			d.AllowedTransitions = append(d.AllowedTransitions, to)
		}
	}
	return d, nil
}

// requestSnapshot is the audit image of a request (camelCase, like the v1 rows), and what
// historical finance reports read back to reconstruct past statuses.
func requestSnapshot(r *store.Request) map[string]any {
	return map[string]any{
		"id": r.ID, "number": r.Number, "requesterId": r.RequesterID, "type": r.Type, "title": r.Title, "vendor": r.Vendor,
		"itemUrl": r.ItemURL, "quantity": r.Quantity, "unitCostCents": r.UnitCostCents, "taxShipCents": r.TaxShipCents,
		"totalCents": r.TotalCents, "actualTotalCents": r.ActualTotalCents, "expenseAccountId": r.ExpenseAccountID, "fundId": r.FundID,
		"eventId": r.EventID, "functionalClass": r.FunctionalClass, "justification": r.Justification, "neededBy": r.NeededBy,
		"status": r.Status, "submittedAt": isoPtr(r.SubmittedAt), "decidedAt": isoPtr(r.DecidedAt),
		"createdAt": isoMs(int64(r.CreatedAt)), "updatedAt": isoMs(int64(r.UpdatedAt)),
	}
}

func jobSnapshot(j *store.Job) map[string]any {
	return map[string]any{
		"id": j.ID, "requestId": j.RequestID, "status": j.Status, "claimedByTokenId": j.ClaimedByTokenID, "claimedAt": isoPtr(j.ClaimedAt),
		"attempts": j.Attempts, "lastError": j.LastError, "createdAt": isoMs(int64(j.CreatedAt)), "updatedAt": isoMs(int64(j.UpdatedAt)),
	}
}

func eventSnapshot(e *store.Event) map[string]any {
	return map[string]any{
		"id": e.ID, "lumaId": e.LumaID, "name": e.Name, "startsAt": e.StartsAt, "endsAt": e.EndsAt, "location": e.Location, "url": e.URL,
		"hostUserId": e.HostUserID, "createdAt": isoMs(int64(e.CreatedAt)), "updatedAt": isoMs(int64(e.UpdatedAt)),
	}
}

func userSnapshot(u *store.User) map[string]any {
	return map[string]any{
		"id": u.ID, "discordId": u.DiscordID, "name": u.Name, "email": u.Email, "avatarUrl": u.AvatarURL, "role": u.Role, "active": u.Active, "title": u.Title,
		"lastLoginAt": isoPtr(u.LastLoginAt), "createdAt": isoMs(int64(u.CreatedAt)), "updatedAt": isoMs(int64(u.UpdatedAt)),
	}
}

func optOf[T any](f validate.Field[T]) store.Opt[T] {
	return store.Opt[T]{V: f.V, Set: f.Set, Null: f.Null}
}
