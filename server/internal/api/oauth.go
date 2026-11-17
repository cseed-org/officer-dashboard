package api

// OAuth 2.1 authorization server for MCP agents (docs/mcp.md).
//
// An officer adds https://<dashboard>/mcp to their agent (Claude Code, claude.ai, Cursor…).
// The agent finds this server through the RFC 9728 / RFC 8414 metadata, registers itself
// (RFC 7591, public client, no secret), and opens /oauth/authorize in the browser. The
// officer signs in to the dashboard as usual (Discord) and clicks Allow; the agent swaps
// the code (PKCE S256, required) for an access token and a rotating refresh token. Every
// MCP call then acts as that officer, with exactly the officer's permissions.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

const (
	oauthCodeTTL    = 2 * time.Minute
	oauthAccessTTL  = time.Hour
	oauthRefreshTTL = 30 * 24 * time.Hour
	// oauthRefreshRaceGrace: replaying the refresh token rotated away less than this long ago
	// is a concurrent refresh by the same client (it loses with invalid_grant), not a leak.
	oauthRefreshRaceGrace = 10 * time.Second
	// oauthMaxNext caps the authorize URL a signed-out officer is sent back to after sign-in:
	// it rides in a cookie (with its name and attributes, under the browser's 4 KB limit).
	oauthMaxNext = 3000
	// mcpScope is the only scope: an agent acts as the officer, nothing narrower or wider.
	mcpScope = "dashboard"
)

// baseURL is the public origin: APP_URL when set, else the request's own host (local dev).
func (c *Ctx) baseURL() string {
	if u := strings.TrimRight(c.S.cfg.AppURL, "/"); u != "" {
		return u
	}
	scheme := "http"
	if c.R.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.R.Host
}

// allowCORS lets browser-based MCP clients (MCP Inspector, web agents) call the bearer-token
// endpoints. Nothing here uses cookies, so a wildcard origin grants no ambient authority.
func allowCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Protocol-Version, Mcp-Session-Id, Last-Event-ID")
	h.Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
	h.Set("Access-Control-Max-Age", "86400")
}

func (c *Ctx) corsPreflight() error {
	allowCORS(c.W)
	c.W.WriteHeader(http.StatusNoContent)
	return nil
}

// oauthProtectedResource is RFC 9728 metadata for /mcp.
func (c *Ctx) oauthProtectedResource() error {
	allowCORS(c.W)
	base := c.baseURL()
	writeJSON(c.W, 200, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{mcpScope},
		"resource_name":            "CSEED Officer Dashboard",
	})
	return nil
}

// oauthServerMetadata is RFC 8414 authorization server metadata.
func (c *Ctx) oauthServerMetadata() error {
	allowCORS(c.W)
	base := c.baseURL()
	writeJSON(c.W, 200, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{mcpScope},
	})
	return nil
}

// oauthError is the RFC 6749 §5.2 JSON error.
func (c *Ctx) oauthError(status int, code, desc string) error {
	allowCORS(c.W)
	c.W.Header().Set("Cache-Control", "no-store")
	writeJSON(c.W, status, map[string]string{"error": code, "error_description": desc})
	return nil
}

// validRedirectURI accepts https, http on a loopback host (native apps, RFC 8252), and
// private-use schemes such as cursor:// or vscode://. Never script-capable schemes.
func validRedirectURI(raw string) bool {
	if len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Fragment != "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopbackHost(u.Hostname())
	case "javascript", "data", "file", "vbscript", "blob", "about", "ftp", "ws", "wss":
		return false
	}
	// Private-use scheme: letters, digits, + - . only (RFC 3986), as clients use reverse-DNS names.
	for _, r := range u.Scheme {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// redirectMatches compares a requested redirect URI with a registered one. Exact match,
// except that a loopback http URI may use any port (RFC 8252 §7.3).
func redirectMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	a, err1 := url.Parse(registered)
	b, err2 := url.Parse(requested)
	if err1 != nil || err2 != nil || a.Scheme != "http" || b.Scheme != "http" || !isLoopbackHost(a.Hostname()) {
		return false
	}
	return a.Hostname() == b.Hostname() && a.Path == b.Path && a.RawQuery == b.RawQuery
}

