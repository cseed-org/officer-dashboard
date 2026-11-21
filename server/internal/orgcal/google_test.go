package orgcal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"officerdashboard/internal/calendar"
	"testing"
)

func TestInsertRetryUsesSameIDAndSendUpdates(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("sendUpdates") != "all" {
			t.Error("lost sendUpdates")
		}
		if calls == 1 {
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			w.WriteHeader(409)
			return
		}
		if r.Method != "PATCH" || r.URL.Path != "/calendars/org/events/stable" {
			t.Error(r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"stable","etag":"v2"}`))
	}))
	defer s.Close()
	g := calendar.NewGoogle("client", "secret", "callback")
	g.APIBase = s.URL
	c := Client{g}
	e, err := c.Upsert(context.Background(), "access", "org", "", "stable", Event{Summary: "Test"}, true)
	if err != nil || e.ID != "stable" || e.ETag != "v2" || calls != 2 {
		t.Fatalf("%+v %v calls=%d", e, err, calls)
	}
}
