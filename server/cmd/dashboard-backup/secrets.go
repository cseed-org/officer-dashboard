//go:build linux

package main

import (
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
)

// Values stay in this process's memory, never in its exported environment.
var secretValues map[string]string

func setting(key string) string {
	if value, ok := secretValues[key]; ok {
		return value
	}
	return os.Getenv(key)
}

func loadSecretFiles() error {
	secretValues = make(map[string]string)
	if image := os.Getenv("DEPLOY_IMAGE"); image != "" && !regexp.MustCompile(`^[A-Za-z0-9./:_-]+@sha256:[a-f0-9]{64}$`).MatchString(image) {
		return errors.New("DEPLOY_IMAGE must be pinned by digest")
	}
	for _, key := range []string{"BACKUP_BEARER_SECRET", "BACKUP_ACCESS_CLIENT_ID", "BACKUP_ACCESS_CLIENT_SECRET", "BACKUP_MEMBERSHIP_API_TOKEN", "DISCORD_WEBHOOK_URL"} {
		path := os.Getenv(key + "_FILE")
		if path == "" {
			continue
		}
		if os.Getenv(key) != "" {
			return errors.New("set either a credential or its _FILE, not both")
		}
		file, err := os.Open(path)
		if err != nil {
			return errors.New("cannot read backup secret file")
		}
		body, err := io.ReadAll(io.LimitReader(file, 262145))
		file.Close()
		if err != nil || len(body) > 262144 || len(strings.TrimSpace(string(body))) == 0 {
			return errors.New("empty or invalid backup secret file")
		}
		secretValues[key] = strings.TrimSpace(string(body))
	}
	return nil
}
