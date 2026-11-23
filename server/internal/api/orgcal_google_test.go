package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/orgcal"
	"officerdashboard/internal/store"
	"strings"
	"sync"
	"testing"
	"time"
)

type orgRequest struct {
	Method, Path, Query string
	Body                map[string]any
}
type orgFake struct {
	mu                       sync.Mutex
	requests                 []orgRequest
	events                   map[string][]orgcal.Event
	failWrites               bool
	failACLInsert            bool
	revoked                  bool
	gone                     bool
	instances                []orgcal.Event
	pauseWrite, enteredWrite chan struct{}
}

func (f *orgFake) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead && r.URL.Path == "/" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/events") {
		f.mu.Lock()
		pause, entered := f.pauseWrite, f.enteredWrite
		f.pauseWrite = nil
		f.mu.Unlock()
		if pause != nil {
			close(entered)
			<-pause
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var b map[string]any
	_ = json.NewDecoder(r.Body).Decode(&b)
	f.requests = append(f.requests, orgRequest{r.Method, r.URL.Path, r.URL.RawQuery, b})
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" && f.revoked {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	if r.URL.Path == "/token" {
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"org-sub","email":"org@example.org"}`))
		_, _ = fmt.Fprintf(w, `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"id_token":"x.%s.x","scope":"openid email https://www.googleapis.com/auth/calendar"}`, claims)
		return
	}
	if r.URL.Path == "/calendars" {
		name, _ := b["summary"].(string)
		id := map[string]string{"Internal Meetings": "internal", "External Meetings": "external", "Weekly Programs": "programs", "Events": "events"}[name]
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/acl") && r.Method == "GET" {
		_, _ = w.Write([]byte(`{"items":[{"id":"stale","role":"reader","scope":{"type":"user","value":"former@example.org"}}]}`))
		return
	}
	if strings.Contains(r.URL.Path, "/acl") && r.Method == "POST" && f.failACLInsert {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid scope value"}}`))
		return
	}
	if strings.Contains(r.URL.Path, "/acl") {
		w.WriteHeader(204)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/instances") {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": f.instances})
		return
	}
	if r.Method == "GET" {
		if f.gone && r.URL.Query().Get("syncToken") != "" {
			f.gone = false
			w.WriteHeader(410)
			return
		}
		key := strings.Split(r.URL.Path, "/")[2]
		_ = json.NewEncoder(w).Encode(map[string]any{"items": f.events[key], "nextSyncToken": "sync-next"})
		return
	}
	if f.failWrites {
		w.WriteHeader(503)
		return
	}
	if r.Method == "DELETE" {
		parts := strings.Split(r.URL.Path, "/")
		key := parts[2]
		id := parts[len(parts)-1]
		items := f.events[key][:0]
		for _, e := range f.events[key] {
			if e.ID != id {
				items = append(items, e)
			}
		}
		f.events[key] = items
		w.WriteHeader(204)
		return
	}
	id, _ := b["id"].(string)
	if id == "" {
		parts := strings.Split(r.URL.Path, "/")
		id = parts[len(parts)-1]
	}
	var event orgcal.Event
	raw, _ := json.Marshal(b)
	_ = json.Unmarshal(raw, &event)
	event.ID = id
	event.ETag = "saved-etag"
	parts := strings.Split(r.URL.Path, "/")
	key := parts[2]
	items := f.events[key][:0]
	for _, old := range f.events[key] {
		if old.ID != id {
			items = append(items, old)
		}
	}
	f.events[key] = append(items, event)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "etag": "saved-etag"})
}