// oauthRegister is RFC 7591 dynamic client registration for public clients.
func (c *Ctx) oauthRegister() error {
	var body struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.W, c.R.Body, 16<<10))
	if err != nil || json.Unmarshal(raw, &body) != nil {
		return c.oauthError(400, "invalid_client_metadata", "Body must be JSON client metadata")
	}
	if len(body.RedirectURIs) == 0 || len(body.RedirectURIs) > 10 {
		return c.oauthError(400, "invalid_redirect_uri", "Give 1 to 10 redirect_uris")
	}
	for _, u := range body.RedirectURIs {
		if !validRedirectURI(u) {
			return c.oauthError(400, "invalid_redirect_uri", "Unsupported redirect URI: use https, http on localhost, or an app scheme")
		}
	}
	if body.AuthMethod != "" && body.AuthMethod != "none" {
		return c.oauthError(400, "invalid_client_metadata", "Only public clients (token_endpoint_auth_method none) are supported")
	}
	name := cleanClientName(body.ClientName)
	cl := store.OAuthClient{ID: "mcp_" + auth.RandomToken(16), Name: name, RedirectURIs: body.RedirectURIs, CreatedAt: store.Ms(c.S.nowMs())}
	if err := c.S.db.Write(c.Context(), func(w *store.Queries) error { return w.InsertOAuthClient(c.Context(), cl) }); err != nil {
		return err
	}
	allowCORS(c.W)
	writeJSON(c.W, 201, map[string]any{
		"client_id":                  cl.ID,
		"client_id_issued_at":        int64(cl.CreatedAt) / 1000,
		"client_name":                cl.Name,
		"redirect_uris":              cl.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      mcpScope,
	})
	return nil
}

// cleanClientName makes a self-asserted client name safe to show on the consent page:
// no control or invisible format characters (a right-to-left override could make
// "evil" read as something else), single spaces, at most 80 characters.
func cleanClientName(raw string) string {
	var b strings.Builder
	space := false
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || !unicode.IsPrint(r):
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	name := b.String()
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	if name == "" {
		name = "MCP client"
	}
	return name
}

// resourceOK accepts a missing RFC 8707 resource indicator or one naming this server.
func (c *Ctx) resourceOK(v string) bool {
	if v == "" {
		return true
	}
	base := c.baseURL()
	v = strings.TrimRight(v, "/")
	return v == base || v == base+"/mcp"
}

// authzRequest is a validated /oauth/authorize request.
type authzRequest struct {
	client                              *store.OAuthClient
	redirectURI, state, challenge, mode string
}

// parseAuthorize validates the request. When the client or redirect URI is bad it returns
// a message for an error page (never redirect to an unverified URI); other problems are
// reported to the client by redirect.
func (c *Ctx) parseAuthorize(v url.Values) (*authzRequest, string, string) {
	cl, err := c.S.db.Read().OAuthClientByID(c.Context(), v.Get("client_id"))
	if err != nil {
		return nil, "", "This app is not registered with the dashboard. Reconnect it from your agent and try again."
	}
	ru := v.Get("redirect_uri")
	ok := false
	for _, reg := range cl.RedirectURIs {
		if ru == "" && len(cl.RedirectURIs) == 1 {
			ru, ok = reg, true
			break
		}
		if redirectMatches(reg, ru) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, "", "The app asked to return to an address it did not register. Reconnect it from your agent and try again."
	}
	a := &authzRequest{client: cl, redirectURI: ru, state: v.Get("state"), challenge: v.Get("code_challenge"), mode: v.Get("code_challenge_method")}
	if len(a.state) > 1024 {
		a.state = ""
		return a, "invalid_request", ""
	}
	switch {
	case v.Get("response_type") != "code":
		return a, "unsupported_response_type", ""
	case a.challenge == "" || a.mode != "S256" || len(a.challenge) < 43 || len(a.challenge) > 128:
		return a, "invalid_request", ""
	case !c.resourceOK(v.Get("resource")):
		return a, "invalid_target", ""
	}
	return a, "", ""
}

