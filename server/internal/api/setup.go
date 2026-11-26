package api

import (
	"fmt"
	"net/http"
	"time"
)

// SetupConfig records presence, never credential values or secret file paths.
type SetupConfig struct{ BootstrapOwner, DiscordWebhook bool }

type setupIntegration struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Action string `json:"action"`
	Docs   string `json:"docs"`
}

const setupDocs = "https://github.com/cseed-org/officer-dashboard/blob/main/docs/configuration.md"

func (c *Ctx) setupStatus() error {
	if _, err := c.ownerIn(c.S.db.Read()); err != nil {
		return err
	}
	s := c.S
	history, err := s.db.Read().SetupHistory(c.Context())
	if err != nil {
		return err
	}
	entries := []setupIntegration{}
	add := func(key, label string, present bool, hint string) {
		state := "missing"
		if present {
			state = "configured"
		}
		entries = append(entries, setupIntegration{key, label, state, hint, "env", setupDocs})
	}
	drive := s.driveStatusBody(c.Context())
	state, detail := "missing", "Connect a Google account and choose a shared drive folder."
	if drive["connected"] == true {
		detail = fmt.Sprintf("Connected as %s. Choose a root folder.", drive["email"])
		if drive["rootReachable"] == true && drive["canWrite"] == true {
			state, detail = "connected", fmt.Sprintf("%s ; %s", drive["email"], drive["rootName"])
		} else if drive["rootFolderId"] != "" {
			state, detail = "error", "Root folder is unreachable or cannot accept files. Check Drive access below."
		}
	}
	if drive["needsReconnect"] == true {
		state, detail = "error", "Google Drive access expired or was revoked. Reconnect below."
	}
	if drive["configured"] != true && drive["connected"] == true {
		state, detail = "error", "Google OAuth configuration is missing. Restore the host secrets to use this account."
	}
	if usage, ok := drive["usage"].(map[string]any); ok {
		detail += fmt.Sprintf(" ; %v%% of DRIVE_QUOTA_BYTES used", usage["percent"])
	}
	detail += " ; Last export: " + setupRun(history.ExportInstalled, history.LastExport)
	// Backup state is not installed on this stack; the sidecar does not publish a DB status yet.
	detail += " ; Last backup upload: not installed ; Outbox age: not installed"
	entries = append(entries, setupIntegration{"drive", "Google Drive", state, detail, "connect", setupDocs})
	email, reconnect, last, err := c.orgGoogleState()
	if err != nil {
		return err
	}
	state, detail = "missing", "Connect the organization's Google account."
	if email != nil {
		state, detail = "connected", "Connected as "+*email
	}
	if reconnect || last != "" {
		state, detail = "error", "Calendar sync needs attention. Reconnect or retry below."
	}
	if s.cfg.Google == nil {
		state, detail = "missing", "Set GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET_FILE in host configuration."
	}
	entries = append(entries, setupIntegration{"org-calendar", "Org Google Calendar", state, detail, "connect", setupDocs})
	add("discord-login", "Discord login", s.cfg.Discord != nil, "Set DISCORD_CLIENT_ID and DISCORD_CLIENT_SECRET_FILE in host configuration.")
	add("discord-bot", "Discord bot", s.cfg.DM != nil, "Set DISCORD_BOT_TOKEN_FILE in host secrets.")
	add("discord-webhook", "Discord notifications", s.cfg.Setup.DiscordWebhook, "Set DISCORD_WEBHOOK_URL_FILE in host secrets.")
	add("smtp", "SMTP email", s.cfg.Mailer != nil, "Set SMTP_HOST and SMTP_FROM; use SMTP_USERNAME_FILE and SMTP_PASSWORD_FILE in host secrets.")
	cf := s.cfg.Cloudflare != nil && s.cfg.Cloudflare.Client != nil && s.cfg.Cloudflare.Client.Config.Token != ""
	add("cloudflare", "Cloudflare sync", cf, "Set CLOUDFLARE_API_TOKEN_FILE in host secrets and the account/resource IDs. Last sync: "+setupRun(true, history.LastCloudflare))
	entries = append(entries, setupIntegration{"d1-backups", "D1 backups", "configured", "Managed by the backup container. Last D1 upload: unavailable in dashboard status.", "none", setupDocs})
	add("bootstrap-owner", "Bootstrap owner", s.cfg.Setup.BootstrapOwner, "Set BOOTSTRAP_OWNER_DISCORD_ID and BOOTSTRAP_OWNER_NAME in host configuration. Existing ownership survives restarts.")
	add("sealing-key", "OAuth sealing key", s.sealer != nil, "Derived from GOOGLE_OAUTH_CLIENT_SECRET_FILE in host secrets; keep it stable across restarts.")
	c.W.Header().Set("Cache-Control", "no-store")
	writeJSON(c.W, http.StatusOK, map[string]any{"integrations": entries})
	return nil
}

func setupRun(installed bool, ms int64) string {
	if !installed {
		return "not installed"
	}
	if ms == 0 {
		return "never"
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
