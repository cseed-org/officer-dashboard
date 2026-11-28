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
// /auth/callback and whose state it minted with auth.MintSetupState: an expiry, a nonce
// and an HMAC under a key derived from the Discord client secret. Discord sends the
// admin's browser back here with a code; this page shows it, and the admin pastes it into
// that terminal.
//
// The page is shown only for a state this server verifies with its own client secret
// (auth.VerifySetupState): a state anyone could choose would let an attacker swap the
// state of their own sign-in link, lure an officer to approve it, and get the officer's
// sign-in code shown (review of #128, F-1). No client secret configured, no relay.
//
// The page does nothing else: no session, no cookie, no token exchange, no database write
// and no redirect. withAuth skips its session lookup for it (so no renewal cookie or
// write either), and authCallback hands over before touching its cookies. Any other
// request goes the normal sign-in path, which refuses a `setup.` state as a bad state.

// setupRelay returns the state and code of a setup callback request, or ok false. It
// matches exactly the request the router sends to authCallback: GET (HEAD has no page to
// show), the path /auth/callback as sent, with no escaped form.
func (s *Server) setupRelay(r *http.Request) (state, code string, ok bool) {
	if r.Method != http.MethodGet || r.URL.Path != "/auth/callback" || r.URL.RawPath != "" {
		return "", "", false
	}
	d := s.cfg.Discord
	if d == nil || d.ClientSecret == "" {
		return "", "", false
	}
	q := r.URL.Query()
	if len(q["state"]) != 1 || len(q["code"]) != 1 {
		return "", "", false
	}
	state, code = q.Get("state"), q.Get("code")
	if !auth.ValidSetupCode(code) || !auth.VerifySetupState(d.ClientSecret, state, s.now()) {
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
<h1>Dashboard setup code for <code>scripts/setup discord connect</code></h1>
<p>Paste this only into the terminal where <strong>you yourself</strong> just ran <code>scripts/setup discord connect</code>.
Ctrl+A (⌘A on a Mac) in the box selects all of it.</p>
<p><textarea readonly autofocus rows="3" aria-label="setup code">{{.}}</textarea></p>
<p><strong>Never send it to anyone</strong>: not to another officer, not to "support", not in chat or email. Someone who has it
may be able to sign in as you.</p>
<p><small>If you did not just start dashboard setup yourself, close this tab and tell the dashboard owner.</small></p>
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
