package fiscal

import (
	"testing"
	"time"
)

func TestFiscalYearFor(t *testing.T) {
	cases := map[string]int{"2026-07-01": 2027, "2027-06-30": 2027, "2026-01-15": 2026}
	for in, want := range cases {
		got, err := YearFor(in, DefaultStartMonth)
		if err != nil || got != want {
			t.Errorf("YearFor(%s) = %d,%v want %d", in, got, err, want)
		}
	}
	if got, _ := YearFor("2026-07-01", 1); got != 2026 {
		t.Errorf("calendar FY got %d", got)
	}
	if _, err := YearFor("7/1/2026", DefaultStartMonth); err == nil {
		t.Error("malformed date should fail")
	}
}

func TestYearRange(t *testing.T) {
	s, e := YearRange(2027, DefaultStartMonth)
	if s != "2026-07-01" || e != "2027-06-30" {
		t.Errorf("got %s %s", s, e)
	}
	s, e = YearRange(2026, 1)
	if s != "2026-01-01" || e != "2026-12-31" {
		t.Errorf("calendar got %s %s", s, e)
	}
}

func TestMonthlyPeriods(t *testing.T) {
	p := MonthlyPeriods(2028, DefaultStartMonth)
	if len(p) != 12 {
		t.Fatalf("len %d", len(p))
	}
	want := map[int]Period{
		0:  {Name: "Jul 2027", StartDate: "2027-07-01", EndDate: "2027-07-31"},
		7:  {Name: "Feb 2028", StartDate: "2028-02-01", EndDate: "2028-02-29"},
		11: {Name: "Jun 2028", StartDate: "2028-06-01", EndDate: "2028-06-30"},
	}
	for i, w := range want {
		if p[i] != w {
			t.Errorf("period %d = %+v want %+v", i, p[i], w)
		}
	}
}

func TestPacificDate(t *testing.T) {
	// 2026-11-01T06:30Z is still Oct 31 in Pacific daylight time (UTC-7).
	got := PacificDate(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	if got != "2026-10-31" {
		t.Errorf("got %s", got)
	}
	// After the fall-back (Nov 1 02:00 PDT → 01:00 PST) the offset is UTC-8.
	if got := PacificDate(time.Date(2026, 11, 2, 7, 59, 0, 0, time.UTC)); got != "2026-11-01" {
		t.Errorf("got %s", got)
	}
}
