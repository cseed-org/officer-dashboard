package api

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"

	"officerdashboard/internal/auth"
)

// The setup relay: `dashboard setup discord connect` (internal/setup/discord) prints a
// Discord authorization link whose redirect is this dashboard's registered
// /auth/callback and whose state starts with auth.SetupStatePrefix. Discord sends the
// admin's browser back here with a code; this page shows it, and the admin pastes it into
// that terminal, which alone holds the client secret and the PKCE verifier needed to use
// it.
//
// The page does nothing else: no session, no cookie, no token exchange, no database write
// and no redirect. withAuth skips its session lookup for it (so no renewal cookie or
// write either), and authCallback hands over before touching its cookies. A callback that
// is not exactly setup-shaped (auth.ValidSetupRelay) goes the normal sign-in path, which
// refuses it as a bad state.

// setupRelay returns the state and code of a setup callback request, or ok false.
func setupRelay(r *http.Request) (state, code string, ok bool) {
	if r.Method != http.MethodGet || cleanPath(r.URL.Path) != "/auth/callback" {
		return "", "", false
	}
	q := r.URL.Query()
	if len(q["state"]) != 1 || len(q["code"]) != 1 {
		return "", "", false
	}
	state, code = q.Get("state"), q.Get("code")
	if !auth.ValidSetupRelay(state, code) {
		return "", "", false
	}
	return state, code, true
}

const setupRelayStyle = `body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem;color:#1d1d1f;background:#fff}` +
	`textarea{font:15px/1.4 ui-monospace,monospace;width:100%;padding:.5rem;box-sizing:border-box;resize:none;word-break:break-all;color:inherit;background:transparent;border:1px solid #888}` +
	`small{color:#555}@media (prefers-color-scheme:dark){body{color:#eee;background:#111}small{color:#aaa}}`

// setupRelayCSP allows the one style block by its hash and nothing else: no script, no
// other origin, no form, no frame.
var setupRelayCSP = func() string {
	sum := sha256.Sum256([]byte(setupRelayStyle))
	return "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'; " +
		"frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
}()

var setupRelayPage = template.Must(template.New("relay").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>Dashboard setup code</title><style>` + setupRelayStyle + `</style></head>
<body><main>
<h1>Dashboard setup code</h1>
<p>Copy this setup code into the terminal running <code>scripts/setup discord connect</code>, then close this tab.
Ctrl+A (⌘A on a Mac) in the box selects all of it.</p>
<p><textarea readonly autofocus rows="3" aria-label="setup code">{{.}}</textarea></p>
<p><small>The code works only together with that terminal, which holds the app's secret. Nobody has been signed in.
If you did not start dashboard setup, close this tab: nothing has changed.</small></p>
</main></body></html>
`))

func (c *Ctx) setupRelayPage(state, code string) error {
	h := c.W.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", setupRelayCSP)
	c.W.WriteHeader(http.StatusOK)
	return setupRelayPage.Execute(c.W, state+":"+code)
}
