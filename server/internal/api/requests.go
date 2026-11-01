package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"officerdashboard/internal/accounting"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/files"
	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/money"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/purchases"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

var (
	purchaseTypes     = []string{"reimbursement", "club_purchase"}
	functionalClasses = []string{"program", "mgmt_general", "fundraising"}
	purchaseStatuses  = []string{"draft", "submitted", "approved", "denied", "fulfilling", "fulfilled", "reimbursed", "closed", "cancelled"}
	attachmentKinds   = []string{"receipt", "invoice", "quote", "other"}
)

func (c *Ctx) requestsList() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var cursor string
	var limit int
	var scope, status string
	var eventID, qtext validate.Field[string]
	if err := c.query(func(r *validate.Reader) {
		cursor, limit = pageQuery(r)
		scope = r.Str("scope", validate.S{Enum: []string{"mine", "all"}, HasDef: true, Default: "mine"}).V
		status = r.Str("status", validate.S{Enum: append([]string{"pending"}, purchaseStatuses...)}).V
		eventID = r.Str("eventId", validate.S{Min: 1, Max: 64})
		qtext = r.Str("q", validate.S{Max: 100})
	}); err != nil {
		return err
	}
	f := store.RequestFilter{EventID: eventID.V, Q: qtext.V}
	if scope == "mine" {
		f.RequesterID = u.ID
	}
	ctx := c.Context()
	q := c.S.db.Read()
	counts, err := q.RequestCounts(ctx, f)
	if err != nil {
		return err
	}
	if status == "pending" {
		status = "submitted"
	}
	f.Status = status
	f.Limit = limit + 1
	if cursor != "" {
		ms, id, err := parseCursor(cursor)
		if err != nil {
			return err
		}
		f.HasCursor, f.CursorCreatedAt, f.CursorID = true, ms, id
	}
	rows, err := q.Requests(ctx, f)
	if err != nil {
		return err
	}
	cat, err := loadCatalog(ctx, q, false)
	if err != nil {
		return err
	}
	out := struct {
		Items      []RequestListItem `json:"items"`
		NextCursor *string           `json:"nextCursor"`
		Counts     map[string]int    `json:"counts"`
	}{Items: []RequestListItem{}, Counts: counts}
	for i := range rows {
		if i == limit {
			next := cursorKey(int64(rows[limit-1].CreatedAt), rows[limit-1].ID)
			out.NextCursor = &next
			break
		}
		it, err := requestItem(&rows[i], cat)
		if err != nil {
			return err
		}
		out.Items = append(out.Items, it)
	}
	writeJSON(c.W, 200, out)
	return nil
}

func (c *Ctx) requestDetail() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	r, err := q.RequestByID(ctx, c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		return err
	}
	d, err := requestDetail(ctx, q, cat, r, u)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"request": d})
	return nil
}

// purchaseFields are the editable request fields as read from a JSON body.
type purchaseFields struct {
	Type            validate.Field[string]
	Title           validate.Field[string]
	Vendor          validate.Field[string]
	ItemURL         validate.Field[string]
	Quantity        validate.Field[int64]
	UnitCostCents   validate.Field[int64]
	TaxShipCents    validate.Field[int64]
	ExpenseAccount  validate.Field[string]
	FundID          validate.Field[string]
	EventID         validate.Field[string]
	FunctionalClass validate.Field[string]
	Justification   validate.Field[string]
	NeededBy        validate.Field[string]
	Submit          bool
}

