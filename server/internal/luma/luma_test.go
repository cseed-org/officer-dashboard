package luma

import (
	"strings"
	"testing"
	"time"
)

func TestParseGuestsQuotedBOMCRLFAndNormalisedEmail(t *testing.T) {
	res, err := ParseGuests("\ufeffFull Name,Email Address,Approval Status,Registered At,Checked In At\r\n\"Lee, Ann\",ANN@EXAMPLE.ORG,approved,2026-10-01T01:00:00Z,2026-10-02T01:00:00Z\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 || len(res.Rows) != 1 {
		t.Fatalf("errors %+v rows %d", res.Errors, len(res.Rows))
	}
	g := res.Rows[0]
	if g.Name == nil || *g.Name != "Lee, Ann" || g.Email != "ann@example.org" || g.ApprovalStatus == nil || *g.ApprovalStatus != "approved" {
		t.Errorf("row %+v", g)
	}
	if g.CheckedInAt == nil || *g.CheckedInAt != "2026-10-02T01:00:00.000Z" {
		t.Errorf("checkedInAt %v", g.CheckedInAt)
	}
}

func TestMalformedCSVBadEmailAndDates(t *testing.T) {
	if _, err := ParseGuests("name,email\n\"unfinished,x"); err == nil {
		t.Error("unterminated quote must fail")
	}
	res, _ := ParseGuests("email,checked_in_at\nx,not a date")
	if len(res.Errors) != 1 {
		t.Errorf("errors %+v", res.Errors)
	}
	ok, err := ParseReflections("Email,Timestamp\na@b.org,2026-10-02T10:00:00Z")
	if err != nil || len(ok.Rows) != 1 {
		t.Errorf("rows %+v err %v", ok, err)
	}
	if _, err := ParseReflections("Name\nAnn"); err == nil || !strings.Contains(err.Error(), "email") {
		t.Errorf("err %v", err)
	}
	if _, err := ParseGuests(""); err == nil {
		t.Error("empty CSV must fail")
	}
}

func TestIsRegistered(t *testing.T) {
	for _, s := range []string{"pending_approval", "invited", "waitlisted", "declined", "Pending Approval", " WAIT-LIST "} {
		want := s == " WAIT-LIST " // not a known alias; only exact normalised names are excluded
		if got := IsRegistered(&s, false); got != want {
			t.Errorf("IsRegistered(%q) = %v want %v", s, got, want)
		}
	}
	inv := "invited"
	if !IsRegistered(&inv, true) {
		t.Error("checked-in guests count as registered")
	}
	ap := "approved"
	if !IsRegistered(&ap, false) || !IsRegistered(nil, false) {
		t.Error("approved and unknown statuses are registered")
	}
}

func TestSurveyTimestampsArePacificAndRowsArePhysical(t *testing.T) {
	r, err := ParseReflections("email,timestamp\na@b.org,10/31/2026 11:30:00 PM")
	if err != nil || r.Rows[0].SubmittedAt != "2026-11-01T06:30:00.000Z" {
		t.Errorf("got %+v err %v", r.Rows, err)
	}
	e, _ := ParseReflections("email,timestamp\n\na@b.org,bad")
	if len(e.Errors) != 1 || e.Errors[0].Row != 3 {
		t.Errorf("row evidence %+v", e.Errors)
	}
	n, _ := ParseReflections("email,timestamp\na@b.org,03/08/2026 02:30:00")
	if len(n.Errors) != 1 {
		t.Errorf("nonexistent Pacific time must error: %+v", n.Errors)
	}
}

func TestCSVTimestampEdges(t *testing.T) {
	cases := map[string]string{
		"2026-10-02T10:00:00Z":        "2026-10-02T10:00:00.000Z",
		"2026-10-02 10:00":            "2026-10-02T17:00:00.000Z", // PDT
		"2026-12-02 10:00:00":         "2026-12-02T18:00:00.000Z", // PST
		"2026-10-02T10:00:00.5-05:00": "2026-10-02T15:00:00.500Z",
		"11/01/2026 01:30:00 AM":      "2026-11-01T08:30:00.000Z", // ambiguous: earlier (PDT) instant
		"2026-10-02":                  "2026-10-02T07:00:00.000Z",
		"10/2/2026 9:05 am":           "2026-10-02T16:05:00.000Z",
	}
	for in, want := range cases {
		got, err := CSVTimestamp(in)
		if err != nil || got != want {
			t.Errorf("CSVTimestamp(%q) = %q, %v want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"yesterday", "2026-02-30", "13/45/2026 10:00", "2026-10-02T25:00:00Z", "10/2/2026 13:00 PM"} {
		if _, err := CSVTimestamp(in); err == nil {
			t.Errorf("CSVTimestamp(%q) should fail", in)
		}
	}
}

func TestClubTimezoneTimestamps(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]string{
		"2026-01-15 18:00":          "2026-01-15T23:00:00.000Z",
		"2026-07-15 18:00":          "2026-07-15T22:00:00.000Z",
		"2026-01-15T18:00:00-08:00": "2026-01-16T02:00:00.000Z",
		"2026-01-15T18:00:00Z":      "2026-01-15T18:00:00.000Z",
		"2026-11-01 01:30":          "2026-11-01T05:30:00.000Z",
	} {
		got, err := CSVTimestamp(input, ny)
		if err != nil || got != want {
			t.Errorf("%s: %s, %v; want %s", input, got, err, want)
		}
	}
	if _, err := CSVTimestamp("2026-03-08 02:30", ny); err == nil {
		t.Fatal("accepted DST gap")
	}
	guests, err := ParseGuests("email,registered at\na@b.org,2026-01-15 18:00", ny)
	if err != nil || len(guests.Rows) != 1 || *guests.Rows[0].RegisteredAt != "2026-01-15T23:00:00.000Z" {
		t.Fatalf("guests: %+v %v", guests, err)
	}
	reflections, err := ParseReflections("email,timestamp\na@b.org,2026-01-15 18:00", ny)
	if err != nil || len(reflections.Rows) != 1 || reflections.Rows[0].SubmittedAt != "2026-01-15T23:00:00.000Z" {
		t.Fatalf("reflections: %+v %v", reflections, err)
	}
}
