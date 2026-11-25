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
	"net/http/httptest"
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
	verifier, err := NewAccessVerifier("test-team.cloudflareaccess.com", "expected-aud", "service-aud")
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
	serviceAssertion := token(issuer, []string{"service-aud"}, now+3600, 0, "RS256", "key")
	// Exercise the real middleware separately from application routing: unknown
	// descendants and the application roots still require the correct Access app.
	gated := e.srv.withAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, path := range []string{"/api/me", "/oauth/authorize", "/api/agentish/jobs", "/api/ingestion", "/api/agent", "/api/agent/jobs", "/api/ingest", "/api/ingest/buildspace/checkins"} {
		servicePath := path == "/api/agent" || strings.HasPrefix(path, "/api/agent/") || path == "/api/ingest" || strings.HasPrefix(path, "/api/ingest/")
		for name, assertion := range map[string]string{"browser": valid, "service": serviceAssertion, "missing": ""} {
			t.Run(path+"/"+name, func(t *testing.T) {
				r := httptest.NewRequest("GET", path, nil)
				r.Header.Set("Cf-Access-Jwt-Assertion", assertion)
				w := httptest.NewRecorder()
				gated.ServeHTTP(w, r)
				want := 401
				if (servicePath && name == "service") || (!servicePath && name == "browser") {
					want = 204
				}
				if w.Code != want {
					t.Fatalf("status = %d, want %d", w.Code, want)
				}
			})
		}
	}
	// A valid service assertion still needs the dashboard's own service token.
	expectStatus(t, "edge is not a service token", e.get("/api/agent/jobs", actor{headers: map[string]string{"Cf-Access-Jwt-Assertion": serviceAssertion}}), 401)
	admin := e.director()
	admin.headers = who.headers
	plaintext := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "access-test", "scopes": []string{"agent:jobs"}}, admin)).str("plaintext")
	service := bearer(plaintext)
	service.headers = map[string]string{"Cf-Access-Jwt-Assertion": serviceAssertion}
	expectStatus(t, "both service credentials", e.get("/api/agent/jobs", service), 200)
	service.headers["Cf-Access-Jwt-Assertion"] = valid
	expectStatus(t, "browser assertion cannot authorize service route", e.get("/api/agent/jobs", service), 401)
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
	for _, config := range [][3]string{{"", "aud", "service"}, {"team.cloudflareaccess.com", "", "service"}, {"team.cloudflareaccess.com", "aud", ""}, {"", "", "service"}, {"https://team.cloudflareaccess.com", "aud", "service"}, {"evil.example", "aud", "service"}, {"team.cloudflareaccess.com.evil.example", "aud", "service"}} {
		if _, err := NewAccessVerifier(config[0], config[1], config[2]); err == nil {
			t.Fatal("invalid Access config accepted")
		}
	}
	if v, err := NewAccessVerifier("", "", ""); v != nil || err != nil {
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
