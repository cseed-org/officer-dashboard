package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

// SetupStatePrefix starts the OAuth2 state of a link printed by `dashboard setup discord
// connect`. The sign-in callback shows such a callback's code for the admin to copy into
// that terminal instead of signing anyone in (api/setuprelay.go), but only when the state
// verifies (VerifySetupState): it must have been minted with this install's Discord
// client secret, which only the server and the setup terminal hold, within SetupStateTTL.
//
// No sign-in state can start with the prefix: /auth/login makes its state with
// RandomToken, which is unpadded base64url (letters, digits, '-' and '_'), and that
// alphabet has no '.'.
const SetupStatePrefix = "setup."

// SetupStateTTL is how long a setup link's state is accepted, and the furthest ahead its
// expiry may lie.
const SetupStateTTL = 10 * time.Minute

const (
	// Domain separation: the derived key is used for nothing else, and the client secret is
	// never used as a MAC key directly.
	setupKeyLabel = "officer-dashboard setup relay v1"
	setupMACLabel = "setup-relay/v1"
	setupNonceLen = 16
	// The MAC is HMAC-SHA256 truncated to 16 bytes (128 bits). Forging a state means
	// guessing it: one chance in 2^128 per try, and /auth/* allows 30 tries a minute per
	// IP. Truncation does not weaken HMAC beyond its output length (RFC 2104 §5 allows
	// truncation to at least half the hash length, here exactly half).
	setupMACLen = 16
	setupRawLen = 8 + setupNonceLen + setupMACLen // 40 bytes, 54 base64url characters
)

// Discord does not document its codes' alphabet or length (today they are 30 letters and
// digits). This allows URL-unreserved characters only (RFC 3986 §2.3), none of which HTML,
// a URL or the terminal paste format ("state:code") treats specially.
var setupCodeRE = regexp.MustCompile(`^[A-Za-z0-9._~-]{10,256}$`)

// ValidSetupCode reports whether code has the shape setup accepts.
func ValidSetupCode(code string) bool { return setupCodeRE.MatchString(code) }

// deriveSetupKey is K = HMAC-SHA256(key = client secret, message = label). HMAC-SHA256 is
// a pseudorandom function keyed by the secret, so K reveals nothing about the secret, and
// the states, which show only MACs under K, reveal nothing about K.
func deriveSetupKey(secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(setupKeyLabel))
	return m.Sum(nil)
}

func setupMAC(key []byte, expiry int64, nonce []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(setupMACLabel))
	var e [8]byte
	binary.BigEndian.PutUint64(e[:], uint64(expiry))
	m.Write(e[:])
	m.Write(nonce)
	return m.Sum(nil)[:setupMACLen]
}

func signSetupState(key []byte, expiry int64, nonce []byte) string {
	raw := make([]byte, 0, setupRawLen)
	raw = binary.BigEndian.AppendUint64(raw, uint64(expiry))
	raw = append(raw, nonce...)
	raw = append(raw, setupMAC(key, expiry, nonce)...)
	return SetupStatePrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// MintSetupState makes a setup state: the prefix, then base64url(expiry ‖ nonce ‖ mac)
// with the expiry SetupStateTTL after now (Unix seconds, big-endian), a 16-byte random
// nonce, and mac = HMAC-SHA256(K, label ‖ expiry ‖ nonce)[:16]. random is nil for
// crypto/rand.
func MintSetupState(secret string, now time.Time, random io.Reader) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("no Discord client secret to sign the setup link with")
	}
	if random == nil {
		random = rand.Reader
	}
	nonce := make([]byte, setupNonceLen)
	if _, err := io.ReadFull(random, nonce); err != nil {
		return "", errors.New("no random numbers for the setup link")
	}
	return signSetupState(deriveSetupKey(secret), now.Add(SetupStateTTL).Unix(), nonce), nil
}

// SetupStateExpiry is when a well-formed setup state stops being accepted. It does not
// verify the state.
func SetupStateExpiry(state string) (time.Time, bool) {
	raw, ok := decodeSetupState(state)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(binary.BigEndian.Uint64(raw[:8])), 0), true
}

func decodeSetupState(state string) ([]byte, bool) {
	body, ok := strings.CutPrefix(state, SetupStatePrefix)
	if !ok || len(body) != base64.RawURLEncoding.EncodedLen(setupRawLen) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(body)
	if err != nil || len(raw) != setupRawLen {
		return nil, false
	}
	return raw, true
}

// VerifySetupState reports whether state was minted with secret and is still fresh at
// now: the MAC matches (constant time), the expiry is after now and at most SetupStateTTL
// ahead. An empty secret verifies nothing.
func VerifySetupState(secret, state string, now time.Time) bool {
	if strings.TrimSpace(secret) == "" {
		return false
	}
	raw, ok := decodeSetupState(state)
	if !ok {
		return false
	}
	expiry := int64(binary.BigEndian.Uint64(raw[:8]))
	nonce, mac := raw[8:8+setupNonceLen], raw[8+setupNonceLen:]
	if !hmac.Equal(mac, setupMAC(deriveSetupKey(secret), expiry, nonce)) {
		return false
	}
	left := expiry - now.Unix()
	return left > 0 && left <= int64(SetupStateTTL/time.Second)
}
