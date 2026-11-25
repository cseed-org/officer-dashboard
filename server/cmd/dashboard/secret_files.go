package main

import (
	"errors"
	"io"
	"os"
	"strings"
)

// Resolve service credentials in memory for both direct systemd and container entry points.
func fileSettings(getenv func(string) string) (func(string) string, error) {
	if getenv("R2_SECRET_ACCESS_KEY") != "" {
		return nil, errors.New("R2 secret must use R2_SECRET_ACCESS_KEY_FILE")
	}
	if getenv("R2_ANALYTICS_TOKEN") != "" {
		return nil, errors.New("R2 analytics token must use R2_ANALYTICS_TOKEN_FILE")
	}
	values := map[string]string{}
	for _, key := range []string{"R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_ANALYTICS_TOKEN", "DEV_AUTH_KEY", "DISCORD_CLIENT_SECRET", "DISCORD_WEBHOOK_URL", "DISCORD_BOT_TOKEN", "SMTP_USERNAME", "SMTP_PASSWORD", "GOOGLE_OAUTH_CLIENT_SECRET", "CLOUDFLARE_API_TOKEN", "GOOGLE_SERVICE_ACCOUNT_JSON"} {
		path := getenv(key + "_FILE")
		if path == "" {
			continue
		}
		if getenv(key) != "" {
			return nil, errors.New("conflicting secret sources")
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(f, 262145))
		f.Close()
		value := strings.TrimSpace(string(body))
		if err != nil || len(body) > 262144 || value == "" {
			return nil, errors.New("invalid secret file")
		}
		values[key] = value
	}
	return func(key string) string {
		if value, ok := values[key]; ok {
			return value
		}
		return getenv(key)
	}, nil
}