var authzErrorText = map[string]string{
	"invalid_request":           "PKCE is required: send code_challenge (43-128 characters) with code_challenge_method=S256; state may be at most 1024 characters",
	"unsupported_response_type": "Only response_type=code is supported",
	"invalid_target":            "The resource parameter must name this dashboard's /mcp endpoint",
	"access_denied":             "The officer declined",
}

// redirectBack sends the browser to the client's redirect URI with the given parameters.
func (c *Ctx) redirectBack(a *authzRequest, params url.Values) error {
	u, _ := url.Parse(a.redirectURI)
	q := u.Query()
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	if e := params.Get("error"); e != "" && params.Get("error_description") == "" && authzErrorText[e] != "" {
		q.Set("error_description", authzErrorText[e])
	}
	if a.state != "" {
		q.Set("state", a.state)
	}
	q.Set("iss", c.baseURL())
	u.RawQuery = q.Encode()
	c.W.Header().Set("Cache-Control", "no-store")
	http.Redirect(c.W, c.R, u.String(), http.StatusFound)
	return nil
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>{{if .Error}}Cannot connect{{else}}Connect {{.Client}}{{end}} · CSEED</title>
<style>
:root{color-scheme:light dark;--bg:#f6f6f3;--card:#fff;--ink:#16181d;--muted:#5d6370;--line:#e3e3de;--accent:#1f7a4d;--accent-ink:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#121417;--card:#1a1d21;--ink:#eceef1;--muted:#9aa1ad;--line:#2a2e35;--accent:#3fb47a;--accent-ink:#0b1410}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--ink);font:15px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif;padding:1.5rem}
main{width:100%;max-width:27rem;background:var(--card);border:1px solid var(--line);border-radius:14px;padding:1.75rem}
.eyebrow{font-size:.75rem;letter-spacing:.08em;text-transform:uppercase;color:var(--muted);margin:0 0 .5rem}
h1{font-size:1.3rem;line-height:1.3;margin:0 0 1rem}
ul{margin:0 0 1.25rem;padding-left:1.1rem;color:var(--muted)}li{margin:.3rem 0}
.who{display:flex;gap:.6rem;align-items:center;border:1px solid var(--line);border-radius:10px;padding:.6rem .75rem;margin:0 0 1.25rem}
.who small{display:block;color:var(--muted)}
.row{display:flex;gap:.6rem;justify-content:flex-end}
button,a.btn{font:inherit;border-radius:9px;padding:.55rem 1.1rem;border:1px solid var(--line);background:transparent;color:var(--ink);cursor:pointer;text-decoration:none}
button.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-ink);font-weight:600}
code{font-size:.85em;word-break:break-all;text-transform:none;letter-spacing:0}
</style></head><body><main>
<p class="eyebrow">CSEED Officer Dashboard</p>
{{if .Error}}<h1>Cannot connect this app</h1><p>{{.Error}}</p><div class="row"><a class="btn" href="/">Open the dashboard</a></div>
{{else}}<h1>Allow <strong>{{.Client}}</strong> to work in the dashboard as you?</h1>
<div class="who"><div><strong>{{.User}}</strong><small>signed in as {{.Role}}</small></div></div>
<ul>
<li>It can read and change anything you can in the dashboard: events, purchases, design requests, contacts and more.</li>
<li>Everything it does is recorded in the audit log as you, via {{.Client}}.</li>
<li>Disconnect it any time from Settings → Connected agents.</li>
</ul>
<p class="eyebrow">Returns to <code>{{.Redirect}}</code></p>
<form method="post" action="/oauth/authorize">
{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{end}}<div class="row"><button type="submit" name="decision" value="deny">Cancel</button><button class="primary" type="submit" name="decision" value="allow" autofocus>Allow</button></div>
</form>{{end}}
</main></body></html>
`))

func (c *Ctx) renderConsent(status int, data map[string]any) error {
	h := c.W.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	// No script; the inline style and the form post are all it needs. No form-action: browsers
	// apply it to the redirect after posting, which goes to the agent's own (often app-scheme
	// or loopback) redirect URI.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	// Referrer-Policy stays same-origin (withAPIHygiene): no-referrer would make the Allow post
	// send Origin: null, which sameOriginForm rightly refuses. Cross-origin, nothing leaks.
	c.W.WriteHeader(status)
	return consentPage.Execute(c.W, data)
}

// authorizeUser is the signed-in user who may authorize an agent, or a message saying why not.
func (c *Ctx) authorizeUser() (*store.User, string, error) {
	if c.User == nil {
		return nil, "", nil
	}
	u, err := c.S.db.Read().UserByID(c.Context(), c.User.ID)
	if err != nil {
		return nil, "", err
	}
	if !u.Active {
		return nil, "Your dashboard account is inactive.", nil
	}
	pending, err := c.S.onboardingPending(c.Context(), u.ID)
	if err != nil {
		return nil, "", err
	}
	if pending {
		return nil, "Finish setting up your dashboard account first, then connect the app again.", nil
	}
	return u, "", nil
}

// oauthAuthorizeGet shows the consent page, sending the browser to sign-in first if needed.
func (c *Ctx) oauthAuthorizeGet() error {
	a, oerr, page := c.parseAuthorize(c.R.URL.Query())
	if page != "" {
		return c.renderConsent(400, map[string]any{"Error": page})
	}
	if oerr != "" {
		// Never bounce an invalid request to the client's redirect URI: any visitor can
		// register a client, so that would make /oauth/authorize an open redirect
		// (RFC 9700 §4.11.2). The officer sees why instead.
		return c.renderConsent(400, map[string]any{"Error": "The app sent an invalid sign-in request: " + authzErrorText[oerr] + "."})
	}
	u, why, err := c.authorizeUser()
	if err != nil {
		return err
	}
	if why != "" {
		return c.renderConsent(403, map[string]any{"Error": why})
	}
	if u == nil {
		// The request comes back after sign-in through a cookie; past ~4 KB a browser drops
		// it silently and the officer would land on the home page with the agent stuck.
		if len(c.R.URL.RequestURI()) > oauthMaxNext {
			return c.renderConsent(400, map[string]any{"Error": "Sign in to the dashboard in this browser first, then connect the app again."})
		}
		http.Redirect(c.W, c.R, "/login?next="+url.QueryEscape(c.R.URL.RequestURI()), http.StatusFound)
		return nil
	}
	fields := []formField{{"client_id", a.client.ID}, {"redirect_uri", a.redirectURI}, {"state", a.state}, {"code_challenge", a.challenge}, {"code_challenge_method", a.mode}, {"response_type", "code"}}
	shown := a.redirectURI
	if p, err := url.Parse(a.redirectURI); err == nil {
		shown = p.Scheme + "://" + p.Host + p.Path
	}
	return c.renderConsent(200, map[string]any{"Client": a.client.Name, "User": u.Name, "Role": u.Role, "Redirect": shown, "Fields": fields})
}

// oauthAuthorizePost records the officer's decision and issues a code on Allow.
func (c *Ctx) oauthAuthorizePost() error {
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, 16<<10)
	if err := c.R.ParseForm(); err != nil {
		return badRequest("Invalid form")
	}
	// Session cookies are SameSite=Lax, so a cross-site post arrives signed out anyway;
	// this also refuses one from a same-site sibling.
	if !c.sameOriginForm() {
		return forbidden("Cross-origin request blocked")
	}
	a, oerr, page := c.parseAuthorize(c.R.PostForm)
	if page != "" {
		return c.renderConsent(400, map[string]any{"Error": page})
	}
	if oerr != "" {
		// Never bounce an invalid request to the client's redirect URI: any visitor can
		// register a client, so that would make /oauth/authorize an open redirect
		// (RFC 9700 §4.11.2). The officer sees why instead.
		return c.renderConsent(400, map[string]any{"Error": "The app sent an invalid sign-in request: " + authzErrorText[oerr] + "."})
	}
	u, why, err := c.authorizeUser()
	if err != nil {
		return err
	}
	if u == nil {
		return c.renderConsent(401, map[string]any{"Error": "Your session ended. Connect the app again from your agent."})
	}
	if why != "" {
		return c.renderConsent(403, map[string]any{"Error": why})
	}
	if c.R.PostForm.Get("decision") != "allow" {
		return c.redirectBack(a, url.Values{"error": {"access_denied"}})
	}
	code := auth.RandomToken(32)
	err = c.S.db.Write(c.Context(), func(w *store.Queries) error {
		return w.InsertOAuthCode(c.Context(), store.OAuthCode{CodeHash: auth.SHA256Hex(code), ClientID: a.client.ID, UserID: u.ID, RedirectURI: a.redirectURI, CodeChallenge: a.challenge, ExpiresAt: c.S.now().Add(oauthCodeTTL).UnixMilli()})
	})
	if err != nil {
		return err
	}
	return c.redirectBack(a, url.Values{"code": {code}})
}

// sameOriginForm refuses a form post another site submitted: a cross-site Sec-Fetch-Site,
// or an Origin naming another host.
func (c *Ctx) sameOriginForm() bool {
	if c.R.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := c.R.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, c.R.Host) || (c.S.host != "" && strings.EqualFold(u.Host, c.S.host))
}

// pkceOK checks code_verifier against an S256 code_challenge (RFC 7636).
func pkceOK(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

type tokenPair struct{ access, refresh string }

func (s *Server) newTokenPair() (tokenPair, string, string) {
	t := tokenPair{access: "cseed_at_" + auth.RandomToken(32), refresh: "cseed_rt_" + auth.RandomToken(32)}
	return t, auth.SHA256Hex(t.access), auth.SHA256Hex(t.refresh)
}

func (c *Ctx) writeTokens(t tokenPair) error {
	allowCORS(c.W)
	c.W.Header().Set("Cache-Control", "no-store")
	writeJSON(c.W, 200, map[string]any{
		"access_token":  t.access,
		"token_type":    "Bearer",
		"expires_in":    int(oauthAccessTTL / time.Second),
		"refresh_token": t.refresh,
		"scope":         mcpScope,
	})
	return nil
}

var errGrant = errors.New("invalid grant")

// oauthToken redeems an authorization code or rotates a refresh token.
func (c *Ctx) oauthToken() error {
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, 16<<10)
	if err := c.R.ParseForm(); err != nil {
		return c.oauthError(400, "invalid_request", "Body must be application/x-www-form-urlencoded")
	}
	f := c.R.PostForm
	ctx := c.Context()
	now := c.S.nowMs()
	tokens, accessHash, refreshHash := c.S.newTokenPair()
	accessExp, refreshExp := c.S.now().Add(oauthAccessTTL).UnixMilli(), c.S.now().Add(oauthRefreshTTL).UnixMilli()
	if !c.resourceOK(f.Get("resource")) {
		return c.oauthError(400, "invalid_target", "The resource parameter must name this dashboard's /mcp endpoint")
	}
	switch f.Get("grant_type") {
	case "authorization_code":
		// Take the code in its own transaction, so a failed attempt still burns it: a code
		// is good for exactly one try (RFC 6749 §4.1.2).
		var code, replayed *store.OAuthCode
		err := c.S.db.Write(ctx, func(w *store.Queries) error {
			var err error
			code, err = w.TakeOAuthCode(ctx, auth.SHA256Hex(f.Get("code")), now)
			if errors.Is(err, store.ErrCodeReused) {
				replayed = code
				return errGrant
			}
			if errors.Is(err, store.ErrNotFound) {
				return errGrant
			}
			return err
		})
		if replayed != nil && replayed.GrantID != nil {
			// A redeemed code presented again: whoever holds it may also hold the tokens it
			// bought, so revoke them (RFC 6749 §4.1.2).
			c.revokeReplayed(*replayed.GrantID, "agent.revoked_code_reuse", now)
		}
		if err == nil && (code.ExpiresAt <= now || code.ClientID != f.Get("client_id") || !pkceOK(f.Get("code_verifier"), code.CodeChallenge)) {
			err = errGrant
		}
		// redirect_uri must repeat the one authorized when it was sent (RFC 6749 §4.1.3).
		if ru := f.Get("redirect_uri"); err == nil && ru != "" && ru != code.RedirectURI {
			err = errGrant
		}
		if err == nil {
			err = c.S.db.Write(ctx, func(w *store.Queries) error {
				u, err := w.UserByID(ctx, code.UserID)
				if err != nil || !u.Active {
					return errGrant
				}
				// Reconnecting the same client replaces its old grant instead of piling up rows.
				if err := w.RevokeClientGrants(ctx, u.ID, code.ClientID, now); err != nil {
					return err
				}
				g := store.OAuthGrant{ID: auth.UUID(), ClientID: code.ClientID, UserID: u.ID, AccessHash: accessHash, AccessExpiresAt: accessExp, RefreshHash: refreshHash, RefreshExpiresAt: refreshExp, CreatedAt: now}
				if err := w.InsertOAuthGrant(ctx, g); err != nil {
					return err
				}
				if err := w.SetCodeGrant(ctx, code.CodeHash, g.ID); err != nil {
					return err
				}
				cl, err := w.OAuthClientByID(ctx, code.ClientID)
				if err != nil {
					return err
				}
				return w.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), ActorUserID: &u.ID, ActorAgent: &g.ID, Action: "agent.connected", EntityType: "agent", EntityID: &g.ID,
					AfterJSON: snapshot(map[string]any{"client": cl.Name}), CreatedAt: store.Ms(now)})
			})
		}
		if errors.Is(err, errGrant) {
			return c.oauthError(400, "invalid_grant", "The code is invalid, expired, already used, or the code_verifier does not match")
		}
		if err != nil {
			return err
		}
		return c.writeTokens(tokens)
	case "refresh_token":
		var reused *store.OAuthGrant
		err := c.S.db.Write(ctx, func(w *store.Queries) error {
			hash := auth.SHA256Hex(f.Get("refresh_token"))
			g, err := w.GrantByRefreshHash(ctx, hash)
			if errors.Is(err, store.ErrNotFound) {
				// A refresh token that was already rotated away is being replayed: someone
				// else holds a copy. Disconnect the agent (RFC 9700 §4.14.2), unless it was
				// rotated moments ago: that is the same client refreshing twice at once
				// (two windows sharing one sign-in), which only loses the race.
				if old, err := w.GrantByPrevRefreshHash(ctx, hash); err == nil && old.RevokedAt == nil &&
					(old.RotatedAt == nil || now-*old.RotatedAt >= oauthRefreshRaceGrace.Milliseconds()) {
					reused = old
				}
				return errGrant
			}
			if err != nil {
				return err
			}
			// client_id is optional here (the refresh token is bound to its client), but it must match when sent.
			if cid := f.Get("client_id"); g.RevokedAt != nil || g.RefreshExpiresAt <= now || (cid != "" && cid != g.ClientID) {
				return errGrant
			}
			u, err := w.UserByID(ctx, g.UserID)
			if err != nil || !u.Active {
				return errGrant
			}
			return w.RotateGrant(ctx, g.ID, accessHash, accessExp, refreshHash, refreshExp, now)
		})
		if reused != nil {
			c.revokeReplayed(reused.ID, "agent.revoked_token_reuse", now)
		}
		if errors.Is(err, errGrant) {
			return c.oauthError(400, "invalid_grant", "The refresh token is invalid, expired or revoked; connect the app again")
		}
		if err != nil {
			return err
		}
		return c.writeTokens(tokens)
	default:
		return c.oauthError(400, "unsupported_grant_type", "Use authorization_code or refresh_token")
	}
}

// revokeReplayed disconnects a grant whose code or refresh token was presented again, in its
// own transaction (the token request's rolled back), and audits it once.
func (c *Ctx) revokeReplayed(grantID, action string, now int64) {
	ctx := c.Context()
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		g, err := w.GrantByID(ctx, grantID)
		if err != nil || g.RevokedAt != nil {
			return err
		}
		if err := w.RevokeGrant(ctx, g.ID, now); err != nil {
			return err
		}
		return w.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), ActorUserID: &g.UserID, ActorAgent: &g.ID, Action: action, EntityType: "agent", EntityID: &g.ID,
			AfterJSON: snapshot(map[string]any{"client": g.ClientName, "revoked": true}), CreatedAt: store.Ms(now)})
	})
	if err != nil {
		c.S.log.Error("revoke replayed grant", "grant", grantID, "error", err)
	}
}

// formField is a hidden input on the consent page.
type formField struct{ Name, Value string }
