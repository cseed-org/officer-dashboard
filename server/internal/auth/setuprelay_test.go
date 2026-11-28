package auth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

var relayNow = time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

func mint(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	s, err := MintSetupState(secret, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSetupStateIsAcceptedOnlyWhileFreshAndWithTheSameSecret(t *testing.T) {
	const secret = "the-discord-client-secret-value"
	s := mint(t, secret, relayNow)
	if !strings.HasPrefix(s, SetupStatePrefix) || len(s) != len(SetupStatePrefix)+54 {
		t.Fatalf("state %q", s)
	}
	for _, at := range []time.Duration{0, time.Second, 9*time.Minute + 59*time.Second} {
		if !VerifySetupState(secret, s, relayNow.Add(at)) {
			t.Errorf("refused %s after minting", at)
		}
	}
	for _, at := range []time.Duration{SetupStateTTL, SetupStateTTL + time.Second, time.Hour} {
		if VerifySetupState(secret, s, relayNow.Add(at)) {
			t.Errorf("accepted %s after minting (expired)", at)
		}
	}
	if VerifySetupState(secret+"x", s, relayNow) || VerifySetupState("another-secret", s, relayNow) {
		t.Error("accepted under another secret")
	}
	if VerifySetupState("", s, relayNow) {
		t.Error("no secret configured must mean no relay")
	}
	// Without a secret, a state signed under the empty key (which anyone can make) still
	// verifies nothing.
	forged := signSetupState(deriveSetupKey(""), relayNow.Add(time.Minute).Unix(), bytes.Repeat([]byte{1}, 16))
	if VerifySetupState("", forged, relayNow) || VerifySetupState(" ", forged, relayNow) {
		t.Error("a state signed with the empty secret verified")
	}
	if _, err := MintSetupState("", relayNow, nil); err == nil {
		t.Error("minted with no secret")
	}
	if a, b := mint(t, secret, relayNow), mint(t, secret, relayNow); a == b {
		t.Error("two states share a nonce")
	}
}

func TestSetupStateRefusesTamperingAndAFarExpiry(t *testing.T) {
	const secret = "the-discord-client-secret-value"
	s := mint(t, secret, relayNow)
	body := s[len(SetupStatePrefix):]
	// Changing any one character (expiry, nonce or MAC) breaks it.
	for i := range body {
		b := []byte(body)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		if VerifySetupState(secret, SetupStatePrefix+string(b), relayNow) {
			t.Errorf("accepted with character %d changed", i)
		}
	}
	// A correctly signed state whose expiry is more than SetupStateTTL ahead is refused:
	// a leaked secret-holder's long-lived state, or a clock far off.
	far := signSetupState(deriveSetupKey(secret), relayNow.Add(SetupStateTTL+time.Minute).Unix(), bytes.Repeat([]byte{7}, 16))
	if VerifySetupState(secret, far, relayNow) {
		t.Error("accepted an expiry beyond the TTL")
	}
	if !VerifySetupState(secret, far, relayNow.Add(2*time.Minute)) {
		t.Error("the same state should pass once inside the window (the test's signing is wrong)")
	}
	for name, bad := range map[string]string{
		"no prefix":      body,
		"prefix only":    SetupStatePrefix,
		"short":          s[:len(s)-1],
		"long":           s + "A",
		"padding":        s[:len(s)-1] + "=",
		"not base64url":  SetupStatePrefix + strings.Repeat("+", 54),
		"prefix in caps": "Setup." + body,
		"newline":        s + "\n",
		"old shape":      SetupStatePrefix + strings.Repeat("A", 43),
	} {
		if VerifySetupState(secret, bad, relayNow) {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The state shows only expiry, nonce and a truncated MAC: neither the secret nor the
// derived key appears in it.
func TestSetupStateCarriesNoKeyMaterial(t *testing.T) {
	const secret = "the-discord-client-secret-value"
	s := mint(t, secret, relayNow)
	raw, err := base64.RawURLEncoding.DecodeString(s[len(SetupStatePrefix):])
	if err != nil || len(raw) != 40 {
		t.Fatalf("decoded %d bytes, %v", len(raw), err)
	}
	key := deriveSetupKey(secret)
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, key[:8]) || bytes.Equal(key, []byte(secret)) {
		t.Error("key material in the state")
	}
}

func TestSetupCodeShape(t *testing.T) {
	if !ValidSetupCode("Abc123def456GHI789jkl012MNO345") {
		t.Fatal("a Discord-shaped code must be accepted")
	}
	for name, code := range map[string]string{
		"empty": "", "short": "abc123", "long": strings.Repeat("a", 257), "markup": "abc123def456<script>",
		"quote": `abc123def456"x`, "space": "abc123 def456", "colon": "abc123:def456", "non-ASCII": "abc123def456é",
		"newline": "abc123def456\n", "percent": "abc123def456%3C", "ampersand": "abc123def456&x=1",
	} {
		if ValidSetupCode(code) {
			t.Errorf("%s: accepted %q", name, code)
		}
	}
}

// Sign-in states are RandomToken output: unpadded base64url, whose alphabet has no '.', so
// none can start with SetupStatePrefix.
func TestSignInStatesNeverLookLikeSetupStates(t *testing.T) {
	for range 2000 {
		s := RandomToken(32)
		if strings.HasPrefix(s, SetupStatePrefix) || strings.ContainsRune(s, '.') {
			t.Fatalf("RandomToken gave %q", s)
		}
	}
}
