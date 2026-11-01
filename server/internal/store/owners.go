package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/validate"
)

func (q *Queries) Owners(ctx context.Context) ([]User, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE is_owner=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}
func (q *Queries) SetOwner(ctx context.Context, id string, owner bool, now int64) error {
	if owner {
		return q.Exec(ctx, `UPDATE users SET is_owner=1,role='leadership',updated_at=? WHERE id=?`, now, id)
	}
	return q.Exec(ctx, `UPDATE users SET is_owner=0,updated_at=? WHERE id=?`, now, id)
}

// BootstrapOwner provisions only while the database has no owner. Once ownership
// exists, including after transfer, configuration cannot take it back.
// mismatch tells the caller to warn that the configured ID is no longer an owner.
func (d *DB) BootstrapOwner(ctx context.Context, discordID, name string, now int64) (mismatch bool, err error) {
	discordID = strings.TrimSpace(discordID)
	name = strings.TrimSpace(name)
	if discordID == "" {
		return false, nil
	}
	err = d.Write(ctx, func(q *Queries) error {
		owners, err := q.Owners(ctx)
		if err != nil {
			return err
		}
		if len(owners) > 0 {
			mismatch = true
			for _, u := range owners {
				if u.DiscordID == discordID {
					mismatch = false
				}
			}
			return nil
		}
		if !validate.IsDiscordID(discordID) {
			return fmt.Errorf("BOOTSTRAP_OWNER_DISCORD_ID must be a 17–20 digit Discord ID")
		}
		user, err := q.UserByDiscordID(ctx, discordID)
		var before *string
		switch {
		case errors.Is(err, ErrNotFound):
			if name == "" {
				name = "Dashboard owner"
			}
			user = &User{ID: auth.UUID(), DiscordID: discordID, Name: name, Role: auth.RoleLeadership, Active: true, IsOwner: true, CreatedAt: Ms(now), UpdatedAt: Ms(now)}
			if err := q.InsertUser(ctx, *user); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			raw, _ := json.Marshal(map[string]any{"role": user.Role, "active": user.Active, "isOwner": user.IsOwner, "name": user.Name})
			s := string(raw)
			before = &s
			if name == "" {
				name = user.Name
			}
			if err := q.Exec(ctx, `UPDATE users SET role='leadership',active=1,is_owner=1,name=?,updated_at=? WHERE id=?`, name, now, user.ID); err != nil {
				return err
			}
			if err := q.DeleteUserSessions(ctx, user.ID); err != nil {
				return err
			}
		}
		afterRaw, _ := json.Marshal(map[string]any{"role": "leadership", "active": true, "isOwner": true, "name": name})
		after := string(afterRaw)
		return q.InsertAudit(ctx, AuditEntry{ID: auth.UUID(), Action: "owner.bootstrap", EntityType: "user", EntityID: &user.ID, BeforeJSON: before, AfterJSON: &after, CreatedAt: Ms(now)})
	})
	return mismatch, err
}
