package validate

import "testing"

func TestFormats(t *testing.T) {
	check := func(name string, f func(string) bool, good, bad []string) {
		for _, s := range good {
			if !f(s) {
				t.Errorf("%s(%q) should be valid", name, s)
			}
		}
		for _, s := range bad {
			if f(s) {
				t.Errorf("%s(%q) should be invalid", name, s)
			}
		}
	}
	check("date", IsDate, []string{"2026-10-31", "2028-02-29"}, []string{"2027-02-29", "2026-13-01", "7/1/2026", "2026-1-1", ""})
	check("datetime", IsDateTime,
		[]string{"2026-11-01T10:00:00Z", "2026-09-18T18:00:00-07:00", "2026-11-01T10:00:00.123Z"},
		[]string{"2026-11-01T10:00Z", "2026-11-01T10:00:00", "2026-11-01 10:00:00Z", "2026-11-01T10:00:00-0700", "2026-02-30T10:00:00Z"})
	check("email", IsEmail, []string{"a@example.org", "a.b+c@x.co"}, []string{"x", "a@b", "a@@b.org", ".a@b.org", ""})
	check("url", IsURL, []string{"https://www.adafruit.com/", "http://x.y"}, []string{"adafruit", "/path", ""})
	check("discord", IsDiscordID, []string{"100000000000000001"}, []string{"123", "abc", ""})
}
