package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/cfsync"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

func (c *Ctx) memberFilter() (store.MemberFilter, error) {
	f := store.MemberFilter{}
	err := c.query(func(r *validate.Reader) {
		f.Search = r.Str("q", validate.S{Trim: true, Max: 200}).V
		f.Campus = r.Str("campus", validate.S{Max: 100}).V
		f.GradYear = r.Str("gradYear", validate.S{Max: 20}).V
		f.Officer = r.Str("officer", validate.S{Enum: []string{"yes", "no"}}).V
		f.Limit = int(r.Int("limit", validate.I{Min: validate.Lo(1), Max: validate.Lo(200), HasDef: true, Def: 50}).V)
		f.Offset = int(r.Int("offset", validate.I{Min: validate.Lo(0), Max: validate.Lo(1000000), HasDef: true, Def: 0}).V)
	})
	return f, err
}
func (c *Ctx) membersList() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	f, err := c.memberFilter()
	if err != nil {
		return err
	}
	tx, err := c.S.db.R.BeginTx(c.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := store.On(tx)
	items, total, err := q.Members(c.Context(), f)
	if err != nil {
		return err
	}
	facets, err := q.MemberFacets(c.Context())
	if err != nil {
		return err
	}
	last, msg, err := q.SyncStatus(c.Context(), "members")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	for i := range items {
		items[i].Demographics = nil
	}
	c.W.Header().Set("Cache-Control", "no-store")
	writeJSON(c.W, 200, map[string]any{"items": items, "total": total, "limit": f.Limit, "offset": f.Offset, "connected": c.S.cfg.Cloudflare.Client.D1Connected(), "lastSync": last, "syncError": msg, "facets": facets})
	return nil
}
func (c *Ctx) memberDetail() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	p, err := c.S.db.Read().Member(c.Context(), c.R.PathValue("id"))
	if err != nil {
		return err
	}
	c.W.Header().Set("Cache-Control", "no-store")
	p.Demographics = nil
	writeJSON(c.W, 200, map[string]any{"member": p})
	return nil
}
func (c *Ctx) cloudflareSync() error {
	u, err := c.director()
	if err != nil {
		return err
	}
	if err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		return c.audit(c.Context(), q, "cloudflare.sync_requested", "cloudflare", "", nil, nil)
	}); err != nil {
		return err
	}
	syncCtx, cancel := context.WithTimeout(c.Context(), 45*time.Second)
	defer cancel()
	err = c.S.cfg.Cloudflare.Run(syncCtx, &u.ID)
	if errors.Is(err, cfsync.ErrBusy) {
		return fail(409, "conflict", "A sync or member update is already running")
	}
	if err != nil {
		return fail(502, "internal", "Some Cloudflare sources could not sync. Previous data is retained; check each widget for status.")
	}
	writeJSON(c.W, 200, map[string]any{"ok": true, "connected": c.S.cfg.Cloudflare.Client.D1Connected()})
	return nil
}
func (c *Ctx) memberUpdate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	svc := c.S.cfg.Cloudflare
	if !svc.Client.D1Connected() {
		return fail(503, "internal", "Cloudflare D1 is not connected")
	}
	if !svc.Lock() {
		return fail(409, "conflict", "A sync or member update is already running")
	}
	defer svc.Unlock()
	id := c.R.PathValue("id")
	before, err := c.S.db.Read().Member(c.Context(), id)
	if err != nil {
		return err
	}
	columns := []string{}
	params := []any{}
	changes := map[string]any{}
	note := ""
	err = c.jsonBody(func(r *validate.Reader) {
		note = r.Str("auditNote", validate.S{Req: true, Trim: true, Min: 1, Max: 2000}).V
		for _, key := range []string{"first_name", "last_name", "preferred_name", "uw_email", "cs_email", "personal_email", "major", "grad_year", "campus", "pronouns"} {
			required := key == "first_name" || key == "last_name"
			f := r.Str(key, validate.S{Trim: true, Max: 254, Null: !required})
			if !f.Set {
				continue
			}
			if required && (f.Null || f.V == "") {
				r.Issues.Add(key, "Name is required")
				continue
			}
			var value any = f.V
			if !required && (f.Null || f.V == "") {
				value = nil
			}
			if strings.HasSuffix(key, "email") && value != nil {
				a, e := mail.ParseAddress(f.V)
				if e != nil || a.Address != f.V {
					r.Issues.Add(key, "Enter a valid email address")
				} else {
					domain := ""
					if key == "uw_email" {
						domain = "@uw.edu"
					}
					if key == "cs_email" {
						domain = "@cs.washington.edu"
					}
					if domain != "" && !strings.HasSuffix(strings.ToLower(f.V), domain) {
						r.Issues.Add(key, "Email must end in "+domain)
					}
				}
			}
			if value == nil {
				columns = append(columns, key+"=NULL")
			} else {
				columns = append(columns, key+"=?")
				params = append(params, value)
			}
			changes[key] = value
		}
		for _, key := range []string{"join_date", "has_been_cseed_officer", "person_id", "agreed_to_membership_agreement", "demographics"} {
			if r.Str(key, validate.S{Null: true}).Set {
				r.Issues.Add(key, "This field is read-only")
			}
		}
		if len(columns) == 0 {
			r.Issues.AddForm("Change at least one editable field")
		}
	})
	if err != nil {
		return err
	}
	// Recheck active role and commit intent before making the external mutation.
	operation := auth.UUID()
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		u, e := q.UserByID(c.Context(), c.User.ID)
		if e != nil {
			return e
		}
		if !u.Active || !auth.HasRole(u.Role, auth.RoleDirector) {
			return forbidden("Requires director role")
		}
		return c.audit(c.Context(), q, "member.update_requested", "member", id, before, map[string]any{"operation": operation, "auditNote": note, "changes": changes})
	})
	if err != nil {
		return err
	}
	params = append(params, id)
	result, remoteErr := svc.Client.Query(c.Context(), "UPDATE People SET "+strings.Join(columns, ",")+" WHERE person_id=? RETURNING *", params...)
	var after cloudflare.Person
	if remoteErr == nil {
		if len(result[0].Results) != 1 {
			remoteErr = errors.New("D1 member no longer exists")
		} else {
			remoteErr = json.Unmarshal(result[0].Results[0], &after)
		}
	}
	action := "member.updated"
	if remoteErr != nil {
		action = "member.update_unconfirmed"
		if errors.Is(remoteErr, cloudflare.ErrRejected) {
			action = "member.update_failed"
		}
	}
	var auditMember any
	if remoteErr == nil {
		auditMember = after
	}
	// Finish the audit even if the caller disconnected after D1 committed.
	ctx := c.R.Context()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		if remoteErr == nil {
			if e := q.SaveMember(ctx, after); e != nil {
				return e
			}
		}
		return c.audit(ctx, q, action, "member", id, before, map[string]any{"operation": operation, "auditNote": note, "changes": changes, "member": auditMember})
	})
	if err != nil {
		return fail(502, "internal", "D1 may have saved the change. Sync before retrying; the audit intent was recorded.")
	}
	if errors.Is(remoteErr, cloudflare.ErrRejected) {
		return fail(502, "internal", "Cloudflare D1 rejected the update. Nothing was saved; check the values and D1 edit permission.")
	}
	if remoteErr != nil {
		return fail(502, "internal", "D1 did not confirm the update. Your edits are kept. Sync before retrying.")
	}
	after.Demographics = nil
	writeJSON(c.W, 200, map[string]any{"member": after})
	return nil
}

