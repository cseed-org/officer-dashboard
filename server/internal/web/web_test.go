package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func site() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<h1>home</h1>")},
		"404.html":             {Data: []byte("<h1>nope</h1>")},
		"events/index.html":    {Data: []byte("<h1>events</h1>")},
		"login.html":           {Data: []byte("<h1>login</h1>")},
		"_astro/app.abc123.js": {Data: []byte("console.log(1)")},
		".env":                 {Data: []byte("SECRET=1")},
		"favicon.svg":          {Data: []byte("<svg/>")},
	}
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServesFilesIndexesAndExtensionlessPages(t *testing.T) {
	h := Handler(site(), Options{})
	cases := map[string]string{"/": "home", "/events": "events", "/events/": "events", "/login": "login", "/favicon.svg": "<svg/>"}
	for p, want := range cases {
		rec := get(h, "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s → %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if ct := get(h, "GET", "/login").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type %q", ct)
	}
}

func TestCachePolicy(t *testing.T) {
	h := Handler(site(), Options{})
	if cc := get(h, "GET", "/_astro/app.abc123.js").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("hashed asset cache-control %q", cc)
	}
	if cc := get(h, "GET", "/").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("html cache-control %q", cc)
	}
}

func TestUnknownPathsGetTheFallbackAndNeverLeakDotfilesOrEscapeTheRoot(t *testing.T) {
	h := Handler(site(), Options{})
	for _, p := range []string{"/nope", "/events/abc", "/.env", "/_astro/.env", "/../etc/passwd", "/%2e%2e/etc/passwd"} {
		rec := get(h, "GET", p)
		if rec.Code != 404 || !strings.Contains(rec.Body.String(), "nope") || strings.Contains(rec.Body.String(), "SECRET") {
			t.Errorf("%s → %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get(h, "POST", "/login"); rec.Code != 405 {
		t.Errorf("POST → %d", rec.Code)
	}
	// An SPA shell as the fallback answers 200 so client-side routes work.
	spa := Handler(fstest.MapFS{"index.html": {Data: []byte("shell")}, "200.html": {Data: []byte("shell")}}, Options{Fallback: "200.html", FallbackStatus: 200})
	if rec := get(spa, "GET", "/events/abc"); rec.Code != 200 || rec.Body.String() != "shell" {
		t.Errorf("spa fallback → %d %q", rec.Code, rec.Body.String())
	}
	// With no fallback page at all, a plain 404.
	bare := Handler(fstest.MapFS{"index.html": {Data: []byte("x")}}, Options{})
	if rec := get(bare, "GET", "/missing"); rec.Code != 404 {
		t.Errorf("bare → %d", rec.Code)
	}
	if rec := get(h, "HEAD", "/nope"); rec.Code != 404 || rec.Body.Len() != 0 {
		t.Errorf("HEAD fallback → %d %q", rec.Code, rec.Body.String())
	}
}
