package api

import (
	"fmt"
	"officerdashboard/internal/store"
	"testing"
	"time"
)

func TestCommitmentHistoryInterleaved(t *testing.T) {
	s := &Server{}
	cutoff := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)
	id, other := "req", "other"
	approved, closed, job := `{"status":"approved"}`, `{"status":"closed"}`, `{"requestId":"req"}`
	for _, tc := range []struct {
		name    string
		history []store.AuditEntry
		want    bool
	}{
		{"current", nil, true},
		{"unrelated", []store.AuditEntry{{EntityType: "profile", EntityID: &id, BeforeJSON: &closed}, {EntityType: "purchase_request", EntityID: &other, BeforeJSON: &closed}}, true},
		{"request undo", []store.AuditEntry{{EntityType: "purchase_request", EntityID: &id, BeforeJSON: &closed}, {EntityType: "purchase_request", EntityID: &id, BeforeJSON: &approved}}, true},
		{"creation", []store.AuditEntry{{EntityType: "purchase_request", EntityID: &id}}, false},
		{"job claim", []store.AuditEntry{{EntityType: "fulfillment_job", Action: "job.claimed", BeforeJSON: &job}}, true},
		{"job complete", []store.AuditEntry{{EntityType: "fulfillment_job", Action: "job.done", BeforeJSON: &job}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &store.Request{ID: id, Status: "approved", CreatedAt: 1}
			if got := s.committedAt(r, commitmentHistory(tc.history)[id], "", "2026-09-30", cutoff); got != tc.want {
				t.Fatal(got)
			}
		})
	}
}

func BenchmarkCommitmentHistory(b *testing.B) {
	var history []store.AuditEntry
	for i := 0; i < 6000; i++ {
		id := fmt.Sprintf("req-%d", i%300)
		before := `{"status":"approved"}`
		history = append(history, store.AuditEntry{EntityType: "purchase_request", EntityID: &id, BeforeJSON: &before})
	}
	s := &Server{}
	cutoff := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		grouped := commitmentHistory(history)
		for n := 0; n < 300; n++ {
			id := fmt.Sprintf("req-%d", n)
			s.committedAt(&store.Request{ID: id, Status: "fulfilled"}, grouped[id], "", "2026-09-30", cutoff)
		}
	}
}
