package web

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io"
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

func TestHelloMediaTypesAndRanges(t *testing.T) {
	fsys := site()
	fsys["media/hello-loop.mp4"] = &fstest.MapFile{Data: []byte("0123456789")}
	fsys["media/hello-loop.webm"] = &fstest.MapFile{Data: []byte("0123456789")}
	fsys["media/hello-group.jpg"] = &fstest.MapFile{Data: []byte("jpeg")}
	h := Handler(fsys, Options{})
	for p, want := range map[string]string{"/media/hello-loop.mp4": "video/mp4", "/media/hello-loop.webm": "video/webm", "/media/hello-group.jpg": "image/jpeg"} {
		if ct := get(h, "GET", p).Header().Get("Content-Type"); ct != want {
			t.Errorf("%s content type %q, want %q", p, ct, want)
		}
	}
	// Video players (Safari above all) need byte ranges.
	req := httptest.NewRequest("GET", "/media/hello-loop.mp4", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" {
		t.Errorf("range → %d %q", rec.Code, rec.Body.String())
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

// Every HTML page gets a Content-Security-Policy that allows exactly its own inline
// scripts (by sha256) and nothing inline beyond them; other files get a type and nosniff.
func TestHTMLPagesCarryAContentSecurityPolicyWithTheirScriptHashes(t *testing.T) {
	inline := "console.log('prepaint')"
	fs := site()
	fs["app.html"] = &fstest.MapFile{Data: []byte("<!doctype html><script>" + inline + "</script><script type=\"module\" src=\"/_astro/app.abc123.js\"></script><h1>app</h1>")}
	fs["weird.bin"] = &fstest.MapFile{Data: []byte("<html><script>alert(1)</script>")}
	h := Handler(fs, Options{})
	rec := get(h, "GET", "/app")
	csp := rec.Header().Get("Content-Security-Policy")
	sum := sha256.Sum256([]byte(inline))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	for _, want := range []string{"default-src 'self'", "script-src 'self' " + hash, "img-src 'self' data: blob: https://cdn.discordapp.com",
		"object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'", "form-action 'self' https://discord.com https://accounts.google.com"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline';") && strings.Contains(csp, "script-src 'self' 'unsafe-inline'") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP allows arbitrary inline script: %s", csp)
	}
	if pp := rec.Header().Get("Permissions-Policy"); !strings.Contains(pp, "camera=()") || !strings.Contains(pp, "geolocation=()") {
		t.Errorf("Permissions-Policy %q", pp)
	}
	// The 404 page is HTML too.
	if nf := get(h, "GET", "/missing"); nf.Header().Get("Content-Security-Policy") == "" || !strings.HasPrefix(nf.Header().Get("Content-Type"), "text/html") {
		t.Errorf("404 headers %v", nf.Header())
	}
	// Unknown types are never sniffed into HTML.
	bin := get(h, "GET", "/weird.bin")
	if ct := bin.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/html") || ct == "" {
		t.Errorf("unknown file served as %q", ct)
	}
	if bin.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}

func TestCompressedAssetsAndRevalidation(t *testing.T) {
	body := strings.Repeat("export const answer = 42;\n", 100)
	h := Handler(fstest.MapFS{"_astro/app.js": {Data: []byte(body)}}, Options{})
	req := httptest.NewRequest("GET", "/_astro/app.js", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(body) {
		t.Fatalf("compression: %d %v", rec.Code, rec.Header())
	}
	z, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(z)
	if err != nil || string(decoded) != body {
		t.Fatal("corrupt gzip response")
	}
	z.Close()
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	h.ServeHTTP(cached, req)
	if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
		t.Fatalf("revalidation: %d", cached.Code)
	}
	for _, encoding := range []string{"", "gzip;q=0", "gzip;q=0.000"} {
		req := httptest.NewRequest("GET", "/_astro/app.js", nil)
		req.Header.Set("Accept-Encoding", encoding)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != body {
			t.Fatalf("identity %q", encoding)
		}
	}
	req = httptest.NewRequest("HEAD", "/_astro/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatal("HEAD returned body")
	}
	req = httptest.NewRequest("GET", "/_astro/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-5")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 206 || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != body[:6] {
		t.Fatal("range did not use identity representation")
	}
}

func TestChangedBuildFileIsNotServedFromStartupCache(t *testing.T) {
	root := site()
	h := Handler(root, Options{})
	root["index.html"] = &fstest.MapFile{Data: []byte("rebuilt home page")}
	rec := get(h, "GET", "/")
	if rec.Body.String() != "rebuilt home page" || rec.Header().Get("ETag") != "" {
		t.Fatal("served stale startup bytes")
	}
}

func TestBuildspaceLegacyRedirect(t *testing.T) {
	for _, path := range []string{"/buildspace?person=abc", "/buildspace/?person=abc"} {
		r := get(Handler(site(), Options{}), "GET", path)
		if r.Code != 301 || r.Header().Get("Location") != "/buildspace-launchpad?person=abc" {
			t.Fatalf("redirect: %d %s", r.Code, r.Header().Get("Location"))
		}
	}
}
