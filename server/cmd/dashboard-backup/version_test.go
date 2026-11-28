//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"officerdashboard/internal/setup/sinkbundle"
)

// versionRig is a sink answering GET /version as given, and a webhook recording alerts.
func versionRig(t *testing.T, version func(w http.ResponseWriter)) (*sink, *[]string) {
	t.Helper()
	var mu sync.Mutex
	alerts := []string{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &m)
		mu.Lock()
		alerts = append(alerts, m["content"])
		mu.Unlock()
	}))
	t.Cleanup(hook.Close)
	sinkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" || r.Header.Get("Authorization") != "Bearer version-test-bearer-0123456789abcdef" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		version(w)
	}))
	t.Cleanup(sinkSrv.Close)
	secretValues = map[string]string{"BACKUP_BEARER_SECRET": "version-test-bearer-0123456789abcdef", "DISCORD_WEBHOOK_URL": hook.URL}
	t.Cleanup(func() { secretValues = nil })
	t.Setenv("BACKUP_SINK_URL", sinkSrv.URL)
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "dashboard.db"))
	s, err := newSink()
	if err != nil {
		t.Fatal(err)
	}
	return s, &alerts
}

func revisionAnswer(n int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { fmt.Fprintf(w, `{"revision":%d}`, n) }
}

// withMinimum runs the test as if the service needed revision n.
func withMinimum(t *testing.T, n int) {
	t.Helper()
	old := minSinkRevision
	minSinkRevision = n
	t.Cleanup(func() { minSinkRevision = old })
}

// Review F-1 of #129: the service alerts only when the Worker reports a revision below the
// one it actually needs, and once per (reported, required) pair, not every day.
func TestWorkerBelowTheNeededRevisionIsAlertedOncePerPair(t *testing.T) {
	withMinimum(t, 3)
	reported := 2
	s, alerts := versionRig(t, func(w http.ResponseWriter) { revisionAnswer(reported)(w) })
	checkSinkRevision(s)
	checkSinkRevision(s)
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "revision 2") || !strings.Contains((*alerts)[0], "needs revision 3") {
		t.Fatalf("alerts %q", *alerts)
	}
	reported = 1 // another pair: one more alert
	checkSinkRevision(s)
	checkSinkRevision(s)
	if len(*alerts) != 2 {
		t.Fatalf("alerts %q", *alerts)
	}
}

// Today the service needs nothing a deployed Worker lacks (sinkbundle.MinRevision is 1),
// so no existing install alerts: not for a Worker from before GET /version (404), nor
// for one older than the image.
func TestNoAlertForAWorkerTheServiceCanUse(t *testing.T) {
	if sinkbundle.MinRevision != 1 {
		t.Fatalf("MinRevision is %d; raise it only with the Worker upgrade path", sinkbundle.MinRevision)
	}
	for name, answer := range map[string]func(http.ResponseWriter){
		"before the endpoint (404)": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"not found"}`)
		},
		"older than the image": revisionAnswer(sinkbundle.Revision - 1),
		"same":                 revisionAnswer(sinkbundle.Revision),
		"newer":                revisionAnswer(sinkbundle.Revision + 1),
		"unavailable":          func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) },
		"not a number":         func(w http.ResponseWriter) { fmt.Fprint(w, `{"revision":"x"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			s, alerts := versionRig(t, answer)
			checkSinkRevision(s)
			if len(*alerts) != 0 {
				t.Fatalf("alerts %q", *alerts)
			}
		})
	}
}

// Review F-10: the check has its own short timeout, so a hung Worker delays the backup
// by at most that, never by the sink client's two minutes.
func TestVersionCheckGivesUpQuickly(t *testing.T) {
	old := versionTimeout
	versionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { versionTimeout = old })
	release := make(chan struct{})
	s, alerts := versionRig(t, func(w http.ResponseWriter) { <-release })
	t.Cleanup(func() { close(release) })
	start := time.Now()
	checkSinkRevision(s)
	if took := time.Since(start); took > 2*time.Second || len(*alerts) != 0 {
		t.Fatalf("took %v, alerts %q", took, *alerts)
	}
}
