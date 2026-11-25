package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdServeResolvesFileSecrets(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "oauth-secret")
	if err := os.WriteFile(secret, []byte("synthetic-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"DISCORD_CLIENT_ID": "test-client", "DISCORD_CLIENT_SECRET_FILE": secret, "DB_PATH": filepath.Join(dir, "app.db"), "APP_URL": "https://example.test", "ADDR": "invalid-listen-address"}
	var out, logs bytes.Buffer
	// The real systemd entry point builds OAuth configuration and then fails at listen.
	if code := run([]string{"serve"}, env(values), &out, &logs); code != 1 {
		t.Fatalf("unexpected exit %d", code)
	}
	if strings.Contains(logs.String(), "Discord sign-in is not configured") || !strings.Contains(logs.String(), "missing port") {
		t.Fatalf("serve did not wire file OAuth: %s", logs.String())
	}
	if os.Getenv("DISCORD_CLIENT_SECRET") == "synthetic-secret" {
		t.Fatal("secret exported")
	}
	for _, value := range []string{"", "direct-conflict"} {
		values["DISCORD_CLIENT_SECRET"] = value
		values["DISCORD_CLIENT_SECRET_FILE"] = filepath.Join(dir, "missing")
		logs.Reset()
		if code := run([]string{"serve"}, env(values), &out, &logs); code != 1 || !strings.Contains(logs.String(), "secret file configuration") {
			t.Fatalf("invalid file sources accepted: %s", logs.String())
		}
	}
}

func TestAllApplicationFileCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(path, []byte("test-value\n"), 0600)
	for _, key := range []string{"DISCORD_CLIENT_SECRET", "DISCORD_WEBHOOK_URL", "DISCORD_BOT_TOKEN", "SMTP_USERNAME", "SMTP_PASSWORD", "GOOGLE_OAUTH_CLIENT_SECRET", "CLOUDFLARE_API_TOKEN", "GOOGLE_SERVICE_ACCOUNT_JSON", "DEV_AUTH_KEY"} {
		get, err := fileSettings(env(map[string]string{key + "_FILE": path}))
		if err != nil || get(key) != "test-value" {
			t.Fatalf("%s: %v", key, err)
		}
	}
}
