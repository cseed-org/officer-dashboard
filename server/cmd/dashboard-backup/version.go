//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"officerdashboard/internal/setup/sinkbundle"
)

// minSinkRevision is the oldest Worker revision this service can work with
// (sinkbundle.MinRevision); a variable only so tests can raise it.
var minSinkRevision = sinkbundle.MinRevision

// versionTimeout bounds the version check, so a hung Worker or Access delays the hourly
// backup by at most this (the sink client's own timeout is two minutes).
var versionTimeout = 10 * time.Second

// sinkRevisionMarker records the last (reported, required) pair alerted on, beside the
// database.
func sinkRevisionMarker() string {
	return filepath.Join(filepath.Dir(dbPath()), ".sink-revision-alert")
}

// checkSinkRevision asks the backup Worker its revision (GET /version, authenticated like
// every sink request) and alerts through the failure webhook when the Worker reports a
// revision below the one this service needs (minSinkRevision), once per (reported,
// required) pair. Anything else is ignored: a 404 (a Worker from before the endpoint,
// which this service can still use), an error, or an answer without a number. It never
// fails the run and never waits longer than versionTimeout.
func checkSinkRevision(s *sink) {
	ctx, cancel := context.WithTimeout(operationContext, versionTimeout)
	defer cancel()
	var v struct {
		Revision *int `json:"revision"`
	}
	if err := s.requestContext(ctx, "/version", "GET", nil, &v); err != nil || v.Revision == nil {
		return
	}
	rev, need := *v.Revision, minSinkRevision
	if rev >= need {
		return
	}
	pair := fmt.Sprintf("%d:%d", rev, need)
	if raw, err := os.ReadFile(sinkRevisionMarker()); err == nil && strings.TrimSpace(string(raw)) == pair {
		return
	}
	notify(fmt.Sprintf("The backup Worker runs revision %d, but this backup service needs revision %d. "+
		"Redeploy the Worker from this release (docs/deploy/fresh-install.md step 4) before backups depend on it.", rev, need))
	_ = os.WriteFile(sinkRevisionMarker(), []byte(pair+"\n"), 0o600)
}
