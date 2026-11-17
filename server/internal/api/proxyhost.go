package api

import (
	"net"
	"net/http"
	"strings"
)

// withForwardedHostCheck logs one warning the first time a request that came through a
// trusted proxy (TRUSTED_PROXIES; it carries X-Forwarded-For) names a Host other than
// APP_URL's. That almost always means a tunnel or proxy hostname is routed to this server
// that APP_URL does not name: OAuth discovery, the WWW-Authenticate challenge, cookies and
// redirects all use APP_URL, so agents connecting through that hostname fail in confusing
// ways. The request itself is served unchanged. Direct requests (the container health
// check, local tools) carry no X-Forwarded-For and are never considered.
func (s *Server) withForwardedHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.host != "" && !s.hostWarned.Load() && len(r.Header.Values("X-Forwarded-For")) > 0 &&
			!sameHost(r.Host, s.host) && s.fromTrustedProxy(r) && s.hostWarned.CompareAndSwap(false, true) {
			s.log.Warn("a request forwarded by a trusted proxy names a different host than APP_URL: "+
				"check the tunnel's public hostname (or the proxy's Host header) and APP_URL; "+
				"OAuth metadata, cookies and redirects use APP_URL. Logged once.",
				"host", r.Host, "app_url", s.cfg.AppURL, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

// fromTrustedProxy reports whether the request's peer is one of TRUSTED_PROXIES.
func (s *Server) fromTrustedProxy(r *http.Request) bool {
	ip := net.ParseIP(peerHost(r))
	return ip != nil && inNets(ip, s.trustedProxies)
}

// sameHost compares two Host values case-insensitively, ignoring an explicit default port
// (a proxy may forward "officers.example.org:443").
func sameHost(a, b string) bool {
	trim := func(h string) string {
		h = strings.ToLower(h)
		if host, port, err := net.SplitHostPort(h); err == nil && (port == "443" || port == "80") {
			if strings.Contains(host, ":") {
				return "[" + host + "]"
			}
			return host
		}
		return h
	}
	return trim(a) == trim(b)
}
