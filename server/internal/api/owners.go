package api

import (
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// Re-read authorization inside each write transaction: another owner may have
// removed the caller after middleware loaded their session.
func (c *Ctx) ownerIn(q *store.Queries) (*store.User, error) {
	if _, err := c.officer(); err != nil {
		return nil, err
	}
	u, err := q.UserByID(c.Context(), c.User.ID)
	if err != nil {
		return nil, err
	}
	if !u.Active || !u.IsOwner {
		return nil, forbidden("Requires dashboard owner")
	}
	return u, nil
}
func (c *Ctx) ownerAdd() error    { return c.ownerChange(true) }
func (c *Ctx) ownerRemove() error { return c.ownerChange(false) }
func (c *Ctx) ownerChange(add bool) error {
	if c.User == nil || !c.User.IsOwner {
		return forbidden("Requires dashboard owner")
	}
	ctx := c.Context()
	var out AdminUser
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		if _, err := c.ownerIn(q); err != nil {
			return err
		}
		target, err := q.UserByID(ctx, c.R.PathValue("userId"))
		if err != nil {
			return err
		}
		if !target.Active {
			return badRequest("Owner must be active")
		}
		if target.IsOwner == add {
			out = adminUser(target)
			return nil
		}
		if err := q.SetOwner(ctx, target.ID, add, c.S.nowMs()); err != nil {
			return err
		}
		if target.Role != "leadership" {
			if err := q.DeleteUserSessions(ctx, target.ID); err != nil {
				return err
			}
		}
		updated, err := q.UserByID(ctx, target.ID)
		if err != nil {
			return err
		}
		action := "owner.removed"
		if add {
			action = "owner.added"
		}
		if err := c.audit(ctx, q, action, "user", target.ID, userSnapshot(target), userSnapshot(updated)); err != nil {
			return err
		}
		out = adminUser(updated)
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"user": out})
	return nil
}
func (c *Ctx) ownerTransfer() error {
	if c.User == nil || !c.User.IsOwner {
		return forbidden("Requires dashboard owner")
	}
	var targetID string
	if err := c.jsonBody(func(r *validate.Reader) { targetID = r.Str("toUserId", validate.S{Req: true, Min: 1, Max: 64}).V }); err != nil {
		return err
	}
	if targetID == c.User.ID {
		return badRequest("Choose another user to transfer ownership")
	}
	ctx := c.Context()
	out := []AdminUser{}
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		caller, err := c.ownerIn(q)
		if err != nil {
			return err
		}
		target, err := q.UserByID(ctx, targetID)
		if err != nil {
			return err
		}
		if !target.Active {
			return badRequest("Owner must be active")
		}
		// Establish the next owner before removing the caller. Failure anywhere,
		// including audit writes, rolls back both changes and session invalidation.
		if err := q.SetOwner(ctx, target.ID, true, c.S.nowMs()); err != nil {
			return err
		}
		if err := q.SetOwner(ctx, caller.ID, false, c.S.nowMs()); err != nil {
			return err
		}
		if target.Role != "leadership" {
			if err := q.DeleteUserSessions(ctx, target.ID); err != nil {
				return err
			}
		}
		for _, before := range []*store.User{target, caller} {
			after, err := q.UserByID(ctx, before.ID)
			if err != nil {
				return err
			}
			if err := c.audit(ctx, q, "owner.transferred", "user", before.ID, userSnapshot(before), userSnapshot(after)); err != nil {
				return err
			}
			out = append(out, adminUser(after))
		}
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"users": out})
	return nil
}
