package main

import (
	"bytes"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestSettingsFromEnvAndFlags(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	s, err := loadSettings(fs, nil, env(map[string]string{"ADDR": ":9000", "DB_PATH": "/tmp/x.db", "DEV_AUTH": "1", "DRIVE_ROOT_FOLDER_ID": "root"}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Addr != ":9000" || s.DBPath != "/tmp/x.db" || !s.DevAuth || s.DriveRoot != "root" || s.AppURL != "http://localhost:9000" || s.StaticFallback != "404.html" || s.StaticFallbackStatus != 404 {
		t.Errorf("settings %+v", s)
	}
	fs = flag.NewFlagSet("t", flag.ContinueOnError)
	s, _ = loadSettings(fs, []string{"-addr", ":1234", "-app-url", "https://officers.example/"}, env(nil))
	if s.Addr != ":1234" || s.AppURL != "https://officers.example" || s.DBPath != "./data/dashboard.db" {
		t.Errorf("defaults/flags %+v", s)
	}
	fs = flag.NewFlagSet("t", flag.ContinueOnError)
	if s, _ = loadSettings(fs, nil, env(nil)); s.Addr != ":8090" {
		t.Errorf("default addr %q", s.Addr)
	}
}

func TestSeedMigrateAndServeWiring(t *testing.T) {
	dir := t.TempDir()
	e := env(map[string]string{"DB_PATH": filepath.Join(dir, "d", "dash.db"), "RECEIPTS_DIR": filepath.Join(dir, "r")})
	var out, errb bytes.Buffer
	if code := run([]string{"seed", "-reset"}, e, &out, &errb); code != 0 {
		t.Fatalf("seed → %d %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "5 users, 6 events, 6 requests, 18 journal lines (FY2027)") {
		t.Errorf("seed output %q", out.String())
	}
	out.Reset()
	if code := run([]string{"migrate"}, e, &out, &errb); code != 0 || !strings.Contains(out.String(), "migrations applied") {
		t.Errorf("migrate → %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"version"}, e, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != version {
		t.Errorf("version → %q", out.String())
	}
	if code := run([]string{"bogus"}, e, &out, &errb); code != 2 {
		t.Errorf("unknown command → %d", code)
	}

	// The handler built from settings serves the API and the static directory.
	site := filepath.Join(dir, "site")
	_ = os.MkdirAll(site, 0o755)
	_ = os.WriteFile(filepath.Join(site, "index.html"), []byte("<h1>hi</h1>"), 0o644)
	_ = os.WriteFile(filepath.Join(site, "404.html"), []byte("<h1>gone</h1>"), 0o644)
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	s, _ := loadSettings(fs, []string{"-static", site}, e)
	db, err := openDB(s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv, err := buildServer(s, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	for path, want := range map[string]int{"/": 200, "/api/health": 200, "/missing": 404, "/api/me": 401, "/auth/login": 503} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s → %d want %d (%s)", path, rec.Code, want, rec.Body.String())
		}
	}
}

func TestColdStartIsFast(t *testing.T) {
	dir := t.TempDir()
	e := env(map[string]string{"DB_PATH": filepath.Join(dir, "dash.db")})
	var out, errb bytes.Buffer
	if run([]string{"seed"}, e, &out, &errb) != 0 {
		t.Fatal(errb.String())
	}
	start := time.Now()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	s, _ := loadSettings(fs, nil, e)
	db, err := openDB(s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := buildServer(s, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Errorf("open + migrate-check + wire took %s, budget 300ms", d)
	}
}

func TestHealthcheckCommand(t *testing.T) {
	dir := t.TempDir()
	e := env(map[string]string{"DB_PATH": filepath.Join(dir, "dash.db"), "RECEIPTS_DIR": filepath.Join(dir, "r")})
	var out, errb bytes.Buffer
	if run([]string{"seed"}, e, &out, &errb) != 0 {
		t.Fatal(errb.String())
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	s, _ := loadSettings(fs, nil, e)
	db, err := openDB(s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv, err := buildServer(s, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")
	if code := run([]string{"healthcheck", "-addr", addr}, e, &out, &errb); code != 0 {
		t.Errorf("healthy server → %d %s", code, errb.String())
	}
	ts.Close()
	errb.Reset()
	if code := run([]string{"healthcheck", "-addr", addr}, e, &out, &errb); code != 1 {
		t.Errorf("a dead server should fail the check, got %d", code)
	}
}

// #4: production must be https, or the Secure flag, __Host- prefix and HSTS silently vanish.
func TestAppURLMustBeHTTPSExceptOnLocalhost(t *testing.T) {
	ok := []string{"https://officers.cseed.co", "http://localhost:8090", "http://127.0.0.1:9000", "http://[::1]:8090", "http://localhost"}
	bad := []string{"http://officers.cseed.co", "http://192.168.1.20:8090", "ftp://x", "officers.cseed.co", "http://localhost.evil.example"}
	for _, u := range ok {
		if err := validateAppURL(u); err != nil {
			t.Errorf("%s should be allowed: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := validateAppURL(u); err == nil {
			t.Errorf("%s should be refused", u)
		}
	}
	var out, errb bytes.Buffer
	e := env(map[string]string{"DB_PATH": filepath.Join(t.TempDir(), "d.db"), "APP_URL": "http://officers.cseed.co", "ADDR": "127.0.0.1:0"})
	if code := run([]string{"serve"}, e, &out, &errb); code != 1 || !strings.Contains(errb.String(), "https") {
		t.Errorf("serve with an http APP_URL → %d %s", code, errb.String())
	}
}
