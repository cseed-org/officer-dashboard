package store

import (
	"testing"
	"time"
)

func TestBuildspaceWeekOfDueDays(t *testing.T) {
	// 2026-11-04 is a Wednesday.
	cases := []struct {
		weekday     int
		date        string
		start, due  string
		description string
	}{
		{0, "2026-11-02", "2026-11-02", "2026-11-08", "default Sunday: Monday starts the week"},
		{0, "2026-11-08", "2026-11-02", "2026-11-08", "default Sunday: due day is inside its own week"},
		{0, "2026-11-09", "2026-11-09", "2026-11-15", "default Sunday: Monday after starts the next"},
		{3, "2026-11-04", "2026-10-29", "2026-11-04", "Wednesday due: Thursday–Wednesday"},
		{3, "2026-11-05", "2026-11-05", "2026-11-11", "Wednesday due: Thursday starts the next"},
		{5, "2026-12-31", "2026-12-26", "2027-01-01", "Friday due: spans a year end"},
		{1, "2027-03-08", "2027-03-02", "2027-03-08", "Monday due on a DST-change week"},
	}
	for _, c := range cases {
		rules := []BuildspaceWeekRule{{EffectiveFrom: "0001-01-01", DueWeekday: c.weekday}}
		got := BuildspaceWeekOf(rules, c.date)
		if got.Start != c.start || got.Due != c.due {
			t.Errorf("%s: %s → %+v", c.description, c.date, got)
		}
	}
}

func TestBuildspaceWeekTransition(t *testing.T) {
	// The current Sunday week (Nov 2–8) keeps its deadline; Wednesday takes over on Nov 9.
	// The first Wednesday week runs Nov 9–18 (10 days), then normal Thursday–Wednesday weeks.
	rules := []BuildspaceWeekRule{DefaultBuildspaceRule, {EffectiveFrom: "2026-11-09", DueWeekday: 3}}
	for date, want := range map[string]BuildspaceWeek{
		"2026-11-08": {"2026-11-02", "2026-11-08"},
		"2026-11-09": {"2026-11-09", "2026-11-18"},
		"2026-11-11": {"2026-11-09", "2026-11-18"},
		"2026-11-18": {"2026-11-09", "2026-11-18"},
		"2026-11-19": {"2026-11-19", "2026-11-25"},
	} {
		if got := BuildspaceWeekOf(rules, date); got != want {
			t.Errorf("%s: got %+v want %+v", date, got, want)
		}
	}
	// Moving to the next day (Monday) yields a 7-day transition rather than a 1-day week.
	monday := []BuildspaceWeekRule{DefaultBuildspaceRule, {EffectiveFrom: "2026-11-09", DueWeekday: 1}}
	if got := BuildspaceWeekOf(monday, "2026-11-09"); got != (BuildspaceWeek{"2026-11-09", "2026-11-16"}) {
		t.Errorf("Monday transition: %+v", got)
	}
	// Weeks partition the calendar with no gaps or overlaps across the transition.
	weeks := BuildspaceWeeksDue(rules, "2026-10-01", "2027-01-01")
	for i := 1; i < len(weeks); i++ {
		prev, _ := time.Parse(isoDate, weeks[i-1].Due)
		if prev.AddDate(0, 0, 1).Format(isoDate) != weeks[i].Start {
			t.Fatalf("gap between %+v and %+v", weeks[i-1], weeks[i])
		}
	}
	if len(weeks) == 0 || weeks[0].Due != "2026-10-04" || weeks[len(weeks)-1].Due >= "2027-01-01" {
		t.Fatalf("window: %+v", weeks)
	}
}

func TestBuildspaceDueMomentDST(t *testing.T) {
	la, _ := time.LoadLocation("America/Los_Angeles")
	// End of day on fall-back Sunday (Nov 1 2026, 25 hours long) is Nov 2 00:00 PST = 08:00 UTC.
	if got := BuildspaceDueMoment("2026-11-01", nil, la).UTC(); !got.Equal(time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("fall back end of day: %s", got)
	}
	// 18:00 on spring-forward Sunday (Mar 14 2027) is PDT: 01:00 UTC next day.
	six := "18:00"
	if got := BuildspaceDueMoment("2027-03-14", &six, la).UTC(); !got.Equal(time.Date(2027, 3, 15, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("spring forward 18:00: %s", got)
	}
	// A wall time skipped by spring-forward (02:30) normalises forward to 03:30 PDT.
	gap := "02:30"
	if got := BuildspaceDueMoment("2027-03-14", &gap, la).UTC(); !got.Equal(time.Date(2027, 3, 14, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("spring-forward gap: %s", got)
	}
}

func TestBuildspaceScheduleClockAndPastDue(t *testing.T) {
	la, _ := time.LoadLocation("America/Los_Angeles")
	rules := []BuildspaceWeekRule{DefaultBuildspaceRule}
	// 07:30 UTC on Nov 2 is still Sunday Nov 1 (23:30 PST) in the club zone, the night
	// clocks fell back; a fixed UTC−7 offset would wrongly report Monday.
	now := time.Date(2026, 11, 2, 7, 30, 0, 0, time.UTC)
	s := BuildspaceScheduleAt(rules, BuildspaceClock{Loc: la, Now: now, Today: now.In(la).Format(isoDate)})
	if s.Today != "2026-11-01" || s.Week != (BuildspaceWeek{"2026-10-26", "2026-11-01"}) || s.PastDue {
		t.Fatalf("fall-back night: %+v", s)
	}
	six := "18:00"
	rules = []BuildspaceWeekRule{{EffectiveFrom: "0001-01-01", DueWeekday: 0, DueTime: &six}}
	for _, c := range []struct {
		utc  time.Time
		past bool
	}{{time.Date(2026, 11, 2, 1, 59, 0, 0, time.UTC), false}, {time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC), true}} {
		// 18:00 PST on Nov 1 is 02:00 UTC Nov 2.
		s = BuildspaceScheduleAt(rules, BuildspaceClock{Loc: la, Now: c.utc, Today: c.utc.In(la).Format(isoDate)})
		if s.PastDue != c.past || s.Week.Due != "2026-11-01" {
			t.Fatalf("%s: %+v", c.utc, s)
		}
	}
}
