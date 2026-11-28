package api

import (
	"context"
	"errors"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// The access email is where Cloudflare Access will send a person's sign-in PIN once the
// Access sync is turned on (a later release). Until then it is only stored. Directors type
// it when adding or inviting someone; it is never read from the officer's own profile or
// the calendar email, and officers never set or see it, their own included.

// accessEmailField reads the optional accessEmail body field: trimmed, lower-cased, null clears.
func accessEmailField(r *validate.Reader) validate.Field[string] {
	f := r.Str("accessEmail", validate.S{Null: true, Trim: true, Format: "email", Max: 254})
	if f.Has() {
		f.V = strings.ToLower(f.V)
	}
	return f
}

// accessEmailPtr is the stored value for a field read by accessEmailField.
func accessEmailPtr(f validate.Field[string]) *string {
	if !f.Has() {
		return nil
	}
	v := f.V
	return &v
}

// checkAccessEmailFree refuses an email another account or pending invite already uses.
// discordID is the person being saved, whose own account or invite never clashes. Only
// directors reach this, and directors may see every account and invite, so naming the
// holder tells them nothing they couldn't already read.
func checkAccessEmailFree(ctx context.Context, q *store.Queries, email *string, discordID string) error {
	if email == nil {
		return nil
	}
	kind, name, err := q.AccessEmailHolder(ctx, *email, discordID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if kind == "invite" {
		return conflict("That access email is on the pending invite for " + name + ". Use a different email, or change or revoke that invite first.")
	}
	return conflict("That access email already belongs to " + name + ". Each person needs their own.")
}

// canSetAccessEmail applies who may change whose access email: directors for officers,
// leadership for directors and leadership, and only an owner for an owner. It reloads the
// caller inside the write, like the other admin checks.
func (c *Ctx) canSetAccessEmail(q *store.Queries, target *store.User) error {
	if target.IsOwner {
		_, err := c.ownerIn(q)
		return err
	}
	if auth.HasRole(target.Role, auth.RoleDirector) {
		_, err := c.requireLeadershipIn(q)
		return err
	}
	return nil
}

func sameEmail(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
