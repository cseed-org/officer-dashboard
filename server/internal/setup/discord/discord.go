// Package discord is the Discord side of `dashboard setup discord`: it checks the pasted
// client ID, client secret and bot token against Discord, builds the one consent link that
// gives setup the first owner's Discord ID and a webhook, and trades the code that comes
// back for them.
//
// Discord has no API to create an application or to register a redirect URI, so those
// two steps stay manual; this package only reads redirect_uris (with a bot token) to say
// whether the step was done.
//
// Every value Discord treats as a credential (the client secret, the bot token, a code's
// access token, the PKCE verifier and the webhook URL) is a Secret, which never prints.
// Errors carry the HTTP status, Discord's numeric code and, for the token endpoint, the
// OAuth2 error code; never a request or response body.
package discord

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"officerdashboard/internal/auth"
)

const (
	// docs.discord.com/developers/reference, "Base URL": https://discord.com/api. Calls use
	// API v10, the newest listed in "API Versions".
	APIBase = "https://discord.com/api"
	// docs.discord.com/developers/topics/oauth2, "OAuth2 URLs": "Base authorization URL".
	AuthorizeURL = "https://discord.com/oauth2/authorize"
	// BotPermissions is the bot's permission integer for the invite link: none. The
	// dashboard's only bot calls (internal/notify/dm.go) open a DM channel (POST
	// /users/@me/channels) and post in it (POST /channels/{id}/messages). A DM needs no
	// server permission; it needs the bot and the recipient to share a server, which the
	// invite gives.
	BotPermissions = 0
)

// Secret is a credential. It formats as [redacted] with every fmt verb; Reveal gives the
// value to the one place that must send or store it.
type Secret string

func (Secret) String() string               { return "[redacted]" }
func (Secret) GoString() string             { return "[redacted]" }
func (s Secret) Reveal() string             { return string(s) }
func (Secret) MarshalText() ([]byte, error) { return []byte("[redacted]"), nil }

// Client calls Discord. The zero value is not usable; start from New.
type Client struct {
	BaseURL   string // APIBase; a test points it at a fake
	HTTP      *http.Client
	UserAgent string
	// Sleep waits out a 429 (nil: a timer). MaxWait is the longest retry_after the client
	// sits through; a longer one is returned as a rate-limit error saying how long.
	Sleep   func(context.Context, time.Duration) error
	MaxWait time.Duration
}

// New returns a client for the real Discord API.
func New() *Client {
	return &Client{
		BaseURL: APIBase,
		// No redirects: a 307 or 308 from the token endpoint would make Go send the form
		// (code, code_verifier) to wherever it points. Discord does not redirect these
		// calls, so a 3xx is an error (review of #128, F-5).
		HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		// docs.discord.com/developers/reference, "User Agent": "DiscordBot ($url,
		// $versionNumber)"; requests without one "may be blocked".
		UserAgent: "DiscordBot (https://github.com/cseed-org/officer-dashboard, setup)",
		MaxWait:   30 * time.Second,
	}
}

// Error is a refusal or failure from Discord. It never holds a body or a credential.
type Error struct {
	Op         string // what setup was doing, such as "checking the client ID and secret"
	Status     int    // HTTP status, 0 when Discord could not be reached
	OAuth      string // the token endpoint's OAuth2 error code (RFC 6749 §5.2), if any
	Code       int    // Discord's JSON error code, if any
	RetryAfter time.Duration
	Err        error // a transport error (no body, no credential)
}