func readPurchase(r *validate.Reader, create bool) purchaseFields {
	f := purchaseFields{
		Type:           r.Str("type", validate.S{Req: create, Enum: purchaseTypes}),
		Title:          r.Str("title", validate.S{Req: create, Trim: true, Min: 1, Max: 200}),
		Vendor:         r.Str("vendor", validate.S{Req: create, Trim: true, Min: 1, Max: 200}),
		ItemURL:        r.Str("itemUrl", validate.S{Null: true, Format: "url"}),
		Quantity:       r.Int("quantity", validate.I{Req: create, Min: validate.Lo(1), Max: validate.Lo(10_000)}),
		UnitCostCents:  r.Int("unitCostCents", validate.I{Req: create, Min: validate.Lo(0)}),
		ExpenseAccount: r.Str("expenseAccountId", validate.S{Req: create, Min: 1, Max: 64}),
		FundID:         r.Str("fundId", validate.S{Req: create, Min: 1, Max: 64}),
		EventID:        r.Str("eventId", validate.S{Null: true, Min: 1, Max: 64}),
		Justification:  r.Str("justification", validate.S{Req: create, Trim: true, Min: 1, Max: 4000}),
		NeededBy:       r.Str("neededBy", validate.S{Null: true, Format: "date"}),
	}
	if create {
		f.TaxShipCents = r.Int("taxShipCents", validate.I{Min: validate.Lo(0), HasDef: true, Def: 0})
		f.FunctionalClass = r.Str("functionalClass", validate.S{Enum: functionalClasses, HasDef: true, Default: "program"})
		f.Submit = r.Bool("submit", false)
	} else {
		f.TaxShipCents = r.Int("taxShipCents", validate.I{Min: validate.Lo(0)})
		f.FunctionalClass = r.Str("functionalClass", validate.S{Enum: functionalClasses})
	}
	return f
}

// validateCategory checks the references of a request: an active budgetable expense
// account, an active fund and, when given, an existing event.
func validateCategory(ctx context.Context, q *store.Queries, accountID, fundID string, eventID *string) error {
	cat, err := loadCatalog(ctx, q, false)
	if err != nil {
		return err
	}
	a := cat.acctByID[accountID]
	if a == nil || !a.Active || !a.Budgetable || a.Type != "expense" {
		return badRequest("Select an active budgetable expense account")
	}
	if f := cat.fundByID[fundID]; f == nil || !f.Active {
		return badRequest("Select an active fund")
	}
	if eventID != nil {
		if _, err := q.EventByID(ctx, *eventID); err != nil {
			return notFoundErr("Record not found")
		}
	}
	return nil
}

func totalOrFail(qty, unit, tax int64) (int64, error) {
	t, err := money.PurchaseTotal(qty, unit, tax)
	if err != nil {
		return 0, badRequest("Total exceeds safe integer cents")
	}
	return t, nil
}

func (c *Ctx) requestCreate() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var in purchaseFields
	if err := c.jsonBody(func(r *validate.Reader) { in = readPurchase(r, true) }); err != nil {
		return err
	}
	ctx := c.Context()
	id := auth.UUID()
	now := c.S.now()
	var detail RequestDetail
	var created *store.Request
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		if err := validateCategory(ctx, w, in.ExpenseAccount.V, in.FundID.V, ptrIf(in.EventID)); err != nil {
			return err
		}
		total, err := totalOrFail(in.Quantity.V, in.UnitCostCents.V, in.TaxShipCents.V)
		if err != nil {
			return err
		}
		status := "draft"
		var submitted *int64
		if in.Submit {
			status = "submitted"
			ms := now.UnixMilli()
			submitted = &ms
		}
		r := store.Request{
			ID: id, RequesterID: u.ID, Type: in.Type.V, Title: in.Title.V, Vendor: in.Vendor.V, ItemURL: ptrIf(in.ItemURL),
			Quantity: in.Quantity.V, UnitCostCents: in.UnitCostCents.V, TaxShipCents: in.TaxShipCents.V, TotalCents: total,
			ExpenseAccountID: in.ExpenseAccount.V, FundID: in.FundID.V, EventID: ptrIf(in.EventID), FunctionalClass: in.FunctionalClass.V,
			Justification: in.Justification.V, NeededBy: ptrIf(in.NeededBy), Status: status, SubmittedAt: submitted,
			CreatedAt: store.Ms(now.UnixMilli()), UpdatedAt: store.Ms(now.UnixMilli()),
		}
		fy, err := fiscal.YearFor(fiscal.PacificDate(now), fiscal.DefaultStartMonth)
		if err != nil {
			return err
		}
		if r.Number, err = w.InsertRequest(ctx, r, fy); err != nil {
			return err
		}
		action := "request.created"
		if in.Submit {
			action = "request.created_submitted"
		}
		if err := c.audit(ctx, w, action, "purchase_request", id, nil, requestSnapshot(&r)); err != nil {
			return err
		}
		created = &r
		cat, err := loadCatalog(ctx, w, true)
		if err != nil {
			return err
		}
		detail, err = requestDetail(ctx, w, cat, &r, u)
		return err
	})
	if err != nil {
		return err
	}
	if in.Submit {
		c.notify(created, "submit", "")
	}
	writeJSON(c.W, 200, map[string]any{"request": detail})
	return nil
}

