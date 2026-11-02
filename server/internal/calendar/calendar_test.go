package calendar

import (
	"strings"
	"testing"
	"time"
)

func TestICSEscapesFoldsAndCancels(t *testing.T) {
	start := time.Date(2026, 11, 20, 18, 0, 0, 0, time.FixedZone("PST", -8*3600))
	e := Event{UID: "event-1@x", Summary: "a, b; c\\d\nnext " + strings.Repeat("é", 60), Start: start, End: start.Add(time.Hour)}
	out := ICS(e, "from@x.test", "to@x.test", start)
	if !strings.Contains(out, `SUMMARY:a\, b\; c\\d\nnext`) || !strings.Contains(out, "DTSTART:20261121T020000Z") || !strings.Contains(out, "METHOD:REQUEST") {
		t.Fatal(out)
	}
	for _, line := range strings.Split(out, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("unfolded line %q", line)
		}
	}
	e.Cancelled = true
	if c := ICS(e, "f@x.test", "t@x.test", start); !strings.Contains(c, "METHOD:CANCEL") || !strings.Contains(c, "STATUS:CANCELLED") {
		t.Fatal(c)
	}
}

func TestWindowDefaultsToOneHour(t *testing.T) {
	s, e, err := Window("2026-11-20T18:00:00-08:00", nil)
	if err != nil || e.Sub(s) != time.Hour {
		t.Fatal(s, e, err)
	}
	bad := "2026-11-20T17:00:00-08:00"
	if _, e2, _ := Window("2026-11-20T18:00:00-08:00", &bad); e2.Sub(s) != time.Hour {
		t.Fatal("an end before the start falls back to one hour")
	}
}

func TestSealerRoundTripAndKeySeparation(t *testing.T) {
	a, b := NewSealer("one"), NewSealer("two")
	sealed := a.Seal("refresh-token")
	if strings.Contains(sealed, "refresh") {
		t.Fatal("not sealed")
	}
	if got, err := a.Open(sealed); err != nil || got != "refresh-token" {
		t.Fatal(got, err)
	}
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("another key opened the token")
	}
}

func TestBuildMIMEHasCalendarPart(t *testing.T) {
	m := Build("from@x.test", Message{To: "to@x.test", Subject: "Invitation: Hack Night", Text: "hi", ICS: "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"}, time.Now())
	s := string(m)
	if !strings.Contains(s, "text/calendar; charset=utf-8; method=REQUEST") || !strings.Contains(s, `filename="invite.ics"`) {
		t.Fatal(s)
	}
}