func TestOrgCalendarGoogleProgramSkipMoveAndRestore(t *testing.T) {
	e, f := orgGoogleEnv(t)
	original := orgcal.Time{DateTime: "2026-11-03T10:00:00-08:00", TimeZone: "America/Los_Angeles"}
	f.instances = []orgcal.Event{{ID: "instance-3", Start: original, End: orgcal.Time{DateTime: "2026-11-03T11:00:00-08:00"}, OriginalStartTime: &original, Status: "confirmed"}}
	who := e.officer()
	r := e.mustOK(e.post("/api/programs", map[string]any{"trackId": "buildspace", "name": "Program", "weekday": 2, "startTime": "10:00", "durationMin": 60, "startsOn": "2026-11-01", "endsOn": "2026-11-30"}, who))
	id := r.str("program", "id")
	e.srv.WaitBackground()
	e.mustOK(e.post("/api/programs/"+id+"/exceptions", map[string]any{"onDate": "2026-11-10", "kind": "skip"}, who))
	e.srv.WaitBackground()
	e.mustOK(e.post("/api/programs/"+id+"/exceptions", map[string]any{"onDate": "2026-11-03", "kind": "move", "newStartsAt": "2026-11-04T18:00:00Z", "newEndsAt": "2026-11-04T19:00:00Z", "newLocation": "New room"}, who))
	e.srv.WaitBackground()
	f.mu.Lock()
	skip, moved := false, false
	for _, r := range f.requests {
		if recurrence, ok := r.Body["recurrence"].([]any); ok {
			for _, v := range recurrence {
				if v == "EXDATE;TZID=America/Los_Angeles:20261110T100000" {
					skip = true
				}
			}
		}
		if r.Path == "/calendars/programs/events/instance-3" && r.Method == "PATCH" && r.Body["location"] == "New room" {
			moved = true
		}
	}
	f.mu.Unlock()
	if !skip || !moved {
		t.Fatalf("skip=%v move=%v", skip, moved)
	}
	e.mustOK(e.do("DELETE", "/api/programs/"+id+"/exceptions?onDate=2026-11-03", nil, who))
	e.srv.WaitBackground()
}
func orgGoogleEnv(t *testing.T) (*testEnv, *orgFake) {
	f := &orgFake{events: map[string][]orgcal.Event{}}
	httpSrv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(httpSrv.Close)
	g := calendar.NewGoogle("cid", "secret", "http://localhost:8090/auth/google/callback")
	g.APIBase = httpSrv.URL
	g.TokenURL = httpSrv.URL + "/token"
	g.AuthURL = httpSrv.URL + "/consent"
	e := newEnv(t, func(c *Config) { c.Google = g })
	ctx := context.Background()
	err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.SaveOrgGoogleAccount(ctx, store.OrgGoogleAccount{Subject: "org-sub", Email: "org@example.org", SealedRefresh: e.srv.sealer.Seal("refresh"), SealedAccess: e.srv.sealer.Seal("access"), ExpiresAt: e.clock.Add(time.Hour).UnixMilli()})
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, f
}
func TestOrgCalendarGoogleBootstrapDeliveryACLAndDelete(t *testing.T) {
	e, f := orgGoogleEnv(t)
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "external", "title": "Meeting", "tentativeFrom": "2026-11-24", "tentativeTo": "2026-11-28", "invitees": []any{map[string]any{"kind": "email", "ref": "guest@example.org"}}, "reminders": []any{map[string]any{"method": "popup", "minutes": 15}}}, e.officer()))
	id := r.str("event", "id")
	e.srv.WaitBackground()
	f.mu.Lock()
	made, pushed, removed := 0, false, false
	for _, r := range f.requests {
		if r.Path == "/calendars" {
			made++
		}
		if r.Path == "/calendars/external/events" && r.Method == "POST" {
			pushed = true
			if r.Query != "sendUpdates=all" || r.Body["summary"] != "[Tentative] Meeting" {
				t.Error(r)
			}
			start := r.Body["start"].(map[string]any)
			end := r.Body["end"].(map[string]any)
			if start["date"] != "2026-11-24" || end["date"] != "2026-11-29" {
				t.Error(start, end)
			}
			if len(r.Body["attendees"].([]any)) != 1 {
				t.Error(r.Body)
			}
		}
		if r.Path == "/calendars/internal/acl/stale" && r.Method == "DELETE" {
			removed = true
		}
	}
	f.mu.Unlock()
	if made != 4 || !pushed || !removed {
		t.Fatalf("bootstrap=%d pushed=%v removed stale=%v", made, pushed, removed)
	}
	cals := e.mustOK(e.get("/api/orgcal/calendars", e.officer()))
	if cals.at("calendars", 0, "googleCalendarId") != "internal" {
		t.Fatal(string(cals.Raw))
	}
	e.mustOK(e.do("DELETE", "/api/orgcal/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, r := range f.requests {
		if r.Method == "DELETE" && strings.Contains(r.Path, "/external/events/") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing Google delete")
	}
}
func TestOrgCalendarGooglePullAnd410(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.events["internal"] = []orgcal.Event{{ID: "remote", ETag: "remote-v1", Summary: "From Google", Start: orgcal.Time{DateTime: "2026-11-24T18:00:00Z"}, End: orgcal.Time{DateTime: "2026-11-24T19:00:00Z"}}}
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", e.officer()))
	if len(r.list("events")) != 1 || r.str("events", 0, "title") != "From Google" {
		t.Fatal(string(r.Raw))
	}
	f.mu.Lock()
	f.gone = true
	f.events["internal"] = []orgcal.Event{}
	f.mu.Unlock()
	e.clock = e.clock.Add(6 * time.Minute)
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", e.officer()))
	if len(r.list("events")) != 0 {
		t.Fatal("full resync did not remove missing remote event", string(r.Raw))
	}
}
func TestOrgCalendarGoogleFailedSendRetriedAfterRestart(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.failWrites = true
	e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Retry", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}, e.officer()))
	e.srv.WaitBackground()
	f.mu.Lock()
	f.failWrites = false
	f.mu.Unlock()
	e.clock = e.clock.Add(time.Hour)
	restarted := New(e.cfg)
	if err := restarted.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", e.officer()))
	if r.at("events", 0, "googleEventId") == nil {
		t.Fatal("retry did not record Google id", string(r.Raw))
	}
}
func TestOrgCalendarOAuthLeadershipAndPKCE(t *testing.T) {
	e, _ := orgGoogleEnv(t)
	r := e.get("/api/settings/org-google/connect", e.officer())
	if r.Status != 403 {
		t.Fatal(r.Status)
	}
	leader := e.as("user-leadership")
	r = e.get("/api/settings/org-google/connect", leader)
	if r.Status != 302 {
		t.Fatal(r.Status, string(r.Raw))
	}
	u, _ := url.Parse(r.Header.Get("Location"))
	if u.Query().Get("scope") != orgcal.Scope || u.Query().Get("redirect_uri") != "http://localhost:8090/auth/org-google/callback" {
		t.Fatal(u)
	}
	leader.cookies = (&http.Response{Header: r.Header}).Cookies()
	r = e.get("/auth/org-google/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code=code", leader)
	if r.Status != 302 || !strings.Contains(r.Header.Get("Location"), "connected") {
		t.Fatal(r.Status, string(r.Raw), r.Header)
	}
	e.srv.WaitBackground()
	e.mustOK(e.do("DELETE", "/api/settings/org-google", nil, leader))
	r = e.mustOK(e.get("/api/settings/org-google/status", leader))
	if r.at("connected") != false {
		t.Fatal(string(r.Raw))
	}
}