func (c *Ctx) requestUpdate() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var in purchaseFields
	if err := c.jsonBody(func(r *validate.Reader) { in = readPurchase(r, false) }); err != nil {
		return err
	}
	ctx := c.Context()
	var detail RequestDetail
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		r, err := w.RequestByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		if u.ID != r.RequesterID {
			return forbidden("Only the requester may edit or delete")
		}
		if r.Status != "draft" {
			return conflict("Only drafts may be edited or deleted")
		}
		before := requestSnapshot(r)
		next := *r
		if in.Type.Has() {
			next.Type = in.Type.V
		}
		if in.Title.Has() {
			next.Title = in.Title.V
		}
		if in.Vendor.Has() {
			next.Vendor = in.Vendor.V
		}
		if in.ItemURL.Set {
			next.ItemURL = ptrIf(in.ItemURL)
		}
		if in.Quantity.Has() {
			next.Quantity = in.Quantity.V
		}
		if in.UnitCostCents.Has() {
			next.UnitCostCents = in.UnitCostCents.V
		}
		if in.TaxShipCents.Has() {
			next.TaxShipCents = in.TaxShipCents.V
		}
		if in.ExpenseAccount.Has() {
			next.ExpenseAccountID = in.ExpenseAccount.V
		}
		if in.FundID.Has() {
			next.FundID = in.FundID.V
		}
		if in.EventID.Set {
			next.EventID = ptrIf(in.EventID)
		}
		if in.FunctionalClass.Has() {
			next.FunctionalClass = in.FunctionalClass.V
		}
		if in.Justification.Has() {
			next.Justification = in.Justification.V
		}
		if in.NeededBy.Set {
			next.NeededBy = ptrIf(in.NeededBy)
		}
		if err := validateCategory(ctx, w, next.ExpenseAccountID, next.FundID, next.EventID); err != nil {
			return err
		}
		if next.TotalCents, err = totalOrFail(next.Quantity, next.UnitCostCents, next.TaxShipCents); err != nil {
			return err
		}
		next.UpdatedAt = store.Ms(c.S.nowMs())
		if err := w.UpdateDraftFields(ctx, next, c.S.nowMs()); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "request.updated", "purchase_request", r.ID, before, requestSnapshot(&next)); err != nil {
			return err
		}
		cat, err := loadCatalog(ctx, w, true)
		if err != nil {
			return err
		}
		detail, err = requestDetail(ctx, w, cat, &next, u)
		return err
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"request": detail})
	return nil
}

func (c *Ctx) requestDelete() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		r, err := w.RequestByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		if u.ID != r.RequesterID {
			return forbidden("Only the requester may edit or delete")
		}
		if r.Status != "draft" {
			return conflict("Only drafts may be edited or deleted")
		}
		attached, err := w.RequestAttachments(ctx, r.ID)
		if err != nil {
			return err
		}
		snap := requestSnapshot(r)
		list := make([]map[string]any, len(attached))
		for i, a := range attached {
			list[i] = map[string]any{"id": a.ID, "kind": a.Kind, "filename": a.Filename, "mimeType": a.MimeType, "sizeBytes": a.SizeBytes, "storage": a.Storage, "storageKey": a.StorageKey}
		}
		snap["attachments"] = list
		if err := w.DeleteRequestWithAttachments(ctx, r.ID); err != nil {
			return err
		}
		return c.audit(ctx, w, "request.deleted", "purchase_request", r.ID, snap, nil)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