func (e *Error) Error() string {
	switch {
	case e.Status == 0:
		return fmt.Sprintf("%s: could not reach Discord: %v", e.Op, e.Err)
	case e.Status == http.StatusTooManyRequests:
		return fmt.Sprintf("%s: Discord is rate-limiting this host; wait %d seconds and run setup again", e.Op, int(e.RetryAfter.Seconds()+0.999))
	}
	s := fmt.Sprintf("%s: Discord answered HTTP %d", e.Op, e.Status)
	if e.OAuth != "" {
		s += " (" + e.OAuth + ")"
	}
	if e.Code != 0 {
		s += fmt.Sprintf(" (code %d)", e.Code)
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// IsRejected: Discord refused the credential (HTTP 401, or invalid_client).
func IsRejected(err error) bool {
	e := asError(err)
	return e != nil && (e.Status == http.StatusUnauthorized || e.OAuth == "invalid_client")
}

// IsInvalidGrant: the code is used, expired, or belongs to another link or verifier.
func IsInvalidGrant(err error) bool {
	e := asError(err)
	return e != nil && e.OAuth == "invalid_grant"
}

// IsRateLimited: Discord answered 429 and the client did not wait it out.
func IsRateLimited(err error) bool {
	e := asError(err)
	return e != nil && e.Status == http.StatusTooManyRequests
}

var oauthCode = regexp.MustCompile(`^[a-z_]{1,40}$`)

// do sends the request build makes, waiting out up to two 429s, and decodes a 2xx JSON
// body into out.
func (c *Client) do(ctx context.Context, op string, build func() (*http.Request, error), out any) error {
	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return &Error{Op: op, Err: errors.New("bad request")}
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json")
		res, err := c.HTTP.Do(req.WithContext(ctx))
		if err != nil {
			return &Error{Op: op, Err: transportError(err)}
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if res.StatusCode/100 == 2 {
			if err := json.Unmarshal(body, out); err != nil {
				return &Error{Op: op, Status: res.StatusCode, Err: errors.New("the answer is not the JSON Discord documents")}
			}
			return nil
		}
		e := &Error{Op: op, Status: res.StatusCode}
		var shape struct {
			Error      string  `json:"error"`
			Code       int     `json:"code"`
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(body, &shape)
		if oauthCode.MatchString(shape.Error) {
			e.OAuth = shape.Error
		}
		e.Code = shape.Code
		if res.StatusCode != http.StatusTooManyRequests {
			return e
		}
		// docs.discord.com/developers/topics/rate-limits: "rely on the Retry-After header or
		// retry_after field"; retry_after is a float number of seconds.
		e.RetryAfter = time.Duration(shape.RetryAfter * float64(time.Second))
		if e.RetryAfter <= 0 {
			if s, err := strconv.ParseFloat(res.Header.Get("Retry-After"), 64); err == nil {
				e.RetryAfter = time.Duration(s * float64(time.Second))
			}
		}
		if attempt >= 2 || e.RetryAfter <= 0 || e.RetryAfter > c.MaxWait {
			return e
		}
		if err := c.sleep(ctx, e.RetryAfter); err != nil {
			return &Error{Op: op, Err: err}
		}
	}
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// transportError keeps the kind of a network failure without its URL (which is not secret,
// but the error text of a failed request can carry the request).
func transportError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func (c *Client) tokenRequest(clientID string, secret Secret, form url.Values) func() (*http.Request, error) {
	return func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/oauth2/token", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		// docs.discord.com/developers/topics/oauth2, "OAuth2 URLs": the token URL "will only
		// accept a content type of application/x-www-form-urlencoded"; clients authenticate
		// with HTTP Basic (RFC 6749 §2.3.1 form-encodes both parts first).
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret.Reveal()))
		return req, nil
	}
}

// CheckClient proves the client ID and secret belong together: a client credentials
// grant (docs.discord.com/developers/topics/oauth2, "Client Credentials Grant", POST
// /oauth2/token with grant_type=client_credentials, Basic auth) succeeds only with both.
// It asks for identify, which team applications may also use ("Team applications are
// limited to the identify and applications.commands.update scope"). The token it returns
// is dropped unused.
func (c *Client) CheckClient(ctx context.Context, clientID string, secret Secret) error {
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"identify"}}
	if err := c.do(ctx, "checking the client ID and secret", c.tokenRequest(clientID, secret, form), &tok); err != nil {
		return err
	}
	if tok.AccessToken == "" {
		return &Error{Op: "checking the client ID and secret", Status: 200, Err: errors.New("no access_token in the answer")}
	}
	return nil
}

