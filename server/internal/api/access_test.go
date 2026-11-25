package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type accessTransport func(*http.Request) (*http.Response, error)

func (f accessTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAccessAssertionGatesPrivateRoutesWithoutReplacingAppAuth(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAccessVerifier("test-team.cloudflareaccess.com", "expected-aud")
	if err != nil {
		t.Fatal(err)
	}
	fetches := 0
	verifier.client.Transport = accessTransport(func(r *http.Request) (*http.Response, error) {
		fetches++
		if r.URL.String() != "https://test-team.cloudflareaccess.com/cdn-cgi/access/certs" {
			t.Errorf("unexpected key endpoint")
		}
		doc := `{"keys":[{"kty":"RSA","kid":"key","alg":"RS256","n":"` + base64.RawURLEncoding.EncodeToString(key.N.Bytes()) + `","e":"AQAB"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(doc))}, nil
	})
	e := newEnv(t, func(c *Config) { c.Access = verifier })
	token := func(issuer string, aud any, exp, nbf int64, alg, kid string) string {
		enc := func(v any) string { raw, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(raw) }
		message := enc(map[string]any{"alg": alg, "kid": kid}) + "." + enc(map[string]any{"iss": issuer, "aud": aud, "exp": exp, "nbf": nbf})
		digest := sha256.Sum256([]byte(message))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return message + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	now := testNow.Unix()
	issuer := "https://test-team.cloudflareaccess.com"
	valid := token(issuer, []string{"expected-aud"}, now+3600, 0, "RS256", "key")
	for name, assertion := range map[string]string{
		"missing": "", "unsigned": "e30.e30.", "wrong issuer": token("https://evil.example", []string{"expected-aud"}, now+3600, 0, "RS256", "key"),
		"wrong aud": token(issuer, []string{"other"}, now+3600, 0, "RS256", "key"),
		"expired":   token(issuer, []string{"expected-aud"}, now, 0, "RS256", "key"),
		"future":    token(issuer, []string{"expected-aud"}, now+3600, now+60, "RS256", "key"),
		"wrong alg": token(issuer, []string{"expected-aud"}, now+3600, 0, "HS256", "key"),
		"wrong key": token(issuer, []string{"expected-aud"}, now+3600, 0, "RS256", "unknown"),
		"tampered":  valid[:len(valid)-4] + "AAAA",
	} {
		t.Run(name, func(t *testing.T) {
			who := e.officer()
			who.headers = map[string]string{"Cf-Access-Jwt-Assertion": assertion}
			expectStatus(t, name, e.get("/api/me", who), 401)
		})
	}
	who := e.officer()
	who.headers = map[string]string{"Cf-Access-Jwt-Assertion": valid}
	expectStatus(t, "valid edge and session", e.get("/api/me", who), 200)
	expectStatus(t, "edge is not a session", e.get("/api/me", actor{headers: who.headers}), 401)
	for _, path := range []string{"/mcp", "/oauth/register", "/oauth/token", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp", "/api/health"} {
		r := e.get(path, anon)
		if strings.Contains(string(r.Raw), "Cloudflare Access authentication required") {
			t.Errorf("agent/probe path gated: %s", path)
		}
	}
	for _, path := range []string{"/oauth/authorize", "/api/portal/invite", "/oauth/register/extra", "/mcp/extra", "/.well-known/oauth-protected-resource/extra"} {
		expectStatus(t, "protected path", e.get(path, anon), 401)
	}
	if fetches != 1 {
		t.Errorf("JWKS cache/flood limit: %d fetches", fetches)
	}
	e.clock = e.clock.Add(2 * time.Hour)
	verifier.client.Transport = accessTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	who.headers["Cf-Access-Jwt-Assertion"] = token(issuer, "expected-aud", e.clock.Unix()+3600, 0, "RS256", "key")
	expectStatus(t, "expired cache fails closed", e.get("/api/me", who), 401)
}

func TestAccessConfigRejectsPartialAndUntrustedKeyOrigins(t *testing.T) {
	for _, pair := range [][2]string{{"", "aud"}, {"team.cloudflareaccess.com", ""}, {"https://team.cloudflareaccess.com", "aud"}, {"evil.example", "aud"}, {"team.cloudflareaccess.com.evil.example", "aud"}} {
		if _, err := NewAccessVerifier(pair[0], pair[1]); err == nil {
			t.Fatal("invalid Access config accepted")
		}
	}
	if v, err := NewAccessVerifier("", ""); v != nil || err != nil {
		t.Fatal("opt out failed")
	}
}

func TestOrgCalendarRejectsUnrankedSessionRole(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	e.exec(`PRAGMA ignore_check_constraints=ON`)
	e.exec(`UPDATE users SET role='observer' WHERE id='user-priya'`)
	expectStatus(t, "unranked role", e.get("/api/orgcal/calendars", who), 403)
}