// ─── attachments ───

func (c *Ctx) requestAttach() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	r, err := c.S.db.Read().RequestByID(ctx, id)
	if err != nil {
		return notFoundErr("Record not found")
	}
	if u.ID != r.RequesterID && !auth.HasRole(u.Role, auth.RoleDirector) {
		return forbidden("Only requester or director may upload")
	}
	if r.Status == "closed" || r.Status == "cancelled" || r.Status == "denied" {
		return conflict("Request no longer accepts attachments")
	}
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, files.MaxBytes+1<<20)
	if err := c.R.ParseMultipartForm(8 << 20); err != nil {
		return badRequest("Expected multipart/form-data with a file")
	}
	rd := validate.FromValues(c.R.MultipartForm.Value)
	kind := rd.Str("kind", validate.S{Enum: attachmentKinds, HasDef: true, Default: "receipt"})
	if rd.Issues.Any() {
		return validationError(rd.Issues)
	}
	fhs := c.R.MultipartForm.File["file"]
	if len(fhs) == 0 {
		return badRequest("File required")
	}
	fh := fhs[0]
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(fh.Header.Get("Content-Type"), ";", 2)[0]))
	if err := files.ValidateUpload(mime, fh.Size); err != nil {
		return err
	}
	f, err := fh.Open()
	if err != nil {
		return badRequest("File required")
	}
	body, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return badRequest("File required")
	}
	if err := files.CheckContent(mime, body); err != nil {
		return err
	}
	at := int64(r.CreatedAt)
	if r.SubmittedAt != nil {
		at = *r.SubmittedAt
	}
	fy, _ := fiscal.YearFor(fiscal.PacificDate(time.UnixMilli(at)), fiscal.DefaultStartMonth)
	stored, err := c.S.cfg.Files.Put(ctx, files.PutInput{Folder: []string{fmt.Sprintf("FY%d", fy), r.ID}, Filename: fh.Filename, MimeType: mime, Body: body})
	if err != nil {
		return err
	}
	var dto AttachmentDTO
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		cur, err := w.RequestByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		if cur.Status == "closed" || cur.Status == "cancelled" || cur.Status == "denied" {
			return conflict("Request no longer accepts attachments")
		}
		a := store.Attachment{ID: auth.UUID(), RequestID: &id, Kind: kind.V, Filename: fh.Filename, MimeType: mime, SizeBytes: stored.SizeBytes,
			Storage: stored.Storage, StorageKey: stored.Key, UploadedBy: u.ID, CreatedAt: store.Ms(c.S.nowMs())}
		if stored.WebViewLink != "" {
			l := stored.WebViewLink
			a.WebViewLink = &l
		}
		if err := w.InsertAttachment(ctx, a); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "request.attachment", "attachment", a.ID, nil, map[string]any{"id": a.ID, "requestId": id, "kind": a.Kind, "filename": a.Filename, "mimeType": a.MimeType, "sizeBytes": a.SizeBytes, "storage": a.Storage, "uploadedBy": u.ID}); err != nil {
			return err
		}
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		dto, err = attachmentDTO(&a, cat)
		return err
	})
	if err != nil {
		// The row was not stored; do not leave the blob behind.
		if derr := c.S.cfg.Files.Delete(context.WithoutCancel(ctx), stored.Key); derr != nil {
			c.S.log.Error("could not remove an orphaned upload", "key", stored.Key, "error", derr)
		}
		return err
	}
	writeJSON(c.W, 200, map[string]any{"attachment": dto})
	return nil
}

var unsafeFilename = regexp.MustCompile(`[^\w.\- ]+`)

