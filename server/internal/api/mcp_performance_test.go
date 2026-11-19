package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type observedBody struct{ reads int }

func (r *observedBody) Read(p []byte) (int, error) { r.reads++; return 0, io.EOF }
func (r *observedBody) Close() error               { return nil }

func TestStaticAssetSkipsSessionDatabase(t *testing.T) {
	// A nil DB makes any accidental lookup fail; include both due/invalid cookies.
	s := New(Config{AppURL: "http://localhost:8090"})
	h := s.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, path := range []string{"/_astro/x.js", "/fonts/x.woff2", "/images/x.png", "/favicon.ico", "/_astro/no-extension"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: s.sessionCookieName(), Value: "renew-me"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || len(rec.Header().Values("Set-Cookie")) > 0 {
			t.Fatalf("%s: %v", path, rec)
		}
	}
}

func TestMCPRejectsBeforeReadingBody(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MCPRateLimit = 1 })
	_, tokens := connectAgent(t, e, "user-director", "body-budget")
	token := tokens.str("access_token")
	if r := mcpRPC(e, token, "ping", nil); r.Status != 200 {
		t.Fatal(r)
	}
	body := &observedBody{}
	req := httptest.NewRequest("POST", "http://localhost:8090/mcp", body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 429 || body.reads != 0 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("%d reads=%d", rec.Code, body.reads)
	}
}

func TestMCPInFlightBodyCap(t *testing.T) {
	e := newEnv(t)
	_, tokens := connectAgent(t, e, "user-director", "concurrency-budget")
	token := tokens.str("access_token")
	_, otherTokens := connectAgent(t, e, "user-director", "other-agent")
	otherToken := otherTokens.str("access_token")
	release := make(chan struct{})
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	// Hold every available slot inside Read. The payload is represented by a gated
	// reader so rejection is tested without allocating N large uploads in the test.
	for i := 0; i < mcpPerGrantInFlight; i++ {
		entered := make(chan struct{})
		body := &gatedReadCloser{data: []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), entered: entered, release: release}
		req := httptest.NewRequest("POST", "http://localhost:8090/mcp", body)
		req.ContentLength = 15 << 20
		req.Header.Set("Authorization", "Bearer "+token)
		wg.Add(1)
		go func() { defer wg.Done(); e.h.ServeHTTP(httptest.NewRecorder(), req) }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("body not reached")
		}
	}
	for i := 0; i < 10; i++ {
		body := &observedBody{}
		req := httptest.NewRequest("POST", "http://localhost:8090/mcp", body)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 429 || body.reads != 0 {
			t.Fatalf("%d reads=%d", rec.Code, body.reads)
		}
	}
	if r := mcpRPC(e, token, "tools/call", map[string]any{"name": "whoami", "arguments": map[string]any{}}); r.Status != 429 {
		t.Fatalf("small tools/call bypassed the busy grant cap: %d %s", r.Status, r.Raw)
	}
	for _, method := range []string{"ping", "tools/list"} {
		if r := mcpRPC(e, token, method, nil); r.Status != 200 {
			t.Fatalf("busy grant control %s: %d %s", method, r.Status, r.Raw)
		}
		if r := mcpRPC(e, otherToken, method, nil); r.Status != 200 {
			t.Fatalf("other grant %s while first grant is busy: %d %s", method, r.Status, r.Raw)
		}
	}
	// Wait only after releasing readers (deferred cleanup also covers failures).
}

func TestStreamingBase64Parity(t *testing.T) {
	for _, s := range []string{"aGVsbG8=", "aGVs bG8=\n", "aGVsbG8", "_-8=", "/+8="} {
		raw, _ := json.Marshal(s)
		data, err := decodeContentBase64(raw)
		if err != nil || len(data) == 0 {
			t.Fatalf("%s: %x %v", s, data, err)
		}
	}
	for _, s := range []string{"a=b", "hello!", "aGVsbG8=bad"} {
		raw, _ := json.Marshal(s)
		if _, err := decodeContentBase64(raw); err == nil {
			t.Fatal(s)
		}
	}
	raw := json.RawMessage(`"aGVsbG8="`)
	data, err := decodeContentBase64(raw)
	if err != nil || strings.Compare(string(data), "hello") != 0 {
		t.Fatalf("%s %v", data, err)
	}
}
