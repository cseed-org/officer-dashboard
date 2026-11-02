// Package calendar delivers dashboard events to officers' calendars: RFC 5545 .ics
// invitations by email, or Google Calendar through each officer's OAuth grant.
package calendar

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Event is what gets put on a calendar.
type Event struct {
	UID         string // stable across updates
	Sequence    int    // bump on every update so clients replace the old copy
	Summary     string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	Cancelled   bool
}

// Window parses the dashboard's ISO start/end. A missing or invalid end becomes one hour.
func Window(startsAt string, endsAt *string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339, startsAt)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end := start.Add(time.Hour)
	if endsAt != nil {
		if e, err := time.Parse(time.RFC3339, *endsAt); err == nil && e.After(start) {
			end = e
		}
	}
	return start, end, nil
}

func icsText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// fold splits content lines at 75 octets without breaking a UTF-8 sequence.
func fold(line string) string {
	if len(line) <= 75 {
		return line
	}
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = 74 // the leading space counts
	}
	b.WriteString(line)
	return b.String()
}

func icsTime(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// ICS renders a METHOD:REQUEST (or CANCEL) invitation from organizer to attendee.
func ICS(e Event, organizer, attendee string, now time.Time) string {
	method, status := "REQUEST", "CONFIRMED"
	if e.Cancelled {
		method, status = "CANCEL", "CANCELLED"
	}
	lines := []string{
		"BEGIN:VCALENDAR",
		"PRODID:-//CSEED//Officer Dashboard//EN",
		"VERSION:2.0",
		"CALSCALE:GREGORIAN",
		"METHOD:" + method,
		"BEGIN:VEVENT",
		"UID:" + icsText(e.UID),
		fmt.Sprintf("SEQUENCE:%d", e.Sequence),
		"DTSTAMP:" + icsTime(now),
		"DTSTART:" + icsTime(e.Start),
		"DTEND:" + icsTime(e.End),
		"SUMMARY:" + icsText(e.Summary),
		"STATUS:" + status,
		"ORGANIZER;CN=CSEED Officer Dashboard:mailto:" + organizer,
		"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:" + attendee,
	}
	if e.Location != "" {
		lines = append(lines, "LOCATION:"+icsText(e.Location))
	}
	if e.Description != "" {
		lines = append(lines, "DESCRIPTION:"+icsText(e.Description))
	}
	if e.URL != "" {
		lines = append(lines, "URL:"+icsText(e.URL))
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	for i, l := range lines {
		lines[i] = fold(l)
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}
