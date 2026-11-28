package discord_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/setup/discord"
	"officerdashboard/internal/setup/discord/discordfake"
)

const redirect = "https://officers.example.org/auth/callback"

func client(t *testing.T, f *discordfake.Server) (*discord.Client, *[]time.Duration) {
	var waits []time.Duration
	c := discord.New()
	c.BaseURL = f.URL + "/api"
	c.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	return c, &waits
}

func fake(t *testing.T) *discordfake.Server {
	f := discordfake.New()
	t.Cleanup(f.Close)
	return f
}

func TestCheckClientAcceptsTheRightSecretOnly(t *testing.T) {
	f := fake(t)
	c, _ := client(t, f)
	ctx := context.Background()
	if err := c.CheckClient(ctx, f.ClientID, discord.Secret(f.ClientSecret)); err != nil {
		t.Fatalf("right secret: %v", err)
	}
	wrong := f.ClientSecret + "x"
	err := c.CheckClient(ctx, f.ClientID, discord.Secret(wrong))
	if !discord.IsRejected(err) {
		t.Fatalf("wrong secret: %v", err)
	}
	if strings.Contains(err.Error(), wrong) || strings.Contains(fmt.Sprintf("%+v %#v", err, err), wrong) {
		t.Error("the error shows the secret")
	}
	if got := f.Requests(); len(got) != 2 || got[0] != "POST /api/oauth2/token" {
		t.Errorf("requests %q", got)
	}
}

func TestApplicationChecksTheBotTokenAndReadsRedirects(t *testing.T) {
	f := fake(t)
	f.RedirectURIs = []string{redirect}
	c, _ := client(t, f)
	ctx := context.Background()
	app, err := c.Application(ctx, discord.Secret(f.BotToken))
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != f.ClientID || !app.RedirectURIsKnown || len(app.RedirectURIs) != 1 || app.RedirectURIs[0] != redirect {
		t.Errorf("app %+v", app)
	}
	f.OmitRedirectURIs = true
	if app, err = c.Application(ctx, discord.Secret(f.BotToken)); err != nil || app.RedirectURIsKnown {
		t.Errorf("without redirect_uris: %+v %v", app, err)
	}
	bad := f.BotToken + "x"
	if _, err := c.Application(ctx, discord.Secret(bad)); !discord.IsRejected(err) || strings.Contains(err.Error(), bad) {
		t.Errorf("wrong token: %v", err)
	}
}

func TestRateLimitIsWaitedOutOrReported(t *testing.T) {
	f := fake(t)
	c, waits := client(t, f)
	f.RateLimit, f.RetryAfter = 2, 1.5
	if err := c.CheckClient(context.Background(), f.ClientID, discord.Secret(f.ClientSecret)); err != nil {
		t.Fatalf("after two 429s: %v", err)
	}
	if len(*waits) != 2 || (*waits)[0] != 1500*time.Millisecond {
		t.Errorf("waited %v, want retry_after (1.5s) twice", *waits)
	}
	// A wait longer than the client will sit through is reported, with the wait.
	f.RateLimit, f.RetryAfter = 1, 600
	err := c.CheckClient(context.Background(), f.ClientID, discord.Secret(f.ClientSecret))
	if !discord.IsRateLimited(err) || !strings.Contains(err.Error(), "600") {
		t.Errorf("long 429: %v", err)
	}
	// So is a 429 that does not go away.
	f.RateLimit, f.RetryAfter = 10, 1
	if err := c.CheckClient(context.Background(), f.ClientID, discord.Secret(f.ClientSecret)); !discord.IsRateLimited(err) {
		t.Errorf("persistent 429: %v", err)
	}
}