// Spreadsheet formulas are neutralized in every text cell.
func csvCell(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if (len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))) || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") {
		return "'" + s
	}
	return s
}

func (c *Ctx) membersExport() error {
	if _, err := c.director(); err != nil {
		return err
	}
	f, err := c.memberFilter()
	if err != nil {
		return err
	}
	// One read transaction gives a consistent export while daily sync replaces the mirror.
	tx, err := c.S.db.R.BeginTx(c.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Use the typed query method on a read transaction.
	q := store.On(tx)
	f.Limit = -1
	f.Offset = 0
	items, _, err := q.Members(c.Context(), f)
	if err != nil {
		return err
	}
	if err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		return c.audit(c.Context(), q, "members.exported", "members", "", nil, map[string]any{"count": len(items), "filters": f})
	}); err != nil {
		return err
	}
	c.W.Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.W.Header().Set("Content-Disposition", `attachment; filename="cseed-members.csv"`)
	c.W.Header().Set("Cache-Control", "no-store")
	w := csv.NewWriter(c.W)
	w.UseCRLF = true
	if err = w.Write([]string{"Person ID", "First name", "Last name", "Preferred name", "UW email", "CS email", "Personal email", "Major", "Graduation year", "Campus", "Join date", "Has been officer"}); err != nil {
		return err
	}
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, p := range items {
		row := []string{p.ID, p.FirstName, p.LastName, val(p.PreferredName), val(p.UWEmail), val(p.CSEmail), val(p.PersonalEmail), val(p.Major), val(p.GradYear), val(p.Campus), p.JoinDate, fmt.Sprint(p.HasBeenOfficer == 1)}
		for i := range row {
			row[i] = csvCell(row[i])
		}
		if err = w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
func (c *Ctx) cloudflareWidgets() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	out := []map[string]any{}
	for _, kind := range []string{"web-analytics", "traffic-security", "performance", "workers-d1", "member-signups"} {
		source := kind
		connected := c.S.cfg.Cloudflare.Client.AnalyticsConnectedFor(kind)
		if kind == "member-signups" {
			source = "members"
			connected = c.S.cfg.Cloudflare.Client.D1Connected()
		}
		last, msg, err := c.S.db.Read().SyncStatus(c.Context(), source)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		var data any
		if connected && last != nil {
			if kind == "member-signups" {
				counts, e := c.S.db.Read().MemberSignupCounts(c.Context(), *last)
				if e != nil {
					return e
				}
				data = counts
			} else {
				raw, e := c.S.db.Read().Snapshot(c.Context(), kind)
				if e != nil && !errors.Is(e, store.ErrNotFound) {
					return e
				}
				if e == nil {
					data = raw
				}
			}
		}
		out = append(out, map[string]any{"id": kind, "connected": connected, "lastSync": last, "error": msg, "data": data})
	}
	c.W.Header().Set("Cache-Control", "no-store")
	writeJSON(c.W, http.StatusOK, map[string]any{"widgets": out})
	return nil
}
