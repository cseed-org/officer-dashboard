package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// leadershipCmd is explicit operator provisioning for upgrades with no leadership
// account. It does not silently promote any existing director on startup.
func leadershipCmd(args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("provision-leadership", flag.ContinueOnError)
	var discordID string
	fs.StringVar(&discordID, "discord-id", "", "existing active owner's Discord ID (required)")
	cfg, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	discordID = strings.TrimSpace(discordID)
	if !validate.IsDiscordID(discordID) {
		return fmt.Errorf("--discord-id must be a 17–20 digit Discord ID")
	}
	db, err := openDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	err = db.Write(ctx, func(q *store.Queries) error {
		user, err := q.UserByDiscordID(ctx, discordID)
		if err != nil {
			return err
		}
		if !user.Active {
			return fmt.Errorf("cannot provision an inactive user")
		}
		if user.Role == auth.RoleLeadership {
			return nil
		}
		if err := q.UpdateUser(ctx, user.ID, store.UserPatch{Role: store.Opt[string]{V: auth.RoleLeadership, Set: true}}, time.Now().UnixMilli()); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, user.ID); err != nil {
			return err
		}
		before, _ := json.Marshal(map[string]string{"role": user.Role})
		after := `{"role":"leadership","source":"operator CLI"}`
		prior := string(before)
		return q.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), Action: "user.provision_leadership", EntityType: "user", EntityID: &user.ID, BeforeJSON: &prior, AfterJSON: &after, CreatedAt: store.Ms(time.Now().UnixMilli())})
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Leadership provisioned for Discord ID %s\n", discordID)
	return nil
}
