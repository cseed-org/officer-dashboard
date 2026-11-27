package plan

import "testing"

// The examples are the table on
// https://developers.cloudflare.com/cloudflare-one/access-controls/policies/app-paths/.
func TestDestinationCoverageFollowsTheAppPathsPage(t *testing.T) {
	cases := []struct {
		app, dest string
		want      bool
	}{
		{"*.example.com", "alpha.example.com", true},
		{"*.example.com", "beta.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "foo.bar.example.com", false},
		{"example.com", "example.com/alpha", true},
		{"example.com/*", "example.com", true},
		{"example.com/*", "example.com/beta", true},
		{"example.com", "alpha.example.com", false},
		{"*.test.example.com", "test.example.com", false},
		{"*test.example.com", "test.example.com", true},
		{"*test.example.com", "alphatest.example.com", true},
		{"*test.example.com", "beta.test.example.com", false},
		{"example.com/alpha/*", "example.com/alpha", false},
		{"example.com/foo*/bar", "example.com/food/stuff/bar", true},
		// A plain path covers everything below it, and nothing beside it.
		{"officers.example.org/api", "officers.example.org/api/ingest", true},
		{"officers.example.org/mcp", "officers.example.org/mcpx", false},
		{"https://*.Example.org/", "officers.example.org", true},
	}
	for _, c := range cases {
		ah, ap := splitDest(c.app)
		dh, dp := splitDest(c.dest)
		if got := hostMatches(ah, dh) && pathCovers(ap, dp); got != c.want {
			t.Errorf("%s covers %s = %v, want %v", c.app, c.dest, got, c.want)
		}
	}
}

func TestDestinationsOverlapBothWays(t *testing.T) {
	host := "officers.example.org"
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{host + "/mcp", host + "/mcp", true},
		{host + "/mcp/tools", host + "/mcp", true}, // a path app below one of ours
		{host + "/mcp", host + "/mcp/tools", true},
		{host + "/admin", host + "/mcp", false},
		{"*.example.org", host + "/api/ingest", true},
		{"*.example.org/api/*", host + "/api/agent", true},
		{"*.example.org/api/*", host, true}, // the site app covers it
		{"wiki.example.org", host, false},
		{"backups.example.org", host, false},
	} {
		if got := DestinationsOverlap(c.a, c.b); got != c.want {
			t.Errorf("overlap(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if !DestinationOnHost("*.example.org/x", host) || DestinationOnHost("*.other.org", host) {
		t.Error("DestinationOnHost")
	}
}