// Application is the part of Discord's application object setup reads.
type Application struct {
	ID   string
	Name string
	// RedirectURIs is the app's registered redirects; RedirectURIsKnown is false when the
	// answer had no redirect_uris field (it is optional in the object).
	RedirectURIs      []string
	RedirectURIsKnown bool
}

// Application reads the application a bot token belongs to: GET /applications/@me with
// "Authorization: Bot <token>" (docs.discord.com/developers/resources/application, "Get
// Current Application": "Returns the application object associated with the requesting
// bot user"; reference, "Authentication").
func (c *Client) Application(ctx context.Context, botToken Secret) (*Application, error) {
	var app struct {
		ID           string    `json:"id"`
		Name         string    `json:"name"`
		RedirectURIs *[]string `json:"redirect_uris"`
	}
	build := func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/v10/applications/@me", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bot "+botToken.Reveal())
		}
		return req, err
	}
	if err := c.do(ctx, "checking the bot token", build, &app); err != nil {
		return nil, err
	}
	out := &Application{ID: app.ID, Name: app.Name}
	if app.RedirectURIs != nil {
		out.RedirectURIs, out.RedirectURIsKnown = *app.RedirectURIs, true
	}
	return out, nil
}

// Token is the answer to a code exchange.
type Token struct {
	AccessToken Secret
	Scopes      []string
	Webhook     *Webhook // set when the authorization included webhook.incoming
}

// Webhook is the webhook a webhook.incoming authorization created. Only URL is secret.
type Webhook struct {
	URL                          Secret
	ID, Name, ChannelID, GuildID string
}

var webhookURL = regexp.MustCompile(`^https://(discord\.com|discordapp\.com|canary\.discord\.com|ptb\.discord\.com)/api/(v[0-9]+/)?webhooks/[0-9]{17,20}/[A-Za-z0-9_-]{20,200}$`)

// ExchangeCode trades a code from a consent link for a token: POST /oauth2/token with
// grant_type=authorization_code, code, redirect_uri and the PKCE code_verifier
// (docs.discord.com/developers/topics/oauth2, "Authorization Code Grant"; PKCE: "an S256
// PKCE code_verifier", "OAuth2 URLs"). With webhook.incoming the answer carries a webhook
// object ("Webhooks": "Webhook Token Response Example").
func (c *Client) ExchangeCode(ctx context.Context, clientID string, secret Secret, code string, verifier Secret, redirectURI string) (*Token, error) {
	const op = "exchanging the setup code"
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier.Reveal()},
	}
	var raw struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		Webhook     *struct {
			ID        string `json:"id"`
			Token     string `json:"token"`
			URL       string `json:"url"`
			Name      string `json:"name"`
			ChannelID string `json:"channel_id"`
			GuildID   string `json:"guild_id"`
		} `json:"webhook"`
	}
	if err := c.do(ctx, op, c.tokenRequest(clientID, secret, form), &raw); err != nil {
		return nil, err
	}
	if raw.AccessToken == "" {
		return nil, &Error{Op: op, Status: 200, Err: errors.New("no access_token in the answer")}
	}
	tok := &Token{AccessToken: Secret(raw.AccessToken), Scopes: strings.Fields(raw.Scope)}
	if w := raw.Webhook; w != nil {
		u := w.URL
		if u == "" && w.ID != "" && w.Token != "" {
			u = "https://discord.com/api/webhooks/" + w.ID + "/" + w.Token
		}
		if !webhookURL.MatchString(u) {
			return nil, &Error{Op: op, Status: 200, Err: errors.New("the webhook in the answer has no usable URL")}
		}
		tok.Webhook = &Webhook{URL: Secret(u), ID: w.ID, Name: w.Name, ChannelID: w.ChannelID, GuildID: w.GuildID}
	}
	return tok, nil
}

// User is the part of Discord's user object setup reads.
type User struct {
	ID, Username, GlobalName string
}

// DisplayName prefers the global name, as the dashboard does.
func (u User) DisplayName() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