func (c *Ctx) attachmentDownload() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	a, err := c.S.db.Read().AttachmentByID(ctx, c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	if a.Storage == "drive" {
		if a.WebViewLink == nil {
			return notFoundErr("Record not found")
		}
		http.Redirect(c.W, c.R, *a.WebViewLink, http.StatusFound)
		return nil
	}
	file, err := c.S.cfg.Files.Get(ctx, a.StorageKey)
	if err != nil {
		return err
	}
	if file == nil {
		return notFoundErr("Record not found")
	}
	h := c.W.Header()
	h.Set("Content-Type", file.MimeType)
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+encodeRFC5987(a.Filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	_, _ = c.W.Write(file.Body)
	return nil
}

func encodeRFC5987(s string) string {
	var b strings.Builder
	for _, by := range []byte(s) {
		switch {
		case by >= 'a' && by <= 'z', by >= 'A' && by <= 'Z', by >= '0' && by <= '9', strings.IndexByte("-_.!~*'()", by) >= 0 && by != '\'':
			b.WriteByte(by)
		default:
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}

// ─── lifecycle ───

// settlement is the body of a posting transition: what was actually paid.
type settlement struct {
	ActualCents int64
	Attachments []string
	OrderRef    string
	Comment     string
}

func readSettlement(r *validate.Reader) settlement {
	s := settlement{}
	s.ActualCents = r.Int("actualTotalCents", validate.I{Req: true, Min: validate.Lo(0)}).V
	s.Attachments = r.Strings("attachmentIds", false, 0, 20, nil, 64)
	s.OrderRef = r.Str("orderReference", validate.S{Trim: true, Max: 200}).V
	s.Comment = r.Str("comment", validate.S{Trim: true, Max: 2000}).V
	return s
}

// settleWrites posts the journal for what was actually paid, releases donor restrictions
// for restricted funds and links receipts. It returns nil entries for a free order. Errors
// carry v1's status mapping.
func (c *Ctx) settleWrites(ctx context.Context, w *store.Queries, r *store.Request, st settlement) error {
	receipts, err := w.AttachmentsByIDs(ctx, st.Attachments)
	if err != nil {
		return err
	}
	uniq := map[string]bool{}
	for _, id := range st.Attachments {
		uniq[id] = true
	}
	if len(uniq) != len(receipts) {
		return badRequest("Receipts must belong to this request and be unposted")
	}
	for _, a := range receipts {
		if a.RequestID == nil || *a.RequestID != r.ID || a.JournalEntry != nil {
			return badRequest("Receipts must belong to this request and be unposted")
		}
	}
	// A genuinely free order has no accounting movement; zero lines cannot be posted.
	if st.ActualCents == 0 {
		return nil
	}
	accounts, err := w.Accounts(ctx)
	if err != nil {
		return err
	}
	funds, err := w.Funds(ctx)
	if err != nil {
		return err
	}
	periods, err := w.FiscalPeriods(ctx)
	if err != nil {
		return err
	}
	var fund *accounting.Fund
	for i := range funds {
		if funds[i].ID == r.FundID {
			fund = &funds[i]
		}
	}
	if fund == nil {
		return notFoundErr("Record not found")
	}
	today := fiscal.PacificDate(c.S.now())
	if fund.Restriction == "with_donor" &&
		((fund.StartDate != nil && today < *fund.StartDate) || (fund.EndDate != nil && today > *fund.EndDate)) {
		return conflict("Restricted fund is outside its spending dates; review the fund allocation")
	}
	byCode := func(code string) *accounting.Account {
		for i := range accounts {
			if accounts[i].Code == code {
				return &accounts[i]
			}
		}
		return nil
	}
	creditCode := "1000"
	if r.Type == purchases.Reimbursement {
		creditCode = "2100"
	}
	credit := byCode(creditCode)
	if credit == nil {
		return notFoundErr("Record not found")
	}
	ectx := engineContext(accounts, funds, periods)
	memo := fmt.Sprintf("%s: %s", r.Number, r.Title)
	if st.OrderRef != "" {
		memo += " (" + st.OrderRef + ")"
	}
	entry, err := accounting.PostEntry(accounting.Entry{
		ID: auth.UUID(), EntryDate: today, Memo: memo, Source: "purchase_request",
		Lines: []accounting.Line{
			{AccountID: r.ExpenseAccountID, FundID: r.FundID, FunctionalClass: r.FunctionalClass, DebitCents: st.ActualCents},
			{AccountID: credit.ID, FundID: r.FundID, CreditCents: st.ActualCents},
		},
	}, ectx)
	if err != nil {
		return engineErr(err)
	}
	userID := ""
	if c.User != nil {
		userID = c.User.ID
	}
	links := store.EntryLinks{PurchaseRequestID: r.ID, SettlementKey: r.ID, CreatedBy: userID, PostedBy: userID}
	now := c.S.nowMs()
	if err := w.InsertPostedEntry(ctx, entry, links, now, auth.UUID); err != nil {
		return err
	}
	if fund.Restriction == "with_donor" {
		available, err := w.AvailableRestricted(ctx, r.FundID)
		if err != nil {
			return err
		}
		var restricted, unrestricted *accounting.Account
		for i := range accounts {
			a := &accounts[i]
			if a.Type == "net_asset" && a.Restriction != nil {
				if *a.Restriction == "with_donor" && restricted == nil {
					restricted = a
				}
				if *a.Restriction == "without_donor" && unrestricted == nil {
					unrestricted = a
				}
			}
		}
		if restricted == nil || unrestricted == nil {
			return notFoundErr("Record not found")
		}
		release, err := accounting.ReleaseRestriction(accounting.Release{
			ID: auth.UUID(), EntryDate: today, Memo: fmt.Sprintf("Release for %s: satisfied purchase purpose", r.Number),
			AmountCents: st.ActualCents, FundID: r.FundID, RestrictedNetAssetID: restricted.ID, UnrestrictedNetAssetID: unrestricted.ID,
			AvailableRestrictedCents: available,
		}, ectx)
		if err != nil {
			return engineErr(err)
		}
		if err := w.InsertPostedEntry(ctx, release, store.EntryLinks{PurchaseRequestID: r.ID, CreatedBy: userID, PostedBy: userID}, now, auth.UUID); err != nil {
			return err
		}
	}
	if len(receipts) > 0 {
		ids := make([]string, len(receipts))
		for i, a := range receipts {
			ids[i] = a.ID
		}
		return w.LinkAttachments(ctx, ids, entry.ID)
	}
	return nil
}

func engineContext(accounts []accounting.Account, funds []accounting.Fund, periods []store.FiscalPeriod) accounting.Context {
	ps := make([]accounting.Period, len(periods))
	for i, p := range periods {
		ps[i] = accounting.Period{ID: p.ID, StartDate: p.StartDate, EndDate: p.EndDate, Status: p.Status}
	}
	return accounting.Context{Accounts: accounts, Funds: funds, Periods: ps}
}

var engineConflictRE = regexp.MustCompile(`open period|inactive|functional|unsafe`)

// engineErr maps posting-engine failures to the status v1 used.
func engineErr(err error) error {
	switch {
	case engineConflictRE.MatchString(err.Error()):
		return conflict(err.Error())
	case strings.Contains(err.Error(), "release exceeds"):
		return conflict("Actual cost exceeds available restricted funding; review the fund allocation")
	}
	return err
}

// notify sends the Discord/console notification for a request event. Best effort, after commit.
func (c *Ctx) notify(r *store.Request, action, comment string) {
	ctx := c.Context()
	requester, err := c.S.db.Read().UserByID(ctx, r.RequesterID)
	if err != nil {
		return
	}
	total := r.TotalCents
	if r.ActualTotalCents != nil {
		total = *r.ActualTotalCents
	}
	e := notify.Event{RequestID: r.ID, Number: r.Number, Title: r.Title, TotalCents: notify.Cents(total), Requester: requester.Name}
	switch action {
	case "submit":
		e.Type = "request.submitted"
	case "approve", "deny":
		e.Type = "request.approved"
		if action == "deny" {
			e.Type = "request.denied"
		}
		e.Director, e.Comment = "Purchasing agent", comment
		if c.User != nil {
			e.Director = c.User.Name
		}
	default:
		e.Type, e.Status = "request.status", r.Status
	}
	c.S.cfg.Notifier.Notify(ctx, e, c.S.link("/requests/"+r.ID, c.R))
}

// link builds an absolute URL to an app page.
func (s *Server) link(path string, r *http.Request) string {
	base := strings.TrimRight(s.cfg.AppURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + path
}

type transitionInput struct {
	To      string
	Comment string
	Settle  *settlement
}

func readTransition(r *validate.Reader) transitionInput {
	to := r.Str("to", validate.S{Req: true, Enum: []string{"fulfilling", "fulfilled", "reimbursed", "closed", "cancelled"}})
	t := transitionInput{To: to.V}
	switch to.V {
	case "fulfilled", "reimbursed":
		s := readSettlement(r)
		t.Settle, t.Comment = &s, s.Comment
	default:
		t.Comment = r.Str("comment", validate.S{Trim: true, Max: 2000}).V
	}
	return t
}

// lifecycle runs submit / approve / deny / transition for the named action.
func (c *Ctx) lifecycle(action string) error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var in transitionInput
	switch action {
	case "submit":
		in.To = "submitted"
	case "approve", "deny":
		if _, err := c.director(); err != nil {
			return err
		}
		if err := c.jsonBody(func(r *validate.Reader) {
			if action == "deny" {
				in.Comment = r.Str("comment", validate.S{Req: true, Trim: true, Min: 1, Max: 2000}).V
			} else {
				in.Comment = r.Str("comment", validate.S{Trim: true, Max: 2000}).V
			}
		}); err != nil {
			return err
		}
		in.To = map[string]string{"approve": "approved", "deny": "denied"}[action]
	case "transition":
		if _, err := c.director(); err != nil {
			return err
		}
		if err := c.jsonBody(func(r *validate.Reader) { in = readTransition(r) }); err != nil {
			return err
		}
	}
	ctx := c.Context()
	var detail RequestDetail
	var after *store.Request
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		r, err := w.RequestByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		if action == "submit" && r.RequesterID != u.ID {
			return forbidden("Only requester may submit")
		}
		now := c.S.nowMs()
		if action == "approve" || action == "deny" {
			if in.To == "approved" && r.RequesterID == u.ID {
				return forbidden("You cannot approve your own request")
			}
			var comment *string
			if in.Comment != "" {
				cm := in.Comment
				comment = &cm
			}
			if err := w.InsertApproval(ctx, store.Approval{ID: auth.UUID(), RequestID: r.ID, DirectorID: u.ID, Decision: in.To, Comment: comment, CreatedAt: store.Ms(now)}); err != nil {
				return err
			}
			if in.To == "approved" && r.Type == purchases.ClubPurchase {
				payload := fmt.Sprintf(`{"vendor":%q,"itemUrl":%s,"quantity":%d,"totalCents":%d}`, r.Vendor, jsonStrOrNull(r.ItemURL), r.Quantity, r.TotalCents)
				if err := w.InsertJob(ctx, store.Job{ID: auth.UUID(), RequestID: r.ID, Status: "queued", PayloadJSON: &payload, CreatedAt: store.Ms(now), UpdatedAt: store.Ms(now)}); err != nil {
					return err
				}
			}
		}
		if !purchases.CanTransition(r.Type, r.Status, in.To) {
			return fail(409, "invalid_transition", fmt.Sprintf("Cannot move %s to %s", r.Status, in.To))
		}
		if in.Settle != nil {
			if err := c.settleWrites(ctx, w, r, *in.Settle); err != nil {
				return err
			}
		}
		if in.To == "closed" && r.Type == purchases.Reimbursement && r.ActualTotalCents != nil && *r.ActualTotalCents > 0 {
			if err := c.payout(ctx, w, r); err != nil {
				return err
			}
		}
		switch {
		case in.To == "cancelled":
			if err := w.MoveJobs(ctx, r.ID, []string{"queued", "claimed", "held"}, "cancelled", now); err != nil {
				return err
			}
		case in.To == "approved" && r.Status == "fulfilling":
			// Sending a held request back to approved cancels the held job (never re-queued,
			// so nothing is bought twice).
			if err := w.MoveJobs(ctx, r.ID, []string{"held"}, "cancelled", now); err != nil {
				return err
			}
		case in.To == "fulfilled":
			if err := w.MoveJobs(ctx, r.ID, []string{"queued", "claimed", "held"}, "done", now); err != nil {
				return err
			}
		}
		change := store.StatusChange{Status: in.To, UpdatedAt: now}
		if in.To == "submitted" {
			change.SubmittedAt = &now
		}
		if in.To == "approved" || in.To == "denied" {
			change.DecidedAt = &now
		}
		if in.Settle != nil {
			a := in.Settle.ActualCents
			change.ActualTotalCents = &a
		}
		before := requestSnapshot(r)
		if err := w.SetRequestStatus(ctx, r.ID, change); err != nil {
			return err
		}
		updated, err := w.RequestByID(ctx, r.ID)
		if err != nil {
			return err
		}
		afterImg := map[string]any{"status": in.To, "updatedAt": isoMs(now), "comment": nilIfEmpty(in.Comment)}
		if change.SubmittedAt != nil {
			afterImg["submittedAt"] = isoMs(now)
		}
		if change.DecidedAt != nil {
			afterImg["decidedAt"] = isoMs(now)
		}
		if change.ActualTotalCents != nil {
			afterImg["actualTotalCents"] = *change.ActualTotalCents
		}
		if in.To == "approved" {
			afterImg["encumbranceMemo"] = fmt.Sprintf("Encumber %d cents to %s/%s", r.TotalCents, r.ExpenseAccountID, r.FundID)
		}
		if err := c.audit(ctx, w, "request."+in.To, "purchase_request", r.ID, before, afterImg); err != nil {
			return err
		}
		after = updated
		cat, err := loadCatalog(ctx, w, true)
		if err != nil {
			return err
		}
		detail, err = requestDetail(ctx, w, cat, updated, u)
		return err
	})
	if err != nil {
		return err
	}
	switch in.To {
	case "submitted":
		c.notify(after, "submit", in.Comment)
	case "approved":
		c.notify(after, "approve", in.Comment)
	case "denied":
		c.notify(after, "deny", in.Comment)
	case "fulfilled", "reimbursed":
		c.notify(after, "status", in.Comment)
	}
	writeJSON(c.W, 200, map[string]any{"request": detail})
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func jsonStrOrNull(s *string) string {
	if s == nil {
		return "null"
	}
	return fmt.Sprintf("%q", *s)
}

// payout records paying a reimbursement (debit payable, credit checking) when it closes.
func (c *Ctx) payout(ctx context.Context, w *store.Queries, r *store.Request) error {
	accounts, err := w.Accounts(ctx)
	if err != nil {
		return err
	}
	funds, err := w.Funds(ctx)
	if err != nil {
		return err
	}
	periods, err := w.FiscalPeriods(ctx)
	if err != nil {
		return err
	}
	find := func(code string) (*accounting.Account, error) {
		for i := range accounts {
			if accounts[i].Code == code {
				return &accounts[i], nil
			}
		}
		return nil, notFoundErr("Record not found")
	}
	payable, err := find("2100")
	if err != nil {
		return err
	}
	cash, err := find("1000")
	if err != nil {
		return err
	}
	amount := *r.ActualTotalCents
	entry, err := accounting.PostEntry(accounting.Entry{
		ID: auth.UUID(), EntryDate: fiscal.PacificDate(c.S.now()), Memo: "Reimbursement paid: " + r.Number, Source: "purchase_request",
		Lines: []accounting.Line{
			{AccountID: payable.ID, FundID: r.FundID, DebitCents: amount},
			{AccountID: cash.ID, FundID: r.FundID, CreditCents: amount},
		},
	}, engineContext(accounts, funds, periods))
	if err != nil {
		return engineErr(err)
	}
	userID := ""
	if c.User != nil {
		userID = c.User.ID
	}
	return w.InsertPostedEntry(ctx, entry, store.EntryLinks{PurchaseRequestID: r.ID, CreatedBy: userID, PostedBy: userID}, c.S.nowMs(), auth.UUID)
}

var _ = errors.New
