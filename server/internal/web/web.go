// Package web serves the Astro static build (web/dist): from a directory in development,
// or from files embedded into the binary in release builds (`-tags embedweb`).
package web

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		for _, seg := range strings.Split(clean, "/") {
			if strings.HasPrefix(seg, ".") && seg != "" {
				notFound(w, r, root, opt)
				return
			}
		}
		name := strings.TrimPrefix(clean, "/")
		for _, cand := range candidates(root, name) {
			if serveFile(w, r, root, cand, http.StatusOK) {
				return
			}
		}
		notFound(w, r, root, opt)
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

func notFound(w http.ResponseWriter, r *http.Request, root fs.FS, opt Options) {
	if !serveFile(w, r, root, opt.Fallback, opt.FallbackStatus) {
		http.NotFound(w, r)
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, root fs.FS, name string, status int) bool {
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
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}
