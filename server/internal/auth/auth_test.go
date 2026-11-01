package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestHasRole(t *testing.T) {
	for _, tc := range []struct {
		actual, required string
		want             bool
	}{
		{"leadership", "director", true}, {"leadership", "leadership", true}, {"leadership", "officer", true},
		{"director", "leadership", false}, {"officer", "leadership", false}, {"bogus", "bogus", false}, {"leadership", "bogus", false},
	} {
		if got := HasRole(tc.actual, tc.required); got != tc.want {
			t.Errorf("HasRole(%q,%q)=%v", tc.actual, tc.required, got)
		}
	}

	if !HasRole("director", "officer") || !HasRole("director", "director") || !HasRole("officer", "officer") {
		t.Error("directors do everything officers can")
	}
	if HasRole("officer", "director") {
		t.Error("officers stay out of director actions")
	}
}

func TestRequiredRoleForPath(t *testing.T) {
	cases := map[string]string{
		"/login": "", "/auth/callback": "", "/api/health": "",
		"/admin/users": "director", "/admin": "director", "/api/admin/tokens": "director",
		"/administrivia": "officer", "/": "officer", "/requests/new": "officer", "/api/overview": "officer",
		"/api/ingest/luma": "", "/api/agent/jobs": "",
	}
	for path, want := range cases {
		if got := RequiredRoleForPath(path); got != want {
			t.Errorf("RequiredRoleForPath(%q) = %q want %q", path, got, want)
		}
	}
}

func TestDecideLogin(t *testing.T) {
	officer := &KnownUser{ID: "u1", Role: "officer", Active: true}
	if d := DecideLogin("42", officer, ""); d.Kind != SignIn || d.UserID != "u1" {
		t.Errorf("active user: %+v", d)
	}
	if d := DecideLogin("42", &KnownUser{ID: "u1", Active: false}, "42"); d.Kind != Deny || d.Reason != "inactive" {
		t.Errorf("inactive: %+v", d)
	}
	if d := DecideLogin("42", nil, "7"); d.Kind != Deny || d.Reason != "not_allowlisted" {
		t.Errorf("unknown: %+v", d)
	}
	if d := DecideLogin("7", nil, "7"); d.Kind != CreateLeadership {
		t.Errorf("bootstrap: %+v", d)
	}
	if d := DecideLogin("", nil, ""); d.Kind != Deny || d.Reason != "not_allowlisted" {
		t.Errorf("empty bootstrap: %+v", d)
	}
}

func TestSafeNextKeepsAppPaths(t *testing.T) {
	for _, in := range []string{"/", "/requests", "/requests/new?event=e1#form", "/events/a%20b"} {
		if got := SafeNext(in); got != in {
			t.Errorf("SafeNext(%q) = %q", in, got)
		}
	}
}

func TestSafeNextFallsBack(t *testing.T) {
	bad := []string{
		"//example.com", "/\\example.com", "/\\/example.com", "/\t/example.com", "/\n/example.com", "/\r/example.com",
		" /requests", "/\x00/example.com", "/\x7f/x", "https://example.com/", "javascript:alert(1)", "requests", "",
		"/.//example.com", "/a/..//example.com", "/%2e//example.com", "/%2E//example.com", "/a/%2e%2e//example.com",
		"//app.invalid//example.com", "https://app.invalid//example.com",
		"/evil", "/api/admin/users", "/auth/dev?as=director", "/eventsfoo",
	}
	for _, in := range bad {
		if got := SafeNext(in); got != "/" {
			t.Errorf("SafeNext(%q) = %q, want /", in, got)
		}
	}
}

func TestSafeNextNeverLeavesTheHost(t *testing.T) {
	for _, in := range []string{"/.//x.com", "/./\\x.com", "/..//x.com", "/%2f/x.com", "/%2F%2Fx.com", "/.%2f/x.com", "///x.com"} {
		out := SafeNext(in)
		u, err := url.Parse("https://officers.cseed.co" + out)
		if err != nil || u.Host != "officers.cseed.co" || strings.HasPrefix(out, "//") || strings.HasPrefix(out, "/\\") {
			t.Errorf("%q → %q leaves the host (%v)", in, out, err)
		}
	}
	if got := SafeNext("/a/../../requests"); got != "/requests" {
		t.Errorf("dot segments: %q", got)
	}
}

func TestTokensAndIDs(t *testing.T) {
	if SHA256Hex("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Error("sha256 mismatch")
	}
	a, b := RandomToken(32), RandomToken(32)
	if a == b || len(a) != 43 {
		t.Errorf("tokens %q %q", a, b)
	}
	id := UUID()
	if len(id) != 36 || id[14] != '4' {
		t.Errorf("uuid %q", id)
	}
}
