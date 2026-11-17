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
	h = http.NotFoundHandler()
	if code, out, _ := mcpCheck(ts.URL); code != 1 || !strings.Contains(out, "FAIL") {
		t.Errorf("not the dashboard → %d\n%s", code, out)
	}
}
