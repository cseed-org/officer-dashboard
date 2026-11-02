package calendar

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrGoogleRevoked means the refresh token no longer works; the officer must reconnect.
var ErrGoogleRevoked = errors.New("google calendar access was revoked; reconnect it in Settings")

// Google is the OAuth 2.0 web-server flow plus the two Calendar API calls we need.
type Google struct {
	ClientID, ClientSecret, RedirectURL   string
	AuthURL, TokenURL, APIBase, RevokeURL string // overridable in tests
	HTTP                                  *http.Client
}

// NewGoogle configures Google Calendar sync (GOOGLE_OAUTH_CLIENT_ID / _SECRET).
func NewGoogle(clientID, secret, redirect string) *Google {
	return &Google{
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirect,
		AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:  "https://oauth2.googleapis.com/token",
		APIBase:   "https://www.googleapis.com/calendar/v3",
		RevokeURL: "https://oauth2.googleapis.com/revoke",
		HTTP:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Scope asks only to manage events, plus the account email for display.
const Scope = "openid email https://www.googleapis.com/auth/calendar.events"

// ConsentURL starts the flow. access_type=offline and prompt=consent make Google return
// a refresh token every time, so reconnecting always works.
func (g *Google) ConsentURL(state, challenge string) string {
	v := url.Values{
		"client_id": {g.ClientID}, "redirect_uri": {g.RedirectURL}, "response_type": {"code"},
		"scope": {Scope}, "state": {state}, "access_type": {"offline"}, "prompt": {"consent"},
		"include_granted_scopes": {"true"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	return g.AuthURL + "?" + v.Encode()
}

// Token is a token endpoint answer.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (g *Google) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return http.DefaultClient
}

func (g *Google) token(ctx context.Context, form url.Values) (*Token, error) {
	form.Set("client_id", g.ClientID)
	form.Set("client_secret", g.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := g.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("google token: %w", err)
	}
	defer res.Body.Close()
	var t Token
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&t)
	if t.Error == "invalid_grant" {
		return nil, ErrGoogleRevoked
	}
	if res.StatusCode/100 != 2 || t.AccessToken == "" {
		return nil, fmt.Errorf("google token: HTTP %d %s", res.StatusCode, t.Error)
	}
	return &t, nil
}

// Exchange trades the callback code for tokens.
func (g *Google) Exchange(ctx context.Context, code, verifier string) (*Token, error) {
	return g.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {g.RedirectURL}, "code_verifier": {verifier}})
}

// Refresh gets a new access token.
func (g *Google) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	return g.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

// Revoke is best effort when an officer disconnects.
func (g *Google) Revoke(ctx context.Context, token string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.RevokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, err := g.client().Do(req); err == nil {
		res.Body.Close()
	}
}

// EmailFromIDToken reads the email claim. The ID token came straight from Google's token
// endpoint over TLS, so the signature check adds nothing here; it is only for display.
func EmailFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.Email
}

type gTime struct {
	DateTime string `json:"dateTime"`
}

// UpsertEvent creates the event on the primary calendar, or updates eventID when set.
// It returns the Google event id. A 404/410 on update falls back to creating it again.
func (g *Google) UpsertEvent(ctx context.Context, accessToken, eventID string, e Event) (string, error) {
	body := map[string]any{
		"summary":     e.Summary,
		"description": strings.TrimSpace(e.Description + "\n\n" + e.URL),
		"location":    e.Location,
		"start":       gTime{e.Start.Format(time.RFC3339)},
		"end":         gTime{e.End.Format(time.RFC3339)},
		"source":      map[string]string{"title": "CSEED Officer Dashboard", "url": e.URL},
	}
	if e.URL == "" {
		delete(body, "source")
	}
	if e.Cancelled {
		body["status"] = "cancelled"
	}
	method, target := http.MethodPost, g.APIBase+"/calendars/primary/events"
	if eventID != "" {
		method, target = http.MethodPatch, target+"/"+url.PathEscape(eventID)
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("google calendar: %w", err)
	}
	defer res.Body.Close()
	if eventID != "" && (res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone) && !e.Cancelled {
		return g.UpsertEvent(ctx, accessToken, "", e)
	}
	if res.StatusCode == http.StatusUnauthorized {
		return "", ErrGoogleRevoked
	}
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("google calendar: HTTP %d", res.StatusCode)
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&out)
	if out.ID == "" {
		out.ID = eventID
	}
	return out.ID, nil
}

// Sealer encrypts OAuth tokens at rest with AES-256-GCM. The key is derived from the
// OAuth client secret, so a copy of the database alone cannot use the tokens.
type Sealer struct{ aead cipher.AEAD }

func NewSealer(secret string) *Sealer {
	key := sha256.Sum256([]byte("cseed-dashboard google-calendar tokens v1\x00" + secret))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return &Sealer{aead: aead}
}

func (s *Sealer) Seal(plain string) string {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *Sealer) Open(sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", errors.New("sealed token is malformed")
	}
	n := s.aead.NonceSize()
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrGoogleRevoked
	}
	return string(plain), nil
}
