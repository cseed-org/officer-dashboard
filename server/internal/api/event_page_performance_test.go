package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/store"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEventListDTOParityWithReferenceCatalog(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := e.db.Read()
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		t.Fatal(err)
	}
	items, err := e.srv.eventItems(ctx, q, cat, cat.events, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]store.Event{}
	for _, event := range cat.events {
		byID[event.ID] = event
	}
	want := paginate(items, func(item EventListItem) string { return eventKey(byID[item.ID]) }, "", 50)
	response := e.mustOK(e.get("/api/events", e.officer()))
	var got page[EventListItem]
	if err := json.Unmarshal([]byte(response.Raw), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event list DTOs changed:\ngot %+v\nwant %+v", got, want)
	}
}

func TestEventSQLPageParityAcrossOffsetsAndDST(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := e.officer()
	q := e.db.Read()
	for i := 0; i < 151; i++ {
		start := fmt.Sprintf("2026-11-%02dT%02d:15:00-08:00", 1+i%25, i%24)
		location := "Seattle"
		name := fmt.Sprintf("History %d", i)
		if i%3 == 0 {
			start = fmt.Sprintf("2026-11-%02dT%02d:15:00Z", 1+i%25, i%24)
			location = "東京"
		}
		if err := e.db.Write(ctx, func(q *store.Queries) error {
			return q.InsertEvent(ctx, store.Event{ID: fmt.Sprintf("perf-event-%03d", i), Name: name, StartsAt: start, Location: &location})
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := q.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	zone, _ := time.LoadLocation("America/Los_Angeles")
	for _, tc := range []struct{ when, search, from, to string }{
		{"all", "", "", ""}, {"past", "", "", ""}, {"upcoming", "history", "2026-11-01", "2026-11-15"}, {"all", "東京", "2026-11-01", "2026-11-01"},
	} {
		var matching []store.Event
		for _, v := range all {
			at, err := luma.ParseInstant(v.StartsAt)
			if err != nil {
				continue
			}
			day := at.In(zone).Format("2006-01-02")
			if tc.when == "past" && !at.Before(e.clock) || tc.when == "upcoming" && at.Before(e.clock) || tc.from != "" && day < tc.from || tc.to != "" && day > tc.to {
				continue
			}
			if tc.search != "" && !strings.Contains(strings.ToLower(v.Name), tc.search) && (v.Location == nil || !strings.Contains(strings.ToLower(*v.Location), tc.search)) {
				continue
			}
			matching = append(matching, v)
		}
		want := paginate(matching, eventKey, "", 10000)
		var gotIDs []string
		cursor := ""
		for {
			query := url.Values{"when": {tc.when}, "limit": {"7"}}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			if tc.search != "" {
				query.Set("q", tc.search)
			}
			if tc.from != "" {
				query.Set("from", tc.from)
			}
			if tc.to != "" {
				query.Set("to", tc.to)
			}
			r := e.mustOK(e.get("/api/events?"+query.Encode(), who))
			for _, v := range r.list("items") {
				gotIDs = append(gotIDs, v.(map[string]any)["id"].(string))
			}
			cursor = r.str("nextCursor")
			if cursor == "" {
				break
			}
		}
		var wantIDs []string
		for _, v := range want.Items {
			wantIDs = append(wantIDs, v.ID)
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("%+v\ngot %v\nwant %v", tc, gotIDs, wantIDs)
		}
	}
}