func TestConsentLinkStateAndPKCE(t *testing.T) {
	cons, err := discord.NewConsent(nil, "123456789012345678", redirect, []string{"identify", "webhook.incoming"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(cons.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || u.Host != "discord.com" || u.Path != "/oauth2/authorize" {
		t.Errorf("link %s", cons.URL)
	}
	if !strings.Contains(u.RawQuery, "scope=identify%20webhook.incoming") {
		t.Errorf("scopes must be joined by %%20: %s", u.RawQuery)
	}
	if q.Get("response_type") != "code" || q.Get("client_id") != "123456789012345678" || q.Get("redirect_uri") != redirect {
		t.Errorf("query %v", q)
	}
	if !strings.HasPrefix(cons.State, "setup.") || q.Get("state") != cons.State || !auth.ValidSetupRelay(cons.State, "abcdefghij0123456789") {
		t.Errorf("state %q", cons.State)
	}
	sum := sha256.Sum256([]byte(cons.Verifier().Reveal()))
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Error("code_challenge is not S256 of the verifier")
	}
	if v := cons.Verifier().Reveal(); len(v) < 43 || len(v) > 128 || strings.Contains(cons.URL, v) {
		t.Errorf("verifier length %d, or it is in the link", len(v))
	}
	other, _ := discord.NewConsent(nil, "123456789012345678", redirect, []string{"identify"})
	if other.State == cons.State || other.Verifier().Reveal() == cons.Verifier().Reveal() {
		t.Error("two consents share a state or verifier")
	}
	// A failing random source fails the consent instead of making a guessable state.
	if _, err := discord.NewConsent(bytes.NewReader(nil), "1", redirect, []string{"identify"}); err == nil {
		t.Error("no randomness, no consent")
	}
}

func TestParsePaste(t *testing.T) {
	state := "setup." + strings.Repeat("Q", 43)
	for in, want := range map[string]string{
		state + ":abcdefghij0123456789":                        "abcdefghij0123456789",
		"  " + state + ":abcdefghij0123456789\n":               "abcdefghij0123456789",
		redirect + "?code=abcdefghij0123456789&state=" + state: "abcdefghij0123456789",
	} {
		s, code, err := discord.ParsePaste(in)
		if err != nil || s != state || code != want {
			t.Errorf("%q → %q %q %v", in, s, code, err)
		}
	}
	for _, in := range []string{"", "abcdefghij0123456789", state, state + ":", state + ":a<b>cdefghij", "nostate:abcdefghij0123456789", redirect + "?code=x"} {
		if _, _, err := discord.ParsePaste(in); err == nil {
			t.Errorf("%q accepted", in)
		} else if strings.Contains(err.Error(), "abcdefghij") {
			t.Errorf("%q: the error quotes the paste: %v", in, err)
		}
	}
}

func TestExchangeBothConsentShapes(t *testing.T) {
	for _, combined := range []bool{true, false} {
		t.Run(fmt.Sprint("combined webhook ", combined), func(t *testing.T) {
			f := fake(t)
			f.CombinedWebhook = combined
			c, _ := client(t, f)
			ctx := context.Background()
			cons, _ := discord.NewConsent(nil, f.ClientID, redirect, []string{"identify", "webhook.incoming"})
			code, _, err := f.Approve(cons.URL)
			if err != nil {
				t.Fatal(err)
			}
			tok, err := c.ExchangeCode(ctx, f.ClientID, discord.Secret(f.ClientSecret), code, cons.Verifier(), redirect)
			if err != nil {
				t.Fatal(err)
			}
			user, err := c.CurrentUser(ctx, tok.AccessToken)
			if err != nil || user.ID != f.UserID || user.DisplayName() != "Club Admin" {
				t.Fatalf("user %+v %v", user, err)
			}
			if combined != (tok.Webhook != nil) {
				t.Fatalf("webhook %v", tok.Webhook)
			}
			if combined && (tok.Webhook.URL.Reveal() != f.Webhooks()[0].URL || tok.Webhook.ChannelID != f.Webhooks()[0].ChannelID) {
				t.Errorf("webhook %+v", tok.Webhook)
			}
			// The same code again is refused by Discord.
			if _, err := c.ExchangeCode(ctx, f.ClientID, discord.Secret(f.ClientSecret), code, cons.Verifier(), redirect); !discord.IsInvalidGrant(err) {
				t.Errorf("reused code: %v", err)
			}
		})
	}
}

func TestExchangeNeedsTheVerifierAndTheSecret(t *testing.T) {
	f := fake(t)
	c, _ := client(t, f)
	ctx := context.Background()
	cons, _ := discord.NewConsent(nil, f.ClientID, redirect, []string{"identify"})
	other, _ := discord.NewConsent(nil, f.ClientID, redirect, []string{"identify"})
	code, _, _ := f.Approve(cons.URL)
	if _, err := c.ExchangeCode(ctx, f.ClientID, discord.Secret("not-the-secret"), code, cons.Verifier(), redirect); !discord.IsRejected(err) {
		t.Errorf("without the secret: %v", err)
	}
	if _, err := c.ExchangeCode(ctx, f.ClientID, discord.Secret(f.ClientSecret), code, other.Verifier(), redirect); !discord.IsInvalidGrant(err) {
		t.Errorf("without the verifier: %v", err)
	}
}

func TestSecretsNeverFormat(t *testing.T) {
	s := discord.Secret("hunter2-but-longer")
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v %s %q %+v %#v", s, s, s, s, s), fmt.Sprintf("%v", struct{ S discord.Secret }{s})} {
		if strings.Contains(out, "hunter2") {
			t.Errorf("formatted: %s", out)
		}
	}
	if s.Reveal() != "hunter2-but-longer" {
		t.Error("Reveal")
	}
	var e *discord.Error
	err := error(&discord.Error{Op: "x", Status: 500})
	if !errors.As(err, &e) {
		t.Error("errors.As")
	}
}

func TestInviteURL(t *testing.T) {
	u, err := url.Parse(discord.InviteURL("123456789012345678"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "discord.com" || u.Path != "/oauth2/authorize" || q.Get("client_id") != "123456789012345678" || q.Get("scope") != "bot" || q.Get("permissions") != "0" {
		t.Errorf("invite %s", u)
	}
}
