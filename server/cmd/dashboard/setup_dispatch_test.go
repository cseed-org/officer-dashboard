package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// setup runs on hosts that are not set up yet, where *_FILE targets may not exist, so it
// must be dispatched before fileSettings would refuse them.
func TestSetupRunsBeforeSecretFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet")
	env := map[string]string{"DISCORD_CLIENT_SECRET_FILE": missing, "CLOUDFLARE_API_TOKEN_FILE": missing}
	var out, errb bytes.Buffer
	code := run([]string{"setup", "--deploy-dir", t.TempDir()}, func(k string) string { return env[k] }, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "scripts/setup host --yes") {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	// Other commands still refuse the missing file.
	errb.Reset()
	if code := run([]string{"version"}, func(k string) string { return env[k] }, &out, &errb); code != 1 {
		t.Fatalf("version with a missing secret file: exit %d", code)
	}
}

func TestSetupHostRunsBeforeSecretFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet")
	env := map[string]string{"DISCORD_CLIENT_SECRET_FILE": missing}
	var out, errb bytes.Buffer
	if code := run([]string{"setup", "host", "-h"}, func(k string) string { return env[k] }, &out, &errb); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if !strings.Contains(errb.String(), "dashboard setup host") {
		t.Fatalf("host usage not shown: %q", errb.String())
	}
}
