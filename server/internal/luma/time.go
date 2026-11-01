package luma

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"officerdashboard/internal/fiscal"
)

var (
	offsetRE = regexp.MustCompile(`(?i)(?:z|[+-]\d{2}:?\d{2})$`)
	isoRE    = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?)?$`)
	usRE     = regexp.MustCompile(`(?i)^(\d{1,2})/(\d{1,2})/(\d{4})\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(AM|PM)?$`)
)

var offsetLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05Z0700", "2006-01-02T15:04Z07:00", "2006-01-02T15:04Z0700",
	"2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05Z0700", "2006-01-02 15:04Z07:00", "2006-01-02 15:04Z0700",
}

// ISO renders an instant the way JS toISOString does: UTC with milliseconds.
func ISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// ParseInstant reads an ISO datetime with an offset (as stored in events.starts_at).
func ParseInstant(s string) (time.Time, error) {
	for _, l := range offsetLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("Invalid timestamp: %s", s)
}

// CSVTimestamp normalises an export timestamp to ISO UTC. Values without an offset are
// Washington local time; API ingest always requires an explicit offset.
func CSVTimestamp(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if offsetRE.MatchString(value) {
		t, err := ParseInstant(value)
		if err != nil {
			return "", fmt.Errorf("Invalid timestamp: %s", value)
		}
		return ISO(t), nil
	}
	iso, us := isoRE.FindStringSubmatch(value), usRE.FindStringSubmatch(value)
	if iso == nil && us == nil {
		return "", fmt.Errorf("Ambiguous timestamp; use ISO 8601 or MM/DD/YYYY HH:mm:ss: %s", value)
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	var y, mo, d, h, mi, sec, ms int
	if iso != nil {
		y, mo, d, h, mi, sec = num(iso[1]), num(iso[2]), num(iso[3]), num(iso[4]), num(iso[5]), num(iso[6])
		if frac := iso[7]; frac != "" {
			ms = num(frac + strings.Repeat("0", 3-len(frac)))
		}
	} else {
		y, mo, d, h, mi, sec = num(us[3]), num(us[1]), num(us[2]), num(us[4]), num(us[5]), num(us[6])
		if us[7] != "" {
			if h < 1 || h > 12 {
				return "", fmt.Errorf("Invalid 12-hour timestamp")
			}
			h = h % 12
			if strings.EqualFold(us[7], "PM") {
				h += 12
			}
		}
	}
	if mo < 1 || mo > 12 || h > 23 || mi > 59 || sec > 59 {
		return "", fmt.Errorf("Invalid timestamp: %s", value)
	}
	desired := time.Date(y, time.Month(mo), d, h, mi, sec, ms*1e6, time.UTC)
	if desired.Year() != y || int(desired.Month()) != mo || desired.Day() != d {
		return "", fmt.Errorf("Invalid timestamp: %s", value)
	}
	// Try both standard and daylight offsets; ambiguous autumn times take the earlier instant.
	var best time.Time
	found := false
	for _, hours := range []int{7, 8} {
		cand := desired.Add(time.Duration(hours) * time.Hour)
		w := cand.In(fiscal.Pacific)
		if w.Year() == y && int(w.Month()) == mo && w.Day() == d && w.Hour() == h && w.Minute() == mi && w.Second() == sec {
			if !found || cand.Before(best) {
				best, found = cand, true
			}
		}
	}
	if !found {
		return "", fmt.Errorf("Nonexistent Pacific local time; supply an offset: %s", value)
	}
	return ISO(best), nil
}
