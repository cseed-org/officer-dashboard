package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesiredMatchesTheDeployDocs fails when docs/deploy changes a name, path or rule
// expression that Desired copies, so the code and the manual guide cannot drift apart
// silently. Each check is the text as the doc spells it, with <your-hostname> filled in;
// runs of whitespace compare equal, since the docs wrap lines.
func TestDesiredMatchesTheDeployDocs(t *testing.T) {
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "deploy", name))
		if err != nil {
			t.Fatal(err)
		}
		return squash(string(raw))
	}
	cloudflare, fresh := read("cloudflare.md"), read("fresh-install.md")
	const host = "officers.example.org"
	d, err := NewDesired(Inputs{AppURL: "https://" + host, LaunchpadGroup: "Launchpad"})
	if err != nil {
		t.Fatal(err)
	}
	doc := func(s string) string { return strings.ReplaceAll(s, host, "<your-hostname>") }
	want := map[string][]string{
		"cloudflare.md": {
			"Name it `" + d.Tunnel.Name + "`",
			"**Service URL:** `" + d.Tunnel.Service + "`",
			"Name `" + d.Groups[0].Name + "`",
			"Name `" + d.Groups[1].Name + "`",
			"**Application name:** `" + d.Apps[0].Name + "`",
			"Name `" + d.Apps[0].Policy.Name + "`, Action **Allow**",
			"**Application name:** `" + d.Apps[1].Name + "`",
			"**Application name:** `" + d.Apps[2].Name + "`",
			"a new policy named `" + d.Apps[2].Policy.Name + "`",
			"Name it `" + d.ServiceTokens[0].Name + "`",
			"**Rule name:** `" + d.Rules[0].Description + "`",
			"**Rule name:** `" + d.Rules[1].Description + "`",
			"**Rule name:** `" + d.Rules[2].Description + "`",
			doc(d.Rules[0].Expression),
			doc(d.Rules[2].Expression),
			"Paths `api/ingest` and `api/agent`",
		},
		"fresh-install.md": {
			"Create a D1 database named `" + d.D1Database + "`",
			"service token named `" + d.ServiceTokens[1].Name + "`",
			"npx wrangler secret put " + d.Worker.Secret,
			"Keep `workers_dev` and `preview_urls` disabled",
		},
	}
	for _, dest := range d.Apps[1].Destinations {
		want["cloudflare.md"] = append(want["cloudflare.md"], "   - `"+strings.TrimPrefix(dest, host+"/")+"`")
	}
	for name, lines := range want {
		text := map[string]string{"cloudflare.md": cloudflare, "fresh-install.md": fresh}[name]
		for _, line := range lines {
			if !strings.Contains(text, squash(line)) {
				t.Errorf("docs/deploy/%s no longer says %q; update plan/desired.go or the doc", name, line)
			}
		}
	}
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }
