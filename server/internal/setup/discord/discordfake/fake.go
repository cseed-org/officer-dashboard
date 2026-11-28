// Package discordfake is a stateful fake of the few Discord endpoints `dashboard setup
// discord` calls, for tests. Shapes follow docs.discord.com (cited beside each handler);
// where Discord does not document something (token-endpoint error bodies, combining
// identify with webhook.incoming), the fake can answer each plausible way.
package discordfake

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

// Server is the fake. Set its fields before the first request.
type Server struct {
	*httptest.Server

	ClientID, ClientSecret string
	// BotToken is accepted by GET /applications/@me, which returns BotAppID (ClientID when
	// empty) and RedirectURIs, or no redirect_uris at all when OmitRedirectURIs is set.
	BotToken         string
	BotAppID         string
	RedirectURIs     []string
	OmitRedirectURIs bool
	// The account that approves consents.
	UserID, Username, GlobalName string
	// CombinedWebhook: an authorization for identify and webhook.incoming together returns
	// the webhook. When false, it returns only identify (the undocumented case setup must
	// cope with). webhook.incoming alone always returns one.
	CombinedWebhook bool
	// RateLimit answers this many next requests with 429 and RetryAfter seconds.
	RateLimit  int
	RetryAfter float64

	mu       sync.Mutex
	codes    map[string]*grant
	tokens   map[string][]string // access token → scopes
	webhooks []Webhook
	requests []string
}

type grant struct {
	scopes    []string
	challenge string
	redirect  string
	used      bool
}

// Webhook is what the fake handed out.
type Webhook struct{ ID, Token, URL, ChannelID, GuildID string }

// New starts a fake with fresh random credentials. Values are made at run time so no
// secret-looking literal is in the repository.
func New() *Server {
	s := &Server{
		ClientID:     "123456789012345678",
		ClientSecret: random(16),
		BotToken:     "MTIz." + random(4) + "." + random(20),
		UserID:       "100000000000000042",
		Username:     "admin",
		GlobalName:   "Club Admin",
		codes:        map[string]*grant{},
		tokens:       map[string][]string{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Requests lists "METHOD /path" for every request received.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// Webhooks lists the webhooks handed out, oldest first.
func (s *Server) Webhooks() []Webhook {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Webhook(nil), s.webhooks...)
}

// Codes lists every code handed out; AccessTokens every access token. Tests check that
// none of them was printed.
func (s *Server) Codes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for c := range s.codes {
		out = append(out, c)
	}
	return out
}

func (s *Server) AccessTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for t := range s.tokens {
		out = append(out, t)
	}
	return out
}

// Burn marks a code as used, as if it had been exchanged already.
func (s *Server) Burn(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.codes[code]; g != nil {
		g.used = true
	}
}

// Approve plays the admin approving an authorization link in a browser: it checks the
// link the way Discord would and returns the code Discord puts in the redirect.
func (s *Server) Approve(link string) (code, state string, err error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", "", err
	}
	q := u.Query()
	switch {
	case u.Scheme+"://"+u.Host+u.Path != "https://discord.com/oauth2/authorize":
		return "", "", fmt.Errorf("not an authorization URL: %s", u.Path)
	case q.Get("response_type") != "code":
		return "", "", fmt.Errorf("response_type %q", q.Get("response_type"))
	case q.Get("client_id") != s.ClientID:
		return "", "", fmt.Errorf("client_id %q", q.Get("client_id"))
	case q.Get("code_challenge") != "" && q.Get("code_challenge_method") != "S256":
		return "", "", fmt.Errorf("code_challenge_method %q", q.Get("code_challenge_method"))
	}
	if !strings.Contains(u.RawQuery, "scope=") || strings.Contains(u.RawQuery, "scope=identify+") {
		// docs.discord.com/developers/topics/oauth2: "scope is a list of OAuth2 scopes
		// separated by url encoded spaces (%20)".
		return "", "", fmt.Errorf("scopes not separated by %%20: %s", u.RawQuery)
	}
	code = random(15)[:30]
	s.mu.Lock()
	s.codes[code] = &grant{scopes: strings.Fields(q.Get("scope")), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	s.mu.Unlock()
	return code, q.Get("state"), nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	limited := s.RateLimit > 0
	if limited {
		s.RateLimit--
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if limited {
		// docs.discord.com/developers/topics/rate-limits, "Exceeding A Rate Limit".
		w.Header().Set("Retry-After", fmt.Sprint(int(s.RetryAfter+0.999)))
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"message": "You are being rate limited.", "retry_after": %g, "global": false}`, s.RetryAfter)
		return
	}
	if r.Header.Get("User-Agent") == "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "DiscordBot (") {
		// docs.discord.com/developers/reference, "User Agent".
		http.Error(w, `{"message": "bad user agent"}`, http.StatusForbidden)
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/oauth2/token":
		s.token(w, r)
	case "GET /api/v10/users/@me":
		s.me(w, r)
	case "GET /api/v10/applications/@me":
		s.application(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message": "404: Not Found", "code": 0}`)
	}
}

// oauthError is the RFC 6749 §5.2 shape. Discord does not document its token-endpoint
// error bodies; this is the shape OAuth2 servers use.
func oauthError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error": %q, "error_description": "fake says %s"}`, code, code)
}

// token is POST /oauth2/token (docs.discord.com/developers/topics/oauth2, "Authorization
// Code Grant" and "Client Credentials Grant").
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		oauthError(w, 400, "invalid_request")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != s.ClientID || secret != s.ClientSecret {
		oauthError(w, 401, "invalid_client")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "client_credentials":
		tok := random(16)
		s.mu.Lock()
		s.tokens[tok] = strings.Fields(r.PostForm.Get("scope"))
		s.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": 604800, "scope": r.PostForm.Get("scope")})
	case "authorization_code":
		s.mu.Lock()
		defer s.mu.Unlock()
		g := s.codes[r.PostForm.Get("code")]
		if g == nil || g.used || g.redirect != r.PostForm.Get("redirect_uri") {
			oauthError(w, 400, "invalid_grant")
			return
		}
		if g.challenge != "" {
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
				oauthError(w, 400, "invalid_grant")
				return
			}
		}
		g.used = true
		scopes := g.scopes
		combined := has(scopes, "identify") && has(scopes, "webhook.incoming")
		if combined && !s.CombinedWebhook {
			scopes = []string{"identify"}
		}
		tok := random(16)
		s.tokens[tok] = scopes
		body := map[string]any{"token_type": "Bearer", "access_token": tok, "scope": strings.Join(scopes, " "), "expires_in": 604800, "refresh_token": random(16)}
		if has(scopes, "webhook.incoming") {
			hook := Webhook{ID: "200000000000000077", Token: random(30), ChannelID: "300000000000000011", GuildID: "400000000000000022"}
			hook.URL = "https://discord.com/api/webhooks/" + hook.ID + "/" + hook.Token
			s.webhooks = append(s.webhooks, hook)
			body["webhook"] = map[string]any{"application_id": s.ClientID, "name": "dashboard", "url": hook.URL, "channel_id": hook.ChannelID,
				"token": hook.Token, "type": 1, "avatar": nil, "guild_id": hook.GuildID, "id": hook.ID}
		}
		json.NewEncoder(w).Encode(body)
	default:
		oauthError(w, 400, "unsupported_grant_type")
	}
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// me is GET /users/@me (docs.discord.com/developers/resources/user, "Get Current User":
// "requires the identify scope").
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	scopes, known := s.tokens[tok]
	s.mu.Unlock()
	if !ok || !known || !has(scopes, "identify") {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message": "401: Unauthorized", "code": 0}`)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"id": s.UserID, "username": s.Username, "global_name": s.GlobalName, "avatar": nil})
}

// application is GET /applications/@me (docs.discord.com/developers/resources/application,
// "Get Current Application"; redirect_uris is optional in the object).
func (s *Server) application(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bot "+s.BotToken {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message": "401: Unauthorized", "code": 0}`)
		return
	}
	id := s.BotAppID
	if id == "" {
		id = s.ClientID
	}
	body := map[string]any{"id": id, "name": "Club dashboard", "bot_public": false, "bot": map[string]any{"id": id, "username": "dashboard"}}
	if !s.OmitRedirectURIs {
		uris := s.RedirectURIs
		if uris == nil {
			uris = []string{}
		}
		body["redirect_uris"] = uris
	}
	json.NewEncoder(w).Encode(body)
}
