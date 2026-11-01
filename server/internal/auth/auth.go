// Package auth holds role rules, login decisions, redirect safety, token hashing and the
// Discord OAuth2 (PKCE) client. It has no database access.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode"
)

const (
	RoleDirector = "director"
	RoleOfficer  = "officer"
)

var rank = map[string]int{RoleOfficer: 1, RoleDirector: 2}

// HasRole: directors can do everything officers can.
func HasRole(actual, required string) bool { return rank[actual] >= rank[required] }

func under(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }

var (
	publicPrefixes       = []string{"/login", "/auth", "/api/health"}
	serviceTokenPrefixes = []string{"/api/ingest", "/api/agent"}
	directorPrefixes     = []string{"/admin", "/api/admin"}
)

// RequiredRoleForPath is the minimum role a signed-in user needs for a path, or "" when the
// session layer should not enforce one (public pages, service-token APIs). Finer rules,
// such as "only directors approve", are enforced in the handler.
func RequiredRoleForPath(path string) string {
	for _, p := range append(append([]string{}, publicPrefixes...), serviceTokenPrefixes...) {
		if under(path, p) {
			return ""
		}
	}
	for _, p := range directorPrefixes {
		if under(path, p) {
			return RoleDirector
		}
	}
	return RoleOfficer
}

// LoginKind is the outcome of an allowlist check.
type LoginKind int

const (
	SignIn LoginKind = iota
	CreateDirector
	Deny
)

// Decision is what to do with a verified Discord account.
type Decision struct {
	Kind   LoginKind
	UserID string
	Reason string // not_allowlisted | inactive (for Deny)
}

// KnownUser is the slice of a users row that decides a login.
type KnownUser struct {
	ID     string
	Role   string
	Active bool
}

// DecideLogin: only users already allowlisted by a director may sign in. The one exception
// is BOOTSTRAP_DIRECTOR_DISCORD_ID, created as a director on first login so a fresh
// deployment has someone to allowlist everybody else.
func DecideLogin(discordID string, user *KnownUser, bootstrapDirectorID string) Decision {
	if user != nil {
		if user.Active {
			return Decision{Kind: SignIn, UserID: user.ID}
		}
		return Decision{Kind: Deny, Reason: "inactive"}
	}
	if b := strings.TrimSpace(bootstrapDirectorID); b != "" && discordID == b {
		return Decision{Kind: CreateDirector}
	}
	return Decision{Kind: Deny, Reason: "not_allowlisted"}
}

// RedirectSections are the top-level page sections a post-login redirect may land in (besides "/").
var RedirectSections = []string{"events", "requests", "finance", "admin"}

// SafeNext returns the post-login redirect target, or "/" unless it is one of the app's own
// pages. Layers, each enough on its own for the cases it covers:
//  1. refuse whitespace, control characters and backslashes (browsers strip tabs/newlines
//     and treat '\' as '/', so "/\t/evil.com" would become "//evil.com");
//  2. refuse anything that isn't a single-slash path;
//  3. normalise dot segments (including %2e); the result must not start with "//", since
//     "/.//evil.com" normalises to "//evil.com", which browsers read as another host;
//  4. allowlist the first path segment so /api/* and /auth/* are never targets.
func SafeNext(next string) string {
	if next == "" {
		return "/"
	}
	for _, r := range next {
		if unicode.IsSpace(r) || r < 0x20 || r == 0x7f || r == '\\' {
			return "/"
		}
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	rest := ""
	path := next
	if i := strings.IndexAny(next, "?#"); i >= 0 {
		path, rest = next[:i], next[i:]
	}
	path = normalisePath(path)
	if strings.HasPrefix(path, "//") || strings.HasPrefix(path, "/\\") {
		return "/"
	}
	if path != "/" {
		parts := strings.Split(path, "/")
		section := ""
		if len(parts) > 1 {
			section = parts[1]
		}
		ok := false
		for _, s := range RedirectSections {
			if s == section {
				ok = true
			}
		}
		if !ok {
			return "/"
		}
	}
	// A query or fragment cannot change the host, but make sure it round-trips.
	if rest != "" {
		if _, err := url.Parse("https://app.invalid" + path + rest); err != nil {
			return "/"
		}
	}
	return path + rest
}

// normalisePath applies WHATWG dot-segment removal, treating %2e as a dot.
func normalisePath(p string) string {
	isSingle := func(s string) bool { s = strings.ToLower(s); return s == "." || s == "%2e" }
	isDouble := func(s string) bool {
		s = strings.ToLower(s)
		return s == ".." || s == ".%2e" || s == "%2e." || s == "%2e%2e"
	}
	segs := strings.Split(p, "/")[1:] // p starts with "/"
	var out []string
	for i, s := range segs {
		last := i == len(segs)-1
		switch {
		case isDouble(s):
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		case isSingle(s):
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, s)
		}
	}
	return "/" + strings.Join(out, "/")
}

// RandomToken is n random bytes, base64url without padding.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// SHA256Hex hashes a secret for storage; only the hash is kept for sessions and tokens.
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// UUID returns a random RFC 4122 v4 UUID.
func UUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
