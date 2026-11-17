package api

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// The agent endpoints sit behind Cloudflare's edge in production: none of their answers may
// be kept by a shared cache, whatever the status.
func TestAgentEndpointsAreNoStore(t *testing.T) {
	e := newEnv(t)
	_, tokens := connectAgent(t, e, "user-priya", "Claude Code")
	token := tokens.str("access_token")
	ping := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}
	badProto := actor{token: token, headers: map[string]string{"Mcp-Protocol-Version": "1999-01-01"}}
	type result struct {
		name string
		r    reply
		want int
	}
	// One request at a time, in this order: the refresh at the end rotates the access token.
	var cases []result
	send := func(name string, want int, r func() reply) { cases = append(cases, result{name, r(), want}) }
	send("protected resource", 200, func() reply { return e.get("/.well-known/oauth-protected-resource", anon) })
	send("protected resource /mcp", 200, func() reply { return e.get("/.well-known/oauth-protected-resource/mcp", anon) })
	send("authorization server", 200, func() reply { return e.get("/.well-known/oauth-authorization-server", anon) })
	send("register", 201, func() reply {
		return e.do("POST", "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": []string{testRedirect}}, loopback(anon))
	})
	send("register refused", 400, func() reply {
		return e.do("POST", "/oauth/register", map[string]any{"redirect_uris": []string{"javascript:x"}}, loopback(anon))
	})
	send("token error", 400, func() reply {
		return e.do("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {"nope"}}, loopback(anon))
	})
	send("mcp without token", 401, func() reply { return e.do("POST", "/mcp", ping, anon) })
	send("mcp bad protocol", 400, func() reply { return e.do("POST", "/mcp", ping, badProto) })
	send("mcp parse error", 400, func() reply { return e.do("POST", "/mcp", "{not json", bearer(token)) })
	send("mcp ping", 200, func() reply { return e.do("POST", "/mcp", ping, bearer(token)) })
	send("mcp no stream", 405, func() reply { return e.get("/mcp", anon) })
	send("mcp preflight", 204, func() reply { return e.do("OPTIONS", "/mcp", nil, anon) })
	send("token preflight", 204, func() reply { return e.do("OPTIONS", "/oauth/token", nil, anon) })
	send("token refresh", 200, func() reply {
		return e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.str("refresh_token")}}, loopback(anon))
	})
	for _, c := range cases {
		if c.r.Status != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, c.r.Status, c.want, c.r.Raw)
		}
		if got := c.r.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control %q, want no-store", c.name, got)
		}
	}
}

// Behind a tunnel the Host header is whatever hostname the tunnel routes; the challenge and
// the discovery documents name APP_URL regardless.
func TestChallengeAndDiscoveryUseAppURL(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AppURL = "https://officers.example" })
	serve := func(method, path string) *http.Response {
		req := httptest.NewRequest(method, "http://misrouted.example"+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec.Result()
	}
	res := serve("POST", "/mcp")
	if res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), `resource_metadata="https://officers.example/.well-known/oauth-protected-resource"`) {
		t.Fatalf("challenge: %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
	body, _ := io.ReadAll(serve("GET", "/.well-known/oauth-protected-resource/mcp").Body)
	if !strings.Contains(string(body), `"resource":"https://officers.example/mcp"`) || strings.Contains(string(body), "misrouted") {
		t.Fatalf("protected resource: %s", body)
	}
	body, _ = io.ReadAll(serve("GET", "/.well-known/oauth-authorization-server").Body)
	if !strings.Contains(string(body), `"issuer":"https://officers.example"`) || strings.Contains(string(body), "misrouted") {
		t.Fatalf("authorization server: %s", body)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// A tunnel hostname that APP_URL does not name is logged once, only for requests a trusted
// proxy forwarded (the container health check talks to the server directly).
func TestForwardedHostMismatchWarnsOnce(t *testing.T) {
	var logs syncBuffer
	_, subnet, _ := net.ParseCIDR("172.31.250.0/28")
	e := newEnv(t, func(c *Config) {
		c.AppURL = "https://officers.example"
		c.TrustedProxies = []*net.IPNet{subnet}
		c.Log = slog.New(slog.NewTextHandler(&logs, nil))
	})
	send := func(host, peer, xff string) {
		req := httptest.NewRequest("GET", "http://"+host+"/.well-known/oauth-authorization-server", nil)
		req.RemoteAddr = peer + ":41000"
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		e.h.ServeHTTP(httptest.NewRecorder(), req)
	}
	warned := func() int { return strings.Count(logs.String(), "names a different host than APP_URL") }

	send("127.0.0.1:8090", "127.0.0.1", "")                 // health check: direct, no proxy
	send("other.example", "172.31.250.3", "")               // from the tunnel's subnet but not forwarded
	send("other.example", "198.51.100.9", "203.0.113.7")    // an untrusted peer's header means nothing
	send("officers.example", "172.31.250.3", "203.0.113.7") // the expected case
	send("OFFICERS.example", "172.31.250.3", "203.0.113.7") // host names are case-insensitive
	if n := warned(); n != 0 {
		t.Fatalf("warned %d times before any misrouted request:\n%s", n, logs.String())
	}
	send("abc-def.trycloudflare.com", "172.31.250.3", "203.0.113.7")
	send("another.example", "172.31.250.3", "203.0.113.8")
	if n := warned(); n != 1 {
		t.Fatalf("warned %d times, want exactly once:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "host=abc-def.trycloudflare.com") || !strings.Contains(logs.String(), "app_url=https://officers.example") {
		t.Fatalf("warning does not name the host and APP_URL:\n%s", logs.String())
	}
}

// The rate limiter answers before the handlers, so its 429 on the agent endpoints carries
// the same no-store and CORS headers they do (a browser-based client can read the 429).
func TestAgentEndpointRateLimitIsNoStore(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AuthRateLimit = 1 })
	peer := actor{remote: "203.0.113.9:40000"}
	for _, p := range []string{"/oauth/token", "/oauth/register"} {
		e.do("POST", p, url.Values{"grant_type": {"authorization_code"}}, peer)
	}
	for _, p := range []string{"/oauth/token", "/oauth/register", "/oauth/authorize"} {
		r := e.do("POST", p, url.Values{"grant_type": {"authorization_code"}}, peer)
		expectStatus(t, p, r, 429)
		if got := r.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s 429: Cache-Control %q, want no-store", p, got)
		}
		cors := r.Header.Get("Access-Control-Allow-Origin")
		if want := map[bool]string{true: "*", false: ""}[p != "/oauth/authorize"]; cors != want {
			t.Errorf("%s 429: Access-Control-Allow-Origin %q, want %q", p, cors, want)
		}
	}
}

func TestSameHost(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"officers.example.org", "officers.example.org", true},
		{"Officers.Example.org:443", "officers.example.org", true},
		{"officers.example.org:8443", "officers.example.org", false},
		{"localhost:8090", "localhost:8090", true},
		{"other.example.org", "officers.example.org", false},
	} {
		if got := sameHost(c.a, c.b); got != c.want {
			t.Errorf("sameHost(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