var snowflake = regexp.MustCompile(`^[0-9]{17,20}$`)

// CurrentUser reads who approved a consent: GET /users/@me with the access token
// (docs.discord.com/developers/resources/user, "Get Current User": "requires the identify
// scope").
func (c *Client) CurrentUser(ctx context.Context, accessToken Secret) (*User, error) {
	const op = "reading your Discord user"
	var u struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		GlobalName *string `json:"global_name"`
	}
	build := func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/v10/users/@me", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+accessToken.Reveal())
		}
		return req, err
	}
	if err := c.do(ctx, op, build, &u); err != nil {
		return nil, err
	}
	if !snowflake.MatchString(u.ID) {
		return nil, &Error{Op: op, Status: 200, Err: errors.New("the answer has no Discord user ID")}
	}
	out := &User{ID: u.ID, Username: u.Username}
	if u.GlobalName != nil {
		out.GlobalName = *u.GlobalName
	}
	return out, nil
}

// Consent is one authorization link and what the terminal keeps to use its code.
type Consent struct {
	State    string
	Scopes   []string
	URL      string
	Expiry   time.Time // the dashboard shows the code only before this
	verifier Secret
}

// Verifier is the PKCE code_verifier, which only this process knows.
func (c *Consent) Verifier() Secret { return c.verifier }

// NewConsent builds a link asking for scopes, back to redirectURI, with a state minted by
// auth.MintSetupState under the client secret (the dashboard shows the code only for a
// state it verifies with the same secret, for auth.SetupStateTTL) and a fresh PKCE S256
// verifier (32 random bytes: 43 characters, RFC 7636 §4.1). random is nil for
// crypto/rand.
func NewConsent(random io.Reader, now time.Time, clientID string, secret Secret, redirectURI string, scopes []string) (*Consent, error) {
	if random == nil {
		random = rand.Reader
	}
	state, err := auth.MintSetupState(secret.Reveal(), now, random)
	if err != nil {
		return nil, err
	}
	expiry, _ := auth.SetupStateExpiry(state)
	b := make([]byte, 32)
	if _, err := io.ReadFull(random, b); err != nil {
		return nil, errors.New("no random numbers for the setup link")
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(v))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"scope":                 {strings.Join(scopes, " ")},
		"state":                 {state},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	// "scope is a list of OAuth2 scopes separated by url encoded spaces (%20)"
	// (docs.discord.com/developers/topics/oauth2). Encode writes a space as '+' and a
	// literal '+' as %2B, so swapping '+' for %20 changes only the spaces.
	link := AuthorizeURL + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
	return &Consent{State: state, Scopes: append([]string(nil), scopes...), URL: link, Expiry: expiry, verifier: Secret(v)}, nil
}

// ParsePaste reads what the admin pasted: the relay page's "state:code", or the whole
// callback URL from the browser's address bar. Errors never quote the paste.
func ParsePaste(in string) (state, code string, err error) {
	in = strings.TrimSpace(in)
	if strings.Contains(in, "?") {
		u, perr := url.Parse(in)
		if perr != nil {
			return "", "", errors.New("that is not the setup code or the page's address")
		}
		q := u.Query()
		state, code = q.Get("state"), q.Get("code")
	} else {
		i := strings.LastIndex(in, ":")
		if i < 0 {
			return "", "", errors.New("that is not the whole setup code: copy the full line the page shows, state and code together")
		}
		state, code = in[:i], in[i+1:]
	}
	if _, ok := auth.SetupStateExpiry(state); !ok || !auth.ValidSetupCode(code) {
		return "", "", errors.New("that is not a setup code: copy the full line the page shows")
	}
	return state, code, nil
}

// InviteURL is the link that adds the app's bot to a server (docs.discord.com/developers/
// topics/oauth2, "Bot Authorization Flow": client_id, scope=bot, permissions).
func InviteURL(clientID string) string {
	q := url.Values{"client_id": {clientID}, "scope": {"bot"}, "permissions": {strconv.Itoa(BotPermissions)}}
	return AuthorizeURL + "?" + q.Encode()
}
