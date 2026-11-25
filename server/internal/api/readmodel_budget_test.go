package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
)

func TestEventSmallPageAllocationBudget(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		body := strings.Repeat("past event ", 100)
		for i := 0; i < 1000; i++ {
			if err := q.InsertEvent(ctx, store.Event{ID: fmt.Sprintf("budget-event-%d", i), Name: "Historical event", StartsAt: "2025-01-01T00:00:00Z", Description: &body}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://localhost:8090/api/events?limit=1", nil)
	req.AddCookie(e.login("user-director"))
	allocations := testing.AllocsPerRun(2, func() {
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})
	// The ordinary timestamp-column index selects the page before metrics work.
	t.Logf("one-event page with 1,000 historical events: %.0f allocations", allocations)
	if allocations > 5000 {
		t.Fatalf("small event page enriched full history: %.0f allocations", allocations)
	}
}

func TestMemberOfficersAllocationBudget(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		for i := 0; i < 5000; i++ {
			email := fmt.Sprintf("budget-%d@uw.example", i)
			if err := q.SaveMember(ctx, cloudflare.Person{ID: fmt.Sprint(i), FirstName: "Member", UWEmail: &email, JoinDate: "2026-01-01"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c := &Ctx{S: e.srv}
	allocations := testing.AllocsPerRun(2, func() {
		if _, err := c.memberOfficers(ctx, e.db.Read()); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 5000 {
		t.Fatalf("officer badges rebuilt the full member email map: %.0f allocations", allocations)
	}
}
