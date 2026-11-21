package orgcal

import (
	"strconv"
	"strings"
	"time"
)

// Occurrences expands the calendar's daily/weekly/monthly/yearly RRULE in wall time.
// Iteration is bounded; ranges from the API are at most a year. Google recurrence
// timezone is retained so changing UTC offsets does not move weekly meeting hours.
func Occurrences(start time.Time, rule, from, to string) []time.Time {
	fields := map[string]string{}
	for _, p := range strings.Split(strings.TrimPrefix(rule, "RRULE:"), ";") {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			fields[kv[0]] = kv[1]
		}
	}
	interval, _ := strconv.Atoi(fields["INTERVAL"])
	if interval < 1 {
		interval = 1
	}
	count, _ := strconv.Atoi(fields["COUNT"])
	if count < 1 {
		count = 100000
	}
	until := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if raw := fields["UNTIL"]; raw != "" {
		if t, err := time.Parse("20060102T150405Z", raw); err == nil {
			until = t
		} else if t, err := time.ParseInLocation("20060102", raw, start.Location()); err == nil {
			until = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
	}
	weekdays := map[string]time.Weekday{"SU": 0, "MO": 1, "TU": 2, "WE": 3, "TH": 4, "FR": 5, "SA": 6}
	matchDay := func(t time.Time) bool {
		raw := fields["BYDAY"]
		if raw == "" {
			return true
		}
		for _, part := range strings.Split(raw, ",") {
			if len(part) < 2 {
				continue
			}
			code := part[len(part)-2:]
			wd, ok := weekdays[code]
			if !ok || wd != t.Weekday() {
				continue
			}
			if len(part) == 2 {
				return true
			}
			n, err := strconv.Atoi(part[:len(part)-2])
			if err != nil {
				continue
			}
			if n > 0 && (t.Day()-1)/7+1 == n {
				return true
			}
			last := time.Date(t.Year(), t.Month()+1, 0, 12, 0, 0, 0, t.Location()).Day()
			if n < 0 && (last-t.Day())/7+1 == -n {
				return true
			}
		}
		return false
	}
	matchMonthDay := func(t time.Time) bool {
		if fields["BYMONTHDAY"] == "" {
			return true
		}
		last := time.Date(t.Year(), t.Month()+1, 0, 12, 0, 0, 0, t.Location()).Day()
		for _, raw := range strings.Split(fields["BYMONTHDAY"], ",") {
			n, _ := strconv.Atoi(raw)
			if n < 0 {
				n = last + n + 1
			}
			if n == t.Day() {
				return true
			}
		}
		return false
	}
	matchMonth := func(t time.Time) bool {
		if fields["BYMONTH"] == "" {
			return true
		}
		for _, raw := range strings.Split(fields["BYMONTH"], ",") {
			n, _ := strconv.Atoi(raw)
			if n == int(t.Month()) {
				return true
			}
		}
		return false
	}
	dateDay := func(t time.Time) int64 {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
	}
	weekBase := dateDay(start) - int64((int(start.Weekday())+6)%7)
	result := []time.Time{}
	n := 0
	for t, day := start, 0; day < 366*200 && t.Format("2006-01-02") < to && !t.After(until) && n < count; t, day = t.AddDate(0, 0, 1), day+1 {
		match := false
		switch fields["FREQ"] {
		case "DAILY":
			match = day%interval == 0
		case "WEEKLY":
			week := (dateDay(t) - int64((int(t.Weekday())+6)%7) - weekBase) / 7
			match = week%int64(interval) == 0 && (fields["BYDAY"] != "" || t.Weekday() == start.Weekday())
		case "MONTHLY":
			months := (t.Year()-start.Year())*12 + int(t.Month()-start.Month())
			match = months%interval == 0 && (fields["BYDAY"] != "" || fields["BYMONTHDAY"] != "" || t.Day() == start.Day())
		case "YEARLY":
			match = (t.Year()-start.Year())%interval == 0 && (fields["BYMONTH"] != "" || t.Month() == start.Month()) && (fields["BYDAY"] != "" || fields["BYMONTHDAY"] != "" || t.Day() == start.Day())
		}
		if !match || !matchDay(t) || !matchMonthDay(t) || !matchMonth(t) {
			continue
		}
		n++
		if t.Format("2006-01-02") >= from {
			result = append(result, t)
		}
	}
	return result
}
