package auth

import "regexp"

// SetupStatePrefix starts the OAuth2 state of a link printed by `dashboard setup discord
// connect`. The sign-in callback shows such a callback's code for the admin to copy into
// that terminal instead of signing anyone in (api/setuprelay.go).
//
// No sign-in state can start with it: /auth/login makes its state with RandomToken, which
// is unpadded base64url (letters, digits, '-' and '_'), and that alphabet has no '.'.
const SetupStatePrefix = "setup."

var (
	// The prefix, then 32 to 64 base64url characters: setup makes 43 (32 random bytes).
	setupStateRE = regexp.MustCompile(`^setup\.[A-Za-z0-9_-]{32,64}$`)
	// Discord does not document its codes' alphabet or length (today they are 30 letters
	// and digits). This allows URL-unreserved characters only (RFC 3986 §2.3), none of
	// which HTML, a URL or the terminal paste format ("state:code") treats specially.
	setupCodeRE = regexp.MustCompile(`^[A-Za-z0-9._~-]{10,256}$`)
)

// ValidSetupRelay reports whether state and code have the exact shape of a setup
// callback. Anything else, even with the prefix, is not one.
func ValidSetupRelay(state, code string) bool {
	return setupStateRE.MatchString(state) && setupCodeRE.MatchString(code)
}
