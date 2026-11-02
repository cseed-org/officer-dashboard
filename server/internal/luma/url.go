package luma

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// CreateURL is where an officer starts a new Luma event by hand (there is no Luma API).
const CreateURL = "https://lu.ma/create"

// ErrEventURL explains what a pasted Luma link must look like.
var ErrEventURL = errors.New("Use the HTTPS link to a Luma event page, like https://lu.ma/your-event")

var (
	eventHosts = map[string]bool{"lu.ma": true, "www.lu.ma": true, "luma.com": true, "www.luma.com": true}
	// A public event slug (lu.ma/founder-stories, lu.ma/abc123xy) or an event id page
	// (lu.ma/event/evt-…). Luma slugs are letters, digits and hyphens.
	slugRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{1,99}$`)
	eventIDRe = regexp.MustCompile(`^evt-[A-Za-z0-9]{4,64}$`)
	// First path segments that are Luma's own pages, not events.
	reservedSlugs = map[string]bool{
		"create": true, "discover": true, "calendar": true, "calendars": true, "home": true, "signin": true, "signup": true,
		"login": true, "logout": true, "settings": true, "explore": true, "user": true, "u": true, "event": true, "events": true,
		"pricing": true, "help": true, "about": true, "terms": true, "privacy": true, "map": true, "search": true, "dashboard": true,
		"check-in": true, "checkin": true, "manage": true, "embed": true, "api": true, "ios": true, "android": true, "download": true,
		"careers": true, "blog": true, "changelog": true, "features": true, "community": true, "library": true, "pay": true,
	}
)

// NormalizeEventURL checks that s is an HTTPS lu.ma or luma.com event page and returns it
// without query, fragment or trailing slash. It accepts /<slug> and /event/evt-<id>.
func NormalizeEventURL(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" {
		return "", ErrEventURL
	}
	host := strings.ToLower(u.Hostname())
	if !eventHosts[host] {
		return "", ErrEventURL
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	switch {
	case len(parts) == 1 && slugRe.MatchString(parts[0]) && !reservedSlugs[strings.ToLower(parts[0])]:
	case len(parts) == 2 && parts[0] == "event" && eventIDRe.MatchString(parts[1]):
	default:
		return "", ErrEventURL
	}
	return "https://" + host + "/" + strings.Join(parts, "/"), nil
}
