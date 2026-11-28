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

// Plan risk 6: the backup service alerts through the failure webhook when the Worker is
// older than the revision it was built with; a Worker from before GET /version (404) is
// revision 1. It alerts at most once a day, and never fails the backup for it.
func TestOlderWorkerIsAlertedOnceADay(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter){
		"before the endpoint": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"not found"}`)
		},
		"older revision": revisionAnswer(sinkbundle.Revision - 1),
	} {
		t.Run(name, func(t *testing.T) {
			s, alerts := versionRig(t, answer)
			now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
			checkSinkRevision(s, now)
			checkSinkRevision(s, now.Add(time.Hour))
			if len(*alerts) != 1 || !strings.Contains((*alerts)[0], fmt.Sprintf("older than revision %d", sinkbundle.Revision)) {
				t.Fatalf("alerts %q", *alerts)
			}
			checkSinkRevision(s, now.Add(24*time.Hour))
			if len(*alerts) != 2 {
				t.Fatalf("no alert the next day: %q", *alerts)
			}
		})
	}
}

func TestSameOrNewerWorkerOrNoAnswerIsNotAlerted(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter){
		"same":         revisionAnswer(sinkbundle.Revision),
		"newer":        revisionAnswer(sinkbundle.Revision + 1),
		"unavailable":  func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) },
		"not a number": func(w http.ResponseWriter) { fmt.Fprint(w, `{"revision":"x"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			s, alerts := versionRig(t, answer)
			checkSinkRevision(s, time.Now())
			if len(*alerts) != 0 {
				t.Fatalf("alerts %q", *alerts)
			}
		})
	}
}
