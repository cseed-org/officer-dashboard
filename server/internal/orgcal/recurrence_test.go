package orgcal

import (
	"strings"
	"testing"
	"time"
)

func TestOccurrencesManualRules(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name, start, rule, from, to string
		want                        []string
	}{
		{"daily interval count", "2027-01-30 10:00", "RRULE:FREQ=DAILY;INTERVAL=3;COUNT=4", "2027-01-01", "2027-03-01",
			[]string{"2027-01-30T18:00:00Z", "2027-02-02T18:00:00Z", "2027-02-05T18:00:00Z", "2027-02-08T18:00:00Z"}},
		{"count includes instances before range", "2027-01-01 10:00", "RRULE:FREQ=DAILY;COUNT=5", "2027-01-03", "2027-02-01",
			[]string{"2027-01-03T18:00:00Z", "2027-01-04T18:00:00Z", "2027-01-05T18:00:00Z"}},
		{"weekly until inclusive", "2027-01-04 18:00", "RRULE:FREQ=WEEKLY;UNTIL=20270119T020000Z", "2027-01-01", "2027-03-01",
			[]string{"2027-01-05T02:00:00Z", "2027-01-12T02:00:00Z", "2027-01-19T02:00:00Z"}},
		{"biweekly byday", "2027-01-04 18:00", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;COUNT=5", "2027-01-01", "2027-03-01",
			[]string{"2027-01-05T02:00:00Z", "2027-01-07T02:00:00Z", "2027-01-19T02:00:00Z", "2027-01-21T02:00:00Z", "2027-02-02T02:00:00Z"}},
		{"biweekly sunday uses monday week start", "2027-01-04 10:00", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU,MO;COUNT=4", "2027-01-01", "2027-03-01",
			[]string{"2027-01-04T18:00:00Z", "2027-01-10T18:00:00Z", "2027-01-18T18:00:00Z", "2027-01-24T18:00:00Z"}},
		{"pulled wkst sunday", "2027-01-04 10:00", "RRULE:FREQ=WEEKLY;WKST=SU;INTERVAL=2;BYDAY=SU,MO;COUNT=4", "2027-01-01", "2027-03-01",
			[]string{"2027-01-04T18:00:00Z", "2027-01-17T18:00:00Z", "2027-01-18T18:00:00Z", "2027-01-31T18:00:00Z"}},
		{"monthly same day", "2027-01-15 09:00", "RRULE:FREQ=MONTHLY;COUNT=3", "2027-01-01", "2028-01-01",
			[]string{"2027-01-15T17:00:00Z", "2027-02-15T17:00:00Z", "2027-03-15T16:00:00Z"}},
		{"monthly 31st skips short months", "2027-01-31 09:00", "RRULE:FREQ=MONTHLY;COUNT=4", "2027-01-01", "2028-01-01",
			[]string{"2027-01-31T17:00:00Z", "2027-03-31T16:00:00Z", "2027-05-31T16:00:00Z", "2027-07-31T16:00:00Z"}},
		{"monthly interval", "2027-01-31 09:00", "RRULE:FREQ=MONTHLY;INTERVAL=2;UNTIL=20270801T000000Z", "2027-01-01", "2028-01-01",
			[]string{"2027-01-31T17:00:00Z", "2027-03-31T16:00:00Z", "2027-05-31T16:00:00Z", "2027-07-31T16:00:00Z"}},
		{"weekly keeps wall time across spring DST", "2027-03-08 10:00", "RRULE:FREQ=WEEKLY;COUNT=3", "2027-03-01", "2027-04-01",
			[]string{"2027-03-08T18:00:00Z", "2027-03-15T17:00:00Z", "2027-03-22T17:00:00Z"}},
		{"daily keeps wall time across fall DST", "2027-11-06 10:00", "RRULE:FREQ=DAILY;COUNT=2", "2027-11-01", "2027-12-01",
			[]string{"2027-11-06T17:00:00Z", "2027-11-07T18:00:00Z"}},
		{"skipped DST hour does not shift later instances", "2027-03-13 02:30", "RRULE:FREQ=DAILY;COUNT=3", "2027-03-01", "2027-04-01",
			[]string{"2027-03-13T10:30:00Z", "2027-03-14T10:30:00Z", "2027-03-15T09:30:00Z"}},
		{"date-only until", "2027-01-04 10:00", "RRULE:FREQ=WEEKLY;UNTIL=20270111", "2027-01-01", "2027-03-01",
			[]string{"2027-01-04T18:00:00Z", "2027-01-11T18:00:00Z"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := []string{}
			for _, v := range Occurrences(at(c.start), c.rule, c.from, c.to) {
				got = append(got, v.UTC().Format(time.RFC3339))
			}
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Fatalf("got  %v\nwant %v", got, c.want)
			}
		})
	}
}
