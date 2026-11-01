// Package validate replaces the zod schemas of v1: format checks and a small reader that
// collects field errors in the same shape as zod's flatten() so ApiError details match.
package validate

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	emailRE    = regexp.MustCompile(`^(?:[A-Za-z0-9_'+\-]+\.)*[A-Za-z0-9_'+\-]*[A-Za-z0-9_+-]@(?:[A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$`)
	datetimeRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)
	discordRE  = regexp.MustCompile(`^\d{17,20}$`)
)

// IsEmail follows zod 4's default email pattern.
func IsEmail(s string) bool { return emailRE.MatchString(s) }

// IsDate is a strict calendar YYYY-MM-DD (rejects Feb 30, honours leap years).
func IsDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// IsDateTime is an ISO 8601 datetime with seconds and a Z or ±HH:MM offset.
func IsDateTime(s string) bool {
	m := datetimeRE.FindStringSubmatch(s)
	return m != nil && IsDate(m[1])
}

// IsURL accepts anything with a scheme that net/url can parse, like `new URL()`.
func IsURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && u.Scheme != "" && (u.Host != "" || u.Opaque != "")
}

// IsDiscordID is 17–20 digits.
func IsDiscordID(s string) bool { return discordRE.MatchString(s) }
