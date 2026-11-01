package auth

import (
	"context"
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

// Discord is an OAuth2 authorization-code client with PKCE (S256) for the `identify` scope.
type Discord struct {
	ClientID, ClientSecret, RedirectURI string
	// Endpoints are overridable for tests.
	AuthorizeURL string
	TokenURL     string
	ProfileURL   string
	HTTP         *http.Client
}

// NewDiscord builds a client with Discord's production endpoints.
func NewDiscord(clientID, secret, redirect string) *Discord {
	return &Discord{
		ClientID: clientID, ClientSecret: secret, RedirectURI: redirect,
		AuthorizeURL: "https://discord.com/oauth2/authorize",
		TokenURL:     "https://discord.com/api/oauth2/token",
		ProfileURL:   "https://discord.com/api/v10/users/@me",
		HTTP:         &http.Client{Timeout: 10 * time.Second},
	}
}

// Challenge is the PKCE S256 challenge for a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthURL is where to send the browser to sign in.
func (d *Discord) AuthURL(state, verifier string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {d.ClientID},
		"state":                 {state},
		"scope":                 {"identify"},
		"redirect_uri":          {d.RedirectURI},
		"code_challenge_method": {"S256"},
		"code_challenge":        {Challenge(verifier)},
	}
	return d.AuthorizeURL + "?" + q.Encode()
}

// Profile is the subset of Discord's /users/@me that the dashboard uses.
type Profile struct {
	ID         string  `json:"id"`
	Username   string  `json:"username"`
	GlobalName *string `json:"global_name"`
	Avatar     *string `json:"avatar"`
}

// DisplayName prefers the global name.
func (p Profile) DisplayName() string {
	if p.GlobalName != nil && *p.GlobalName != "" {
		return *p.GlobalName
	}
	return p.Username
}

// AvatarURL is the CDN URL of the avatar, or "" when there is none.
func (p Profile) AvatarURL() string {
	if p.Avatar == nil || *p.Avatar == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png?size=128", p.ID, *p.Avatar)
}

// Exchange trades the authorization code for the signed-in user's profile.
func (d *Discord) Exchange(ctx context.Context, code, verifier string) (Profile, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {d.RedirectURI},
		"client_id":     {d.ClientID},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(url.QueryEscape(d.ClientID), url.QueryEscape(d.ClientSecret))
	res, err := d.HTTP.Do(req)
	if err != nil {
		return Profile{}, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("discord token exchange failed: %d", res.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return Profile{}, errors.New("discord token response missing access_token")
	}
	preq, err := http.NewRequestWithContext(ctx, http.MethodGet, d.ProfileURL, nil)
	if err != nil {
		return Profile{}, err
	}
	preq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	pres, err := d.HTTP.Do(preq)
	if err != nil {
		return Profile{}, err
	}
	defer pres.Body.Close()
	if pres.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("discord /users/@me failed: %d", pres.StatusCode)
	}
	var p Profile
	if err := json.NewDecoder(io.LimitReader(pres.Body, 1<<20)).Decode(&p); err != nil || p.ID == "" {
		return Profile{}, errors.New("discord profile response invalid")
	}
	return p, nil
}
