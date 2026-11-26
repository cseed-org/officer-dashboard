package api

import (
	"context"
	"encoding/json"
	"errors"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/cfsync"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

func TestSetupStatusOwnerOnly(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "officer", e.get("/api/setup/status", e.officer()), 403)
	expectStatus(t, "director", e.get("/api/setup/status", e.director()), 403)
	expectStatus(t, "owner", e.get("/api/setup/status", e.as("user-leadership")), 200)
}

func TestSetupStatusSafeConfiguredAndConnectedMetadata(t *testing.T) {
	e, _, _ := driveEnv(t)
	owner := connectDrive(t, e)
	r := e.get("/api/setup/status", owner)
	expectStatus(t, "connected owner", r, 200)
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("setup metadata must not be cached")
	}
	var body struct {
		Integrations []struct{ Key, State, Detail, Action, Docs string }
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		t.Fatal(err)
	}
	byKey := map[string]string{}
	for _, i := range body.Integrations {
		byKey[i.Key] = i.State
		if i.Docs == "" || i.Detail == "" {
			t.Fatalf("incomplete entry: %s", i.Key)
		}
	}
	if len(byKey) != 10 || byKey["drive"] != "connected" || byKey["org-calendar"] != "missing" || byKey["sealing-key"] != "configured" {
		t.Fatalf("unexpected states: %v", byKey)
	}
	for _, forbidden := range []string{"synthetic-client-secret", "sealed_refresh", "sealed_access", "access_token", "refresh_token", "discord.com/api/webhooks/", "synthetic-refresh", "synthetic-access", "fake-access-token", "fake-refresh-token"} {
		if strings.Contains(string(r.Raw), forbidden) {
			t.Fatalf("setup response leaked %s", forbidden)
		}
	}
	if !strings.Contains(string(r.Raw), "Last export: not installed") {
		t.Fatal("missing integration must be explicit")
	}
}

func TestSetupStatusConfigurationAndOptionalExport(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Setup = SetupConfig{BootstrapOwner: true, DiscordWebhook: true}
		c.Notifier = notify.NewDiscord("https://example.invalid/synthetic-webhook")
		c.Discord = auth.NewDiscord("synthetic-client", "synthetic-discord-value", "http://localhost/callback")
		c.DM = notify.NewDiscordBot("synthetic-bot-value")
		c.Mailer = &calendar.SMTP{Host: "smtp.invalid", Sender: "test@example.invalid", Password: "synthetic-smtp-value"}
		c.Cloudflare = &cfsync.Service{Client: cloudflare.New(cloudflare.Config{Token: "synthetic-cloudflare-value"})}
	})
	if err := e.db.Write(context.Background(), func(q *store.Queries) error {
		return q.Exec(context.Background(), `CREATE TABLE drive_export_state(path TEXT PRIMARY KEY, exported_at INTEGER);
   INSERT INTO drive_export_state VALUES('event.json',1795550400000);
   INSERT INTO cloudflare_sync_state(kind,last_success) VALUES('members',1795550400000);`)
	}); err != nil {
		t.Fatal(err)
	}
	r := e.get("/api/setup/status", e.as("user-leadership"))
	expectStatus(t, "configuration", r, 200)
	var body struct {
		Integrations []struct{ Key, State, Detail string }
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		t.Fatal(err)
	}
	for _, i := range body.Integrations {
		if i.Key != "drive" && i.Key != "org-calendar" && i.Key != "sealing-key" && i.State != "configured" {
			t.Fatalf("%s: %s", i.Key, i.State)
		}
		if i.Key == "drive" && strings.Contains(i.Detail, "Last export: not installed") {
			t.Fatal("export timestamp not read")
		}
		if i.Key == "cloudflare" && strings.Contains(i.Detail, "Last sync: never") {
			t.Fatal("sync timestamp not read")
		}
	}
	for _, v := range []string{"synthetic-discord-value", "synthetic-bot-value", "synthetic-smtp-value", "synthetic-cloudflare-value", "https://example.invalid/synthetic-webhook"} {
		if strings.Contains(string(r.Raw), v) {
			t.Fatal("credential in response")
		}
	}
}

func TestSetupIsAnAllowedLoginDestination(t *testing.T) {
	if got := auth.SafeNext("/setup"); got != "/setup" {
		t.Fatalf("setup login destination: %s", got)
	}
}

func TestSetupOrgCalendarStatePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		connected, configured, syncError bool
		state, detail                    string
	}{
		{"missing configuration", false, false, false, "missing", "Set GOOGLE_OAUTH_CLIENT_ID"},
		{"connected without configuration", true, false, false, "error", "Restore the host secrets"},
		{"sync error without configuration", true, false, true, "error", "Restore the host secrets"},
		{"connected and configured", true, true, false, "connected", "org@example.invalid"},
		{"sync error", true, true, true, "error", "Calendar sync needs attention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, func(c *Config) {
				if tc.configured {
					c.Google = calendar.NewGoogle("synthetic-client", "synthetic-google-secret", "http://localhost/callback")
				}
			})
			if tc.connected {
				if err := e.db.Write(context.Background(), func(q *store.Queries) error {
					return q.SaveOrgGoogleAccount(context.Background(), store.OrgGoogleAccount{Subject: "synthetic-sub", Email: "org@example.invalid", SealedRefresh: "synthetic-sealed"})
				}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.syncError {
				e.srv.recordOrgSync(errors.New("synthetic sync failure"))
			}
			r := e.mustOK(e.get("/api/setup/status", e.as("user-leadership")))
			for _, v := range r.list("integrations") {
				item := v.(map[string]any)
				if item["key"] == "org-calendar" {
					if item["state"] != tc.state || !strings.Contains(item["detail"].(string), tc.detail) {
						t.Fatalf("unexpected org calendar state: %v", item)
					}
					return
				}
			}
			t.Fatal("org calendar card missing")
		})
	}
}

func TestSetupConnectedAccountsWithoutEmailHaveReadableLabels(t *testing.T) {
	e, _, _ := driveEnv(t)
	owner := connectDrive(t, e)
	if err := e.db.Write(context.Background(), func(q *store.Queries) error {
		if err := q.Exec(context.Background(), `UPDATE drive_google_account SET email=''`); err != nil {
			return err
		}
		return q.SaveOrgGoogleAccount(context.Background(), store.OrgGoogleAccount{Subject: "synthetic-org", SealedRefresh: "synthetic-sealed"})
	}); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/setup/status", owner))
	for _, v := range r.list("integrations") {
		item := v.(map[string]any)
		if item["key"] == "drive" || item["key"] == "org-calendar" {
			if !strings.Contains(item["detail"].(string), "the connected Google account") || strings.Contains(item["detail"].(string), "<nil>") {
				t.Fatalf("unreadable account: %v", item)
			}
		}
	}
}
