package store

import (
	"context"
	"officerdashboard/internal/cloudflare"
	"testing"
	"time"
)

func TestMemberSignupISOCountsAndSearch(t *testing.T) {
	db, err := Open(t.TempDir() + "/members.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	personal := "ada@example.org"
	cs := "ada@cs.washington.edu"
	err = db.Write(ctx, func(q *Queries) error {
		for _, p := range []cloudflare.Person{{ID: "today", FirstName: "Áda", LastName: "Member", JoinDate: "2026-11-01T10:00:00Z", CSEmail: &cs, PersonalEmail: &personal}, {ID: "future", FirstName: "Future", LastName: "Member", JoinDate: "2026-11-02T10:00:00Z"}, {ID: "old", FirstName: "Old", LastName: "Member", JoinDate: "2026-01-01T10:00:00Z"}} {
			if e := q.SaveMember(ctx, p); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	counts, err := db.Read().MemberSignupCounts(ctx, at)
	if err != nil || counts["today"] != 1 || counts["last7Days"] != 1 || counts["last30Days"] != 1 || counts["members"] != 3 {
		t.Fatalf("counts %v %v", counts, err)
	}
	for _, needle := range []string{"ÁDA", "ada@example.org", "ada@cs.washington.edu"} {
		_, n, e := db.Read().Members(ctx, MemberFilter{Search: needle, Limit: 25})
		if e != nil || n != 1 {
			t.Errorf("search %q total=%d err=%v", needle, n, e)
		}
	}
	_, n, err := db.Read().Members(ctx, MemberFilter{Search: "%' OR 1=1 --", Limit: 25})
	if err != nil || n != 0 {
		t.Fatalf("literal search total=%d err=%v", n, err)
	}
}
