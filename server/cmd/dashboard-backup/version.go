//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"officerdashboard/internal/setup/sinkbundle"
)

// sinkRevisionMarker records the day of the last older-Worker alert, beside the database.
func sinkRevisionMarker() string {
	return filepath.Join(filepath.Dir(dbPath()), ".sink-revision-alert")
}

// checkSinkRevision asks the backup Worker its revision (GET /version, authenticated like
// every sink request) and alerts through the failure webhook, at most once a day, when it
// is older than the revision this backup service was built with (sinkbundle.Revision).
// A Worker from before the endpoint answers 404, which is revision 1. The upload protocol
// is unchanged, so an older Worker still takes backups: this never fails the run, and a
// sink that does not answer is left to the backup's own error handling.
func checkSinkRevision(s *sink, now time.Time) {
	var v struct {
		Revision *int `json:"revision"`
	}
	err := s.request("/version", "GET", nil, &v)
	var st statusError
	rev := 0
	switch {
	case errors.As(err, &st) && int(st) == 404:
		rev = 1
	case err != nil || v.Revision == nil:
		return
	default:
		rev = *v.Revision
	}
	if rev >= sinkbundle.Revision {
		return
	}
	day := now.UTC().Format("2006-01-02")
	if raw, err := os.ReadFile(sinkRevisionMarker()); err == nil && strings.TrimSpace(string(raw)) == day {
		return
	}
	notify(fmt.Sprintf("The backup Worker runs revision %d, older than revision %d, which this backup service was built with. "+
		"Backups still run; redeploy the Worker from this release to upgrade it (docs/deploy/fresh-install.md step 4).", rev, sinkbundle.Revision))
	_ = os.WriteFile(sinkRevisionMarker(), []byte(day+"\n"), 0o600)
}
