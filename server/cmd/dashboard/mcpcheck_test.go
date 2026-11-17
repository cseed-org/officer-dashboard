package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"officerdashboard/internal/api"
	"officerdashboard/internal/store"
)

func TestMCPCheckCommand(t *testing.T) {
	var h http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	defer ts.Close()
	db, err := store.Open(t.TempDir() + "/d.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h = api.New(api.Config{DB: db, AppURL: ts.URL, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()

	mcpCheck := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := run(append([]string{"mcp-check"}, args...), env(nil), &out, &errb)
		return code, out.String(), errb.String()
	}
	// Flags after the URL work too.
	code, out, errs := mcpCheck(ts.URL+"/mcp", "-register")
	if code != 0 || !strings.Contains(out, "PASS  POST /oauth/register") || !strings.Contains(out, "0 failed") {
		t.Fatalf("against the dashboard → %d\n%s%s", code, out, errs)
	}
	if code, _, errs := mcpCheck(); code != 2 || !strings.Contains(errs, "usage: dashboard mcp-check") {
		t.Errorf("no URL → %d %q", code, errs)
	}
	if code, _, errs := mcpCheck("officers.example"); code != 2 || !strings.Contains(errs, "not an http(s) URL") {
		t.Errorf("bad URL → %d %q", code, errs)
	}

	// The Access service token comes from the environment and is sent, never printed.
	const id, secret = "0123abcd.access", "s3cr3t-0123456789abcdef"
	inner := h
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/ingest/") && (r.Header.Get("CF-Access-Client-Id") != id || r.Header.Get("CF-Access-Client-Secret") != secret) {
			w.Header().Set("Cf-Ray", "8f00000000000010-SEA")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
	var out2, errb2 bytes.Buffer
	code = run([]string{"mcp-check", ts.URL}, env(map[string]string{"CF_ACCESS_CLIENT_ID": id, "CF_ACCESS_CLIENT_SECRET": secret}), &out2, &errb2)
	if code != 0 || !strings.Contains(out2.String(), "PASS  POST /api/ingest/luma (service call): Access let the service token through") || strings.Contains(out2.String()+errb2.String(), secret) {
		t.Errorf("with the service token → %d\n%s%s", code, out2.String(), errb2.String())
	}
	out2.Reset()
	errb2.Reset()
	if code := run([]string{"mcp-check", ts.URL}, env(map[string]string{"CF_ACCESS_CLIENT_ID": id}), &out2, &errb2); code != 2 || !strings.Contains(errb2.String(), "set both CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET") {
		t.Errorf("only the id → %d %q", code, errb2.String())
	}
	h = inner
	h = http.NotFoundHandler()
	if code, out, _ := mcpCheck(ts.URL); code != 1 || !strings.Contains(out, "FAIL") {
		t.Errorf("not the dashboard → %d\n%s", code, out)
	}
}
