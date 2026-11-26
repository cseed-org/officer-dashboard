package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAlertsPostContentOncePerKeyPerHour(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		got = append(got, m)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	a := NewAlerter(srv.URL, nil)
	a.Now = func() time.Time { return now }
	ctx := context.Background()
	if !a.Alert(ctx, "drive-quota", "Drive is 71% full") {
		t.Fatal("first alert not sent")
	}
	if a.Alert(ctx, "drive-quota", "Drive is 72% full") {
		t.Error("second alert within the hour was sent")
	}
	if !a.Alert(ctx, "drive-revoked", "Drive access revoked") {
		t.Error("another key was rate-limited")
	}
	now = now.Add(61 * time.Minute)
	if !a.Alert(ctx, "drive-quota", "Drive is 73% full") {
		t.Error("alert after an hour not sent")
	}
	if len(got) != 3 || got[0]["content"] != "Drive is 71% full" {
		t.Fatalf("posted %v", got)
	}
	if m, _ := got[0]["allowed_mentions"].(map[string]any); m == nil || len(m["parse"].([]any)) != 0 {
		t.Errorf("mentions not disabled: %v", got[0])
	}
	var nilAlerter *Alerter
	if nilAlerter.Alert(ctx, "k", "m") {
		t.Error("nil alerter reported sending")
	}
	if !NewAlerter("", nil).Alert(ctx, "k", "m") {
		t.Error("console fallback did not log")
	}
}
