// Package web serves the Astro static build (web/dist): from a directory in development,
// or from files embedded into the binary in release builds (`-tags embedweb`).
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// Options tune the not-found behaviour.
type Options struct {
	// Fallback is the page served (relative to the root) for paths that match no file.
	// The front end decides what it is: 404.html answers 404, an SPA shell answers 200.
	Fallback       string
	FallbackStatus int
}

// Handler serves files from root. A directory serves its index.html, a path without an
// extension tries `<path>.html`, and everything else unknown gets the fallback page.
// Dotfiles are never served and directories are never listed.
func Handler(root fs.FS, opt Options) http.Handler {
	if opt.Fallback == "" {
		opt.Fallback = "404.html"
	}
	if opt.FallbackStatus == 0 {
		opt.FallbackStatus = http.StatusNotFound
	}
	pc := &policies{}
	assets := prepareAssets(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		for _, seg := range strings.Split(clean, "/") {
			if strings.HasPrefix(seg, ".") && seg != "" {
				notFound(w, r, pc, root, opt, assets)
				return
			}
		}
		name := strings.TrimPrefix(clean, "/")
		for _, cand := range candidates(root, name) {
			if serveFile(w, r, pc, root, cand, http.StatusOK, assets) {
				return
			}
		}
		notFound(w, r, pc, root, opt, assets)
	})
}

func candidates(root fs.FS, name string) []string {
	if name == "" {
		return []string{"index.html"}
	}
	var out []string
	if info, err := fs.Stat(root, name); err == nil {
		if info.IsDir() {
			out = append(out, name+"/index.html")
		} else {
			out = append(out, name)
		}
	}
	if path.Ext(name) == "" {
		out = append(out, name+".html", name+"/index.html")
	}
	return out
}

func notFound(w http.ResponseWriter, r *http.Request, pc *policies, root fs.FS, opt Options, assets map[string]asset) {
	if !serveFile(w, r, pc, root, opt.Fallback, opt.FallbackStatus, assets) {
		http.NotFound(w, r)
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, pc *policies, root fs.FS, name string, status int, assets map[string]asset) bool {
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	h := w.Header()
	// Never let a browser guess a type: an unknown file is bytes, not HTML.
	h.Set("Content-Type", contentType(name))
	h.Set("X-Content-Type-Options", "nosniff")
	if strings.HasSuffix(name, ".html") {
		csp, err := pc.pagePolicy(root, name, info)
		if err != nil {
			return false
		}
		h.Set("Content-Security-Policy", csp)
		h.Set("Permissions-Policy", PermissionsPolicy)
	}
	switch {
	case strings.HasPrefix(name, "_astro/"):
		// Content-hashed build output never changes under the same name.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	case strings.HasSuffix(name, ".html"):
		h.Set("Cache-Control", "no-cache")
	default:
		h.Set("Cache-Control", "public, max-age=300")
	}
	if status != http.StatusOK {
		// ServeContent would turn this into a 200; write the fallback by hand.
		if ct := contentType(name); ct != "" {
			h.Set("Content-Type", ct)
		}
		body, err := io.ReadAll(f)
		if err != nil {
			return false
		}
		h.Set("Cache-Control", "no-cache")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return true
	}
	if a, ok := assets[name]; ok && a.size == info.Size() && a.modTime.Equal(info.ModTime()) {
		h.Set("ETag", a.etag)
		h.Add("Vary", "Accept-Encoding")
		body := a.body
		if acceptsGzip(r.Header.Get("Accept-Encoding")) && len(a.gzip) > 0 && r.Header.Get("Range") == "" {
			body = a.gzip
			h.Set("Content-Encoding", "gzip")
		}
		if ct := a.contentType; ct != "" {
			h.Set("Content-Type", ct)
		}
		http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(body))
		return true
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		body, err := io.ReadAll(f)
		if err != nil {
			return false
		}
		rs = bytes.NewReader(body)
	}
	http.ServeContent(w, r, path.Base(name), time.Time{}, rs)
	return true
}

func contentType(name string) string {
	switch ext := path.Ext(name); ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json"
	default:
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}

// Prepare text once at startup, rather than compressing on the Raspberry Pi's request path.
// Large uploads and images are served directly; this cache is only for build output.
type asset struct {
	body, gzip        []byte
	etag, contentType string
	size              int64
	modTime           time.Time
}

func prepareAssets(root fs.FS) map[string]asset {
	assets := make(map[string]asset)
	fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch path.Ext(name) {
		case ".html", ".css", ".js", ".svg", ".json":
		default:
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 2<<20 {
			return nil
		}
		body, err := fs.ReadFile(root, name)
		if err != nil {
			return nil
		}
		var buf bytes.Buffer
		z, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
		_, _ = z.Write(body)
		_ = z.Close()
		a := asset{body: body, size: info.Size(), modTime: info.ModTime(), etag: fmt.Sprintf("W/\"%x\"", sha256.Sum256(body)), contentType: contentType(name)}
		if a.contentType == "" {
			switch path.Ext(name) {
			case ".js":
				a.contentType = "text/javascript; charset=utf-8"
			case ".css":
				a.contentType = "text/css; charset=utf-8"
			case ".svg":
				a.contentType = "image/svg+xml"
			}
		}
		if buf.Len() < len(body) {
			a.gzip = buf.Bytes()
		}
		assets[name] = a
		return nil
	})
	return assets
}
func acceptsGzip(header string) bool {
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(item), ";")
		if !strings.EqualFold(parts[0], "gzip") {
			continue
		}
		for _, param := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && key == "q" {
				q, err := strconv.ParseFloat(value, 64)
				if err != nil || !(q > 0 && q <= 1) {
					return false
				}
			}
		}
		return true
	}
	return false
}
