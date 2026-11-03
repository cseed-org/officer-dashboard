package web

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PermissionsPolicy turns off browser features the dashboard never uses.
const PermissionsPolicy = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"

// pagePolicyFor builds a page's Content-Security-Policy. Scripts: same-origin files plus the
// page's own inline scripts by hash (theme pre-paint, navigation, Astro's island loader), so
// injected markup cannot run script. Styles allow inline attributes (Preact sets style=).
// Images: same origin, data:/blob: previews and Discord avatars. Forms may post here and
// continue to Discord or Google sign-in.
func pagePolicyFor(hashes []string) string {
	script := "script-src 'self'"
	for _, h := range hashes {
		script += " 'sha256-" + h + "'"
	}
	return strings.Join([]string{
		"default-src 'self'",
		script,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https://cdn.discordapp.com",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
		"form-action 'self' https://discord.com https://accounts.google.com",
	}, "; ")
}

var inlineScriptRE = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
var srcAttrRE = regexp.MustCompile(`(?i)\ssrc\s*=`)

// inlineScriptHashes returns the base64 sha256 of each inline <script> body in page.
func inlineScriptHashes(page []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range inlineScriptRE.FindAllSubmatch(page, -1) {
		if srcAttrRE.Match(m[1]) {
			continue
		}
		sum := sha256.Sum256(m[2])
		h := base64.StdEncoding.EncodeToString(sum[:])
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

type policyKey struct {
	name string
	size int64
	mod  time.Time
}

// policies caches each HTML file's CSP per file version (size and mtime), so a page is
// hashed once, and again only when a development rebuild changes it.
type policies struct{ cache sync.Map } // policyKey → string

// pagePolicy computes the CSP of an HTML file in root.
func (pc *policies) pagePolicy(root fs.FS, name string, info fs.FileInfo) (string, error) {
	key := policyKey{name, info.Size(), info.ModTime()}
	if v, ok := pc.cache.Load(key); ok {
		return v.(string), nil
	}
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	page, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	p := pagePolicyFor(inlineScriptHashes(page))
	pc.cache.Store(key, p)
	return p, nil
}
