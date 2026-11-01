// Package fiscal holds fiscal-year arithmetic and the organisation's Pacific calendar.
// Fiscal years are named by the calendar year they end in (FY2027 = Jul 2026 – Jun 2027
// with the default July start).
package fiscal

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata" // the Pi image and the container may have no zoneinfo
)

// DefaultStartMonth is July; CSEED's start month is an assumption until the treasurer confirms.
const DefaultStartMonth = 7

var isoDate = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)

// Pacific is the organisation's time zone.
var Pacific = mustLoad("America/Los_Angeles")

func mustLoad(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// YearFor returns the fiscal year containing an ISO date.
func YearFor(date string, startMonth int) (int, error) {
	m := isoDate.FindStringSubmatch(date)
	if m == nil {
		return 0, fmt.Errorf("expected YYYY-MM-DD, got %s", date)
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	if startMonth == 1 || month < startMonth {
		return year, nil
	}
	return year + 1, nil
}

// YearRange returns the first and last day of a fiscal year.
func YearRange(fy, startMonth int) (start, end string) {
	startYear := fy - 1
	if startMonth == 1 {
		startYear = fy
	}
	start = fmt.Sprintf("%d-%02d-01", startYear, startMonth)
	// Day 0 of the start month, one year on = last day of the fiscal year.
	end = time.Date(startYear+1, time.Month(startMonth), 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	return
}

// Period is one calendar-month accounting period.
type Period struct {
	Name      string
	StartDate string
	EndDate   string
}

// MonthlyPeriods lists the 12 calendar-month periods of a fiscal year in order.
func MonthlyPeriods(fy, startMonth int) []Period {
	startYear := fy - 1
	if startMonth == 1 {
		startYear = fy
	}
	out := make([]Period, 12)
	for i := range out {
		first := time.Date(startYear, time.Month(startMonth+i), 1, 0, 0, 0, 0, time.UTC)
		last := time.Date(startYear, time.Month(startMonth+i+1), 0, 0, 0, 0, 0, time.UTC)
		out[i] = Period{Name: first.Format("Jan 2006"), StartDate: first.Format("2006-01-02"), EndDate: last.Format("2006-01-02")}
	}
	return out
}

// PacificDate is the calendar date of an instant in the organisation's time zone.
func PacificDate(t time.Time) string { return t.In(Pacific).Format("2006-01-02") }

// UTCDate is the UTC calendar date; v1 posts journal entries and defaults report dates on it.
func UTCDate(t time.Time) string { return t.UTC().Format("2006-01-02") }
