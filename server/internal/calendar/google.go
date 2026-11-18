package calendar

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// Account is who a grant belongs to, read from the ID token: Sub is Google's stable,
// never reassigned account id (always present when the openid scope is granted); Email is
// for display and may be missing. The ID token came straight from Google's token endpoint
// over TLS, so the signature check adds nothing here.
type Account struct {
	Sub, Email string
}

// AccountFromIDToken reads the sub and email claims; both are empty when the token is
// absent or malformed.
func AccountFromIDToken(idToken string) Account {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Account{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Account{}
	}
	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	_ = json.Unmarshal(raw, &claims)
	return Account{Sub: claims.Sub, Email: claims.Email}
}

type gTime struct {
	DateTime string `json:"dateTime"`
}

// EventID is the Google event id the dashboard gives an invite on one calendar: a hex
// digest of the invite UID and the destination, so it is stable across retries and
// restarts. Hex digits are valid base32hex, which events.insert accepts as a client id.
func EventID(uid, destination string) string {
	sum := sha256.Sum256([]byte(uid + "\x00" + destination))
	return hex.EncodeToString(sum[:])
}

// UpsertEvent writes the event to the primary calendar and returns its Google event id.
// eventID, when set, is an event that may already be there (stored from an earlier
// delivery, possibly one assigned by Google before V0.14.1): it is updated. Otherwise, or
// when that event is gone (404/410), the event is created with newID as its id. Creating
// is idempotent: when newID already exists (409, a repeated insert whose answer or record
// was lost) that same event is updated instead of a second one being added. A
// cancellation of an event that is gone succeeds without creating anything.
func (g *Google) UpsertEvent(ctx context.Context, accessToken, eventID, newID string, e Event) (string, error) {
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
	// An explicit status lets a PATCH restore an event that was cancelled earlier.
	body["status"] = "confirmed"
	if e.Cancelled {
		body["status"] = "cancelled"
	}
	gone := func(status int) bool { return status == http.StatusNotFound || status == http.StatusGone }
	if e.Cancelled && eventID == "" {
		eventID = newID // never create an event just to cancel it
	}
	if eventID != "" {
		status, id, err := g.writeEvent(ctx, accessToken, http.MethodPatch, eventID, body)
		switch {
		case err != nil:
			return "", err
		case status/100 == 2:
			return id, nil
		case gone(status) && e.Cancelled:
			return eventID, nil // already gone from that calendar: nothing left to cancel
		case !gone(status):
			return "", fmt.Errorf("google calendar: HTTP %d", status)
		}
	}
	if newID != "" {
		body["id"] = newID
	}
	status, id, err := g.writeEvent(ctx, accessToken, http.MethodPost, "", body)
	switch {
	case err != nil:
		return "", err
	case status/100 == 2:
		return id, nil
	case status == http.StatusConflict && newID != "" && newID != eventID:
		// The id is taken: an earlier insert of this same invite got through. Update it.
		delete(body, "id")
		status, id, err = g.writeEvent(ctx, accessToken, http.MethodPatch, newID, body)
		if err != nil {
			return "", err
		}
		if status/100 == 2 {
			return id, nil
		}
	}
	return "", fmt.Errorf("google calendar: HTTP %d", status)
}

// writeEvent sends one Calendar API write: POST to create (eventID ""), PATCH to update.
// It returns the HTTP status and, on success, the event id.
func (g *Google) writeEvent(ctx context.Context, accessToken, method, eventID string, body map[string]any) (int, string, error) {
	target := g.APIBase + "/calendars/primary/events"
	if eventID != "" {
		target += "/" + url.PathEscape(eventID)
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := g.client().Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("google calendar: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return 0, "", ErrGoogleRevoked
	}
	if res.StatusCode/100 != 2 {
		return res.StatusCode, "", nil
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&out)
	if out.ID == "" {
		out.ID = eventID
	}
	if out.ID == "" {
		if id, ok := body["id"].(string); ok {
			out.ID = id
		}
	}
	return res.StatusCode, out.ID, nil
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
