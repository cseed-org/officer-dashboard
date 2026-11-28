package auth

import (
	"strings"
	"testing"
)

func TestSetupRelayAcceptsOnlyTheSetupShape(t *testing.T) {
	state := "setup." + strings.Repeat("A", 43)
	code := "Abc123def456GHI789jkl012MNO345"
	if !ValidSetupRelay(state, code) {
		t.Fatal("a setup state and a Discord-shaped code must be accepted")
	}
	for name, c := range map[string]struct{ state, code string }{
		"no prefix":           {strings.Repeat("A", 49), code},
		"prefix in caps":      {"Setup." + strings.Repeat("A", 43), code},
		"prefix only":         {"setup.", code},
		"short state":         {"setup." + strings.Repeat("A", 31), code},
		"long state":          {"setup." + strings.Repeat("A", 65), code},
		"dot in state body":   {"setup." + strings.Repeat("A", 40) + ".AA", code},
		"markup in state":     {"setup." + strings.Repeat("A", 40) + "<b>", code},
		"empty code":          {state, ""},
		"short code":          {state, "abc123"},
		"long code":           {state, strings.Repeat("a", 257)},
		"markup in code":      {state, "abc123def456<script>"},
		"quote in code":       {state, `abc123def456"x`},
		"space in code":       {state, "abc123 def456"},
		"colon in code":       {state, "abc123:def456"},
		"non-ASCII in code":   {state, "abc123def456é"},
		"newline in code":     {state, "abc123def456\n"},
		"percent in code":     {state, "abc123def456%3C"},
		"ampersand in code":   {state, "abc123def456&x=1"},
		"newline after state": {state + "\n", code},
	} {
		if ValidSetupRelay(c.state, c.code) {
			t.Errorf("%s: accepted %q / %q", name, c.state, c.code)
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
