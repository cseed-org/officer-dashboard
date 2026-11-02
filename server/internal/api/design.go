package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/files"
	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// Design requests: one board every officer sees. Members of the Design subteam (and
// director+, as an administrative override) assign and move work; only the requester
// approves a version in review, which is the one way a card reaches Delivered.

// ─── DTOs (DesignRequest* in src/core/contracts.ts) ───

type DesignEventRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	StartsAt string `json:"startsAt"`
}

type DesignRequestItem struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Type         string          `json:"type"`
	Formats      string          `json:"formats"`
	DueDate      *string         `json:"dueDate"`
	Urgency      string          `json:"urgency"`
	Stage        string          `json:"stage"`
	Event        *DesignEventRef `json:"event"`
	Program      *string         `json:"program"`
	Requester    UserSummary     `json:"requester"`
	Assignee     *UserSummary    `json:"assignee"`
	CommentCount int             `json:"commentCount"`
	FileCount    int             `json:"fileCount"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

type DesignCommentDTO struct {
	ID        string      `json:"id"`
	Author    UserSummary `json:"author"`
	Body      string      `json:"body"`
	CreatedAt string      `json:"createdAt"`
}

type DesignFileDTO struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Filename   string      `json:"filename"`
	MimeType   string      `json:"mimeType"`
	SizeBytes  int64       `json:"sizeBytes"`
	URL        string      `json:"url"`
	UploadedBy UserSummary `json:"uploadedBy"`
	CreatedAt  string      `json:"createdAt"`
}

type DesignPermissions struct {
	CanEdit           bool     `json:"canEdit"`
	CanDelete         bool     `json:"canDelete"`
	CanAssign         bool     `json:"canAssign"`
	CanAssignSelf     bool     `json:"canAssignSelf"`
	MoveTo            []string `json:"moveTo"`
	CanApprove        bool     `json:"canApprove"`
	CanRequestChanges bool     `json:"canRequestChanges"`
	UploadKinds       []string `json:"uploadKinds"`
}

type DesignRequestDetail struct {
	DesignRequestItem
	Brief       string             `json:"brief"`
	References  string             `json:"references"`
	ApprovedBy  *UserSummary       `json:"approvedBy"`
	ApprovedAt  *string            `json:"approvedAt"`
	Comments    []DesignCommentDTO `json:"comments"`
	Files       []DesignFileDTO    `json:"files"`
	Permissions DesignPermissions  `json:"permissions"`
}

type DesignViewer struct {
	IsDesigner bool `json:"isDesigner"`
	CanAssign  bool `json:"canAssign"`
}

type DesignRequestDraft struct {
	Title   string         `json:"title"`
	Type    string         `json:"type"`
	Formats string         `json:"formats"`
	DueDate *string        `json:"dueDate"`
	Urgency string         `json:"urgency"`
	Brief   string         `json:"brief"`
	EventID string         `json:"eventId"`
	Event   DesignEventRef `json:"event"`
}

// ─── shared helpers ───

// designCtx is what a design handler knows about the board and the viewer.
type designCtx struct {
	cat       *catalog
	designers []string
	viewer    *store.User
}

func (d *designCtx) isDesigner(id string) bool { return slices.Contains(d.designers, id) }

// staff is director+: an administrative override for assignment and moves, never for approval.
func (d *designCtx) staff() bool { return auth.HasRole(d.viewer.Role, auth.RoleDirector) }

func (d *designCtx) worker() bool { return d.isDesigner(d.viewer.ID) || d.staff() }

func loadDesignCtx(ctx context.Context, q *store.Queries, viewer *store.User) (*designCtx, error) {
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		return nil, err
	}
	ids, err := q.DesignTeamIDs(ctx)
	if err != nil {
		return nil, err
	}
	return &designCtx{cat: cat, designers: ids, viewer: viewer}, nil
}

func (d *designCtx) designerSummaries() []UserSummary {
	out := []UserSummary{}
	for _, id := range d.designers {
		if u := d.cat.users[id]; u != nil {
			out = append(out, userSummary(u))
		}
	}
	return out
}

func (d *designCtx) item(r *store.DesignRequest, comments, files int) (DesignRequestItem, error) {
	req, err := d.cat.user(r.RequesterID)
	if err != nil {
		return DesignRequestItem{}, err
	}
	it := DesignRequestItem{ID: r.ID, Title: r.Title, Type: r.Type, Formats: r.Formats, DueDate: r.DueDate, Urgency: r.Urgency, Stage: r.Stage,
		Program: r.Program, Requester: userSummary(req), CommentCount: comments, FileCount: files, CreatedAt: isoMs(r.CreatedAt), UpdatedAt: isoMs(r.UpdatedAt)}
	if r.AssigneeID != nil {
		if u := d.cat.users[*r.AssigneeID]; u != nil {
			s := userSummary(u)
			it.Assignee = &s
		}
	}
	if r.EventID != nil {
		if e := d.cat.eventByID[*r.EventID]; e != nil {
			it.Event = &DesignEventRef{ID: e.ID, Name: e.Name, StartsAt: e.StartsAt}
		}
	}
	return it, nil
}

func (d *designCtx) permissions(r *store.DesignRequest) DesignPermissions {
	v := d.viewer
	requester := v.ID == r.RequesterID
	open := r.Stage != "delivered"
	p := DesignPermissions{MoveTo: []string{}, UploadKinds: []string{}}
	p.CanEdit = open && (requester || d.staff())
	p.CanDelete = (requester && r.Stage == "new") || d.staff()
	p.CanAssign = open && d.worker()
	p.CanAssignSelf = open && d.isDesigner(v.ID) && (r.AssigneeID == nil || *r.AssigneeID != v.ID)
	p.CanApprove = requester && r.Stage == "in-review"
	p.CanRequestChanges = p.CanApprove
	if d.worker() {
		for _, s := range designMoveTargets(r.Stage) {
			// Moving past New needs a designer: the mover self-assigns if they are one.
			if s != "new" && r.AssigneeID == nil && !d.isDesigner(v.ID) {
				continue
			}
			p.MoveTo = append(p.MoveTo, s)
		}
	} else if p.CanRequestChanges {
		p.MoveTo = append(p.MoveTo, "in-progress")
	}
	// References until delivery; review versions while open; delivered files any time
	// (the design team adds finals before or after approval). Director+ share the team's rights.
	if open && (requester || d.worker()) {
		p.UploadKinds = append(p.UploadKinds, "reference")
	}
	if open && d.worker() {
		p.UploadKinds = append(p.UploadKinds, "review")
	}
	if d.worker() {
		p.UploadKinds = append(p.UploadKinds, "delivered")
	}
	return p
}

// designMoveTargets is where designers may move a card from a stage. Delivered is reached
// only by the requester's approval; a delivered card can be reopened to In progress.
func designMoveTargets(from string) []string {
	if from == "delivered" {
		return []string{"in-progress"}
	}
	out := []string{}
	for _, s := range store.DesignStages {
		if s != from && s != "delivered" {
			out = append(out, s)
		}
	}
	return out
}

func (d *designCtx) detail(ctx context.Context, q *store.Queries, r *store.DesignRequest) (DesignRequestDetail, error) {
	comments, err := q.DesignComments(ctx, r.ID)
	if err != nil {
		return DesignRequestDetail{}, err
	}
	fs, err := q.DesignFiles(ctx, r.ID)
	if err != nil {
		return DesignRequestDetail{}, err
	}
	it, err := d.item(r, len(comments), len(fs))
	if err != nil {
		return DesignRequestDetail{}, err
	}
	out := DesignRequestDetail{DesignRequestItem: it, Brief: r.Brief, References: r.Refs, ApprovedAt: isoPtr(r.ApprovedAt),
		Comments: []DesignCommentDTO{}, Files: []DesignFileDTO{}, Permissions: d.permissions(r)}
	if r.ApprovedBy != nil {
		if u := d.cat.users[*r.ApprovedBy]; u != nil {
			s := userSummary(u)
			out.ApprovedBy = &s
		}
	}
	for _, c := range comments {
		a, err := d.cat.user(c.AuthorID)
		if err != nil {
			return DesignRequestDetail{}, err
		}
		out.Comments = append(out.Comments, DesignCommentDTO{ID: c.ID, Author: userSummary(a), Body: c.Body, CreatedAt: isoMs(c.CreatedAt)})
	}
	for i := range fs {
		f, err := d.fileDTO(&fs[i])
		if err != nil {
			return DesignRequestDetail{}, err
		}
		out.Files = append(out.Files, f)
	}
	return out, nil
}

func (d *designCtx) fileDTO(f *store.DesignFile) (DesignFileDTO, error) {
	up, err := d.cat.user(f.UploadedBy)
	if err != nil {
		return DesignFileDTO{}, err
	}
	url := "/api/design-files/" + f.ID
	if f.WebViewLink != nil {
		url = *f.WebViewLink
	}
	return DesignFileDTO{ID: f.ID, Kind: f.Kind, Filename: f.Filename, MimeType: f.MimeType, SizeBytes: f.SizeBytes, URL: url, UploadedBy: userSummary(up), CreatedAt: isoMs(f.CreatedAt)}, nil
}

func designSnapshot(r *store.DesignRequest) map[string]any {
	return map[string]any{
		"id": r.ID, "title": r.Title, "type": r.Type, "formats": r.Formats, "dueDate": r.DueDate, "urgency": r.Urgency, "brief": r.Brief,
		"references": r.Refs, "eventId": r.EventID, "program": r.Program, "stage": r.Stage, "requesterId": r.RequesterID, "assigneeId": r.AssigneeID,
		"approvedBy": r.ApprovedBy, "approvedAt": isoPtr(r.ApprovedAt), "createdAt": isoMs(r.CreatedAt), "updatedAt": isoMs(r.UpdatedAt),
	}
}

// designInput is the editable part of a request; Field.Set says whether it was supplied.
type designInput struct {
	Title, Type, Formats, DueDate, Urgency, Brief, Refs, EventID, Program validate.Field[string]
}

func readDesign(r *validate.Reader, create bool) designInput {
	return designInput{
		Title:   r.Str("title", validate.S{Req: create, Trim: true, Min: 1, Max: 200}),
		Type:    r.Str("type", validate.S{Req: create, Enum: store.DesignTypes}),
		Formats: r.Str("formats", validate.S{Trim: true, Max: 500}),
		DueDate: r.Str("dueDate", validate.S{Null: true, Format: "date"}),
		Urgency: r.Str("urgency", validate.S{Enum: []string{"normal", "high"}}),
		Brief:   r.Str("brief", validate.S{Req: create, Trim: true, Min: 1, Max: 4000}),
		Refs:    r.Str("references", validate.S{Trim: true, Max: 4000}),
		EventID: r.Str("eventId", validate.S{Null: true, Min: 1, Max: 64}),
		Program: r.Str("program", validate.S{Null: true, Trim: true, Max: 200}),
	}
}

// apply copies supplied fields onto d. Blank program is stored as null.
func (in designInput) apply(d *store.DesignRequest) {
	if in.Title.Has() {
		d.Title = in.Title.V
	}
	if in.Type.Has() {
		d.Type = in.Type.V
	}
	if in.Formats.Has() {
		d.Formats = in.Formats.V
	}
	if in.DueDate.Set {
		d.DueDate = ptrIf(in.DueDate)
	}
	if in.Urgency.Has() {
		d.Urgency = in.Urgency.V
	}
	if in.Brief.Has() {
		d.Brief = in.Brief.V
	}
	if in.Refs.Has() {
		d.Refs = in.Refs.V
	}
	if in.EventID.Set {
		d.EventID = ptrIf(in.EventID)
	}
	if in.Program.Set {
		d.Program = ptrIf(in.Program)
		if d.Program != nil && *d.Program == "" {
			d.Program = nil
		}
	}
}

func eventMissing() error {
	issues := &validate.Issues{}
	issues.Add("eventId", "Event not found")
	return validationError(issues)
}

// jsonBodyFrom is jsonBody for a body already read.
func (c *Ctx) jsonBodyFrom(raw []byte, fn func(r *validate.Reader)) error {
	if !json.Valid(raw) {
		return badRequest("Expected a JSON body")
	}
	r := validate.FromJSON(raw)
	fn(r)
	if r.Issues.Any() {
		return validationError(r.Issues)
	}
	return nil
}

func (c *Ctx) designRespond(ctx context.Context, w *store.Queries, dc *designCtx, id string, out *DesignRequestDetail) error {
	r, err := w.DesignRequestByID(ctx, id)
	if err != nil {
		return err
	}
	d, err := dc.detail(ctx, w, r)
	if err != nil {
		return err
	}
	*out = d
	return nil
}

func writeDesign(c *Ctx, status int, d DesignRequestDetail, designers []UserSummary) {
	writeJSON(c.W, status, map[string]any{"request": d, "designers": designers})
}

// ─── handlers ───

func (c *Ctx) designList() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	dc, err := loadDesignCtx(ctx, q, u)
	if err != nil {
		return err
	}
	rows, err := q.DesignRequests(ctx)
	if err != nil {
		return err
	}
	comments, fileCounts, err := q.DesignCounts(ctx)
	if err != nil {
		return err
	}
	items := make([]DesignRequestItem, 0, len(rows))
	for i := range rows {
		it, err := dc.item(&rows[i], comments[rows[i].ID], fileCounts[rows[i].ID])
		if err != nil {
			return err
		}
		items = append(items, it)
	}
	writeJSON(c.W, 200, map[string]any{"items": items, "designers": dc.designerSummaries(), "viewer": DesignViewer{IsDesigner: dc.isDesigner(u.ID), CanAssign: dc.worker()}})
	return nil
}

func (c *Ctx) designDetail() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	r, err := q.DesignRequestByID(ctx, c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	dc, err := loadDesignCtx(ctx, q, u)
	if err != nil {
		return err
	}
	d, err := dc.detail(ctx, q, r)
	if err != nil {
		return err
	}
	writeDesign(c, 200, d, dc.designerSummaries())
	return nil
}

func (c *Ctx) designCreate() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	var in designInput
	if err := c.jsonBody(func(r *validate.Reader) { in = readDesign(r, true) }); err != nil {
		return err
	}
	return c.createDesign(in)
}

func (c *Ctx) createDesign(in designInput) error {
	u := c.User
	ctx := c.Context()
	now := c.S.nowMs()
	r := store.DesignRequest{ID: auth.UUID(), Urgency: "normal", Stage: "new", RequesterID: u.ID, CreatedAt: now, UpdatedAt: now}
	in.apply(&r)
	var out DesignRequestDetail
	var dc *designCtx
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		var err error
		if dc, err = loadDesignCtx(ctx, w, u); err != nil {
			return err
		}
		if r.EventID != nil && dc.cat.eventByID[*r.EventID] == nil {
			return eventMissing()
		}
		if err := w.InsertDesignRequest(ctx, r); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "design_request.created", "design_request", r.ID, nil, designSnapshot(&r)); err != nil {
			return err
		}
		return c.designRespond(ctx, w, dc, r.ID, &out)
	})
	if err != nil {
		return err
	}
	writeDesign(c, 200, out, dc.designerSummaries())
	return nil
}

// designMutate runs fn on the current row inside a write transaction, then saves, audits
// and answers with the fresh detail.
func (c *Ctx) designMutate(action string, fn func(dc *designCtx, r *store.DesignRequest) error) error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	var out DesignRequestDetail
	var dc *designCtx
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		r, err := w.DesignRequestByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		// Re-read the caller in this transaction so a role or team change is honoured.
		viewer, err := w.UserByID(ctx, u.ID)
		if err != nil || !viewer.Active {
			return forbidden("Account inactive")
		}
		if dc, err = loadDesignCtx(ctx, w, viewer); err != nil {
			return err
		}
		before := designSnapshot(r)
		if err := fn(dc, r); err != nil {
			return err
		}
		if reflect.DeepEqual(before, designSnapshot(r)) {
			// Nothing changed: answer with the current state, no write or audit row.
			return c.designRespond(ctx, w, dc, id, &out)
		}
		r.UpdatedAt = c.S.nowMs()
		if err := w.SaveDesignRequest(ctx, *r); err != nil {
			return err
		}
		if err := c.audit(ctx, w, action, "design_request", id, before, designSnapshot(r)); err != nil {
			return err
		}
		return c.designRespond(ctx, w, dc, id, &out)
	})
	if err != nil {
		return err
	}
	writeDesign(c, 200, out, dc.designerSummaries())
	return nil
}

func (c *Ctx) designUpdate() error {
	var in designInput
	if err := c.jsonBody(func(r *validate.Reader) { in = readDesign(r, false) }); err != nil {
		return err
	}
	return c.designMutate("design_request.updated", func(dc *designCtx, r *store.DesignRequest) error {
		if !dc.permissions(r).CanEdit {
			if r.Stage == "delivered" {
				return conflict("Delivered requests can no longer be edited")
			}
			return forbidden("Only the requester or a director may edit this request")
		}
		in.apply(r)
		if in.EventID.Has() && dc.cat.eventByID[in.EventID.V] == nil {
			return eventMissing()
		}
		return nil
	})
}

func (c *Ctx) designAssign() error {
	var assignee validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		assignee = r.Str("assigneeId", validate.S{Req: true, Null: true, Min: 1, Max: 64})
	}); err != nil {
		return err
	}
	return c.designMutate("design_request.assigned", func(dc *designCtx, r *store.DesignRequest) error {
		p := dc.permissions(r)
		self := assignee.Has() && assignee.V == dc.viewer.ID
		if !p.CanAssign && !(self && p.CanAssignSelf) {
			if r.Stage == "delivered" {
				return conflict("Delivered requests can no longer be reassigned")
			}
			return forbidden("Only the design team can assign requests")
		}
		if !assignee.Has() {
			if r.Stage != "new" {
				return conflict("Move the request back to New before unassigning it")
			}
			r.AssigneeID = nil
			return nil
		}
		if !dc.isDesigner(assignee.V) {
			issues := &validate.Issues{}
			issues.Add("assigneeId", "Assignee must be on the Design subteam")
			return validationError(issues)
		}
		v := assignee.V
		r.AssigneeID = &v
		return nil
	})
}

func (c *Ctx) designMove() error {
	var to string
	if err := c.jsonBody(func(r *validate.Reader) {
		to = r.Str("stage", validate.S{Req: true, Enum: store.DesignStages}).V
	}); err != nil {
		return err
	}
	return c.designMutate("design_request.moved", func(dc *designCtx, r *store.DesignRequest) error {
		if to == r.Stage {
			if !dc.worker() && !dc.permissions(r).CanRequestChanges {
				return forbidden("Only the design team can move requests")
			}
			return nil
		}
		if to == "delivered" {
			return conflict("A request is delivered when its requester approves it in review")
		}
		if !slices.Contains(dc.permissions(r).MoveTo, to) {
			if !dc.worker() {
				return forbidden("Only the design team can move requests")
			}
			return conflict("Assign a designer before moving this request")
		}
		if to != "new" && r.AssigneeID == nil {
			id := dc.viewer.ID // a designer moving an unassigned card takes it
			r.AssigneeID = &id
		}
		if r.Stage == "delivered" {
			r.ApprovedAt, r.ApprovedBy = nil, nil
		}
		r.Stage = to
		return nil
	})
}

func (c *Ctx) designApprove() error {
	return c.designMutate("design_request.approved", func(dc *designCtx, r *store.DesignRequest) error {
		if dc.viewer.ID != r.RequesterID {
			return forbidden("Only the requester can approve this request")
		}
		if r.Stage != "in-review" {
			return conflict("Only a request in review can be approved")
		}
		now, by := c.S.nowMs(), dc.viewer.ID
		r.Stage, r.ApprovedAt, r.ApprovedBy = "delivered", &now, &by
		return nil
	})
}

func (c *Ctx) designDelete() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	var keys []string
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		r, err := w.DesignRequestByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		viewer, err := w.UserByID(ctx, u.ID)
		if err != nil || !viewer.Active {
			return forbidden("Account inactive")
		}
		dc := &designCtx{viewer: viewer}
		if !dc.permissions(r).CanDelete {
			return forbidden("Only the requester can withdraw a new request")
		}
		fs, err := w.DesignFiles(ctx, id)
		if err != nil {
			return err
		}
		cs, err := w.DesignComments(ctx, id)
		if err != nil {
			return err
		}
		snap := designSnapshot(r)
		fileIDs, commentIDs := []string{}, []string{}
		for _, f := range fs {
			keys = append(keys, f.StorageKey) // local files are removed, Drive files trashed
			fileIDs = append(fileIDs, f.ID+" "+f.Filename)
		}
		for _, cm := range cs {
			commentIDs = append(commentIDs, cm.ID)
		}
		snap["files"], snap["commentIds"] = fileIDs, commentIDs
		if err := w.DeleteDesignRequest(ctx, id); err != nil {
			return err
		}
		return c.audit(ctx, w, "design_request.deleted", "design_request", id, snap, nil)
	})
	if err != nil {
		return err
	}
	for _, k := range keys {
		if derr := c.S.cfg.Files.Delete(context.WithoutCancel(ctx), k); derr != nil {
			c.S.log.Error("could not remove a design file", "key", k, "error", derr)
		}
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

func (c *Ctx) designComment() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var body string
	if err := c.jsonBody(func(r *validate.Reader) {
		body = r.Str("body", validate.S{Req: true, Trim: true, Min: 1, Max: 4000}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	var out DesignRequestDetail
	var dc *designCtx
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		if _, err := w.DesignRequestByID(ctx, id); err != nil {
			return notFoundErr("Record not found")
		}
		cm := store.DesignComment{ID: auth.UUID(), RequestID: id, AuthorID: u.ID, Body: body, CreatedAt: c.S.nowMs()}
		if err := w.InsertDesignComment(ctx, cm); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "design_request.commented", "design_request_comment", cm.ID, nil, map[string]any{"id": cm.ID, "requestId": id, "authorId": u.ID, "body": body}); err != nil {
			return err
		}
		if dc, err = loadDesignCtx(ctx, w, u); err != nil {
			return err
		}
		return c.designRespond(ctx, w, dc, id, &out)
	})
	if err != nil {
		return err
	}
	writeDesign(c, 200, out, dc.designerSummaries())
	return nil
}

func (c *Ctx) designUpload() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	q := c.S.db.Read()
	r, err := q.DesignRequestByID(ctx, id)
	if err != nil {
		return notFoundErr("Record not found")
	}
	dc, err := loadDesignCtx(ctx, q, u)
	if err != nil {
		return err
	}
	if len(dc.permissions(r).UploadKinds) == 0 { // cheap check before reading the body
		return forbidden("You cannot add files to this request")
	}
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, files.MaxBytes+1<<20)
	if err := c.R.ParseMultipartForm(8 << 20); err != nil {
		return badRequest("Expected multipart/form-data with a file")
	}
	rd := validate.FromValues(c.R.MultipartForm.Value)
	kind := rd.Str("kind", validate.S{Enum: []string{"reference", "review", "delivered"}, HasDef: true, Default: "reference"})
	if rd.Issues.Any() {
		return validationError(rd.Issues)
	}
	if !slices.Contains(dc.permissions(r).UploadKinds, kind.V) {
		return forbidden("You cannot add " + kind.V + " files to this request")
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
	name := strings.TrimSpace(fh.Filename)
	if name == "" || len(name) > 255 {
		return badRequest("File name must be 1 to 255 characters")
	}
	stored, err := c.S.cfg.Files.Put(ctx, files.PutInput{Folder: []string{"Design requests", id}, Filename: name, MimeType: mime, Body: body})
	if err != nil {
		return err
	}
	var out DesignRequestDetail
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		cur, err := w.DesignRequestByID(ctx, id)
		if err != nil {
			return notFoundErr("Record not found")
		}
		// Re-check with the caller and team as they are now (the upload may have taken a while).
		viewer, err := w.UserByID(ctx, u.ID)
		if err != nil || !viewer.Active {
			return forbidden("Account inactive")
		}
		if dc, err = loadDesignCtx(ctx, w, viewer); err != nil {
			return err
		}
		if !slices.Contains(dc.permissions(cur).UploadKinds, kind.V) {
			return forbidden("You cannot add " + kind.V + " files to this request")
		}
		df := store.DesignFile{ID: auth.UUID(), RequestID: id, Kind: kind.V, Filename: name, MimeType: mime, SizeBytes: stored.SizeBytes,
			Storage: stored.Storage, StorageKey: stored.Key, UploadedBy: u.ID, CreatedAt: c.S.nowMs()}
		if stored.WebViewLink != "" {
			l := stored.WebViewLink
			df.WebViewLink = &l
		}
		if err := w.InsertDesignFile(ctx, df); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "design_request.file", "design_request_file", df.ID, nil, map[string]any{"id": df.ID, "requestId": id, "kind": df.Kind, "filename": df.Filename, "mimeType": df.MimeType, "sizeBytes": df.SizeBytes, "storage": df.Storage, "uploadedBy": u.ID}); err != nil {
			return err
		}
		return c.designRespond(ctx, w, dc, id, &out)
	})
	if err != nil {
		if derr := c.S.cfg.Files.Delete(context.WithoutCancel(ctx), stored.Key); derr != nil {
			c.S.log.Error("could not remove an orphaned upload", "key", stored.Key, "error", derr)
		}
		return err
	}
	writeDesign(c, 200, out, dc.designerSummaries())
	return nil
}

func (c *Ctx) designFileDownload() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	f, err := c.S.db.Read().DesignFileByID(ctx, c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	if f.Storage == "drive" {
		if f.WebViewLink == nil {
			return notFoundErr("Record not found")
		}
		http.Redirect(c.W, c.R, *f.WebViewLink, http.StatusFound)
		return nil
	}
	file, err := c.S.cfg.Files.Get(ctx, f.StorageKey)
	if err != nil {
		return err
	}
	if file == nil {
		return notFoundErr("Record not found")
	}
	h := c.W.Header()
	h.Set("Content-Type", file.MimeType)
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+encodeRFC5987(f.Filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	_, _ = c.W.Write(file.Body)
	return nil
}

// ─── prefill from an event (used by the Events page's "Request design") ───

// designDraftFor proposes a request for an event: due a week before it starts (in
// Vancouver time), never before today.
func (s *Server) designDraftFor(e *store.Event) DesignRequestDraft {
	d := DesignRequestDraft{Title: e.Name + " promo", Type: "social", Urgency: "normal", EventID: e.ID, Event: DesignEventRef{ID: e.ID, Name: e.Name, StartsAt: e.StartsAt}}
	when := ""
	if t, err := luma.ParseInstant(e.StartsAt); err == nil {
		p := t.In(fiscal.Pacific)
		when = p.Format("Mon Jan 2, 3:04 PM")
		// A past event gets no due date; otherwise a week ahead, but never before today.
		if t.After(s.now()) {
			due := fiscal.PacificDate(t.Add(-7 * 24 * time.Hour))
			if today := fiscal.PacificDate(s.now()); due < today {
				due = today
			}
			d.DueDate = &due
		}
	}
	brief := "Promote " + e.Name
	if when != "" {
		brief += " on " + when
	}
	if e.Location != nil && strings.TrimSpace(*e.Location) != "" {
		brief += " at " + strings.TrimSpace(*e.Location)
	}
	d.Brief = brief + ". Include the date, time, place and RSVP link."
	if e.URL != nil && *e.URL != "" {
		d.Brief += fmt.Sprintf(" RSVP: %s", *e.URL)
	}
	d.Title = truncRunes(d.Title, 200)
	d.Brief = truncRunes(d.Brief, 4000)
	return d
}

func (c *Ctx) designDraftFromEvent() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	e, err := c.S.db.Read().EventByID(c.Context(), c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	writeJSON(c.W, 200, map[string]any{"draft": c.S.designDraftFor(e)})
	return nil
}

// designCreateFromEvent creates a request linked to the event. The body may override any
// field of the draft; an empty body takes the draft as is.
func (c *Ctx) designCreateFromEvent() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	e, err := c.S.db.Read().EventByID(c.Context(), c.R.PathValue("id"))
	if err != nil {
		return notFoundErr("Record not found")
	}
	var in designInput
	raw, err := readBody(c, 1<<20)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := c.jsonBodyFrom(raw, func(r *validate.Reader) { in = readDesign(r, false) }); err != nil {
			return err
		}
	}
	draft := c.S.designDraftFor(e)
	fill := func(f *validate.Field[string], v string) {
		if !f.Has() {
			*f = validate.Field[string]{V: v, Set: true}
		}
	}
	fill(&in.Title, draft.Title)
	fill(&in.Type, draft.Type)
	fill(&in.Brief, draft.Brief)
	in.EventID = validate.Field[string]{V: e.ID, Set: true}
	if !in.DueDate.Set && draft.DueDate != nil {
		in.DueDate = validate.Field[string]{V: *draft.DueDate, Set: true}
	}
	return c.createDesign(in)
}

// truncRunes cuts s to at most n characters without splitting a UTF-8 sequence.
func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}
