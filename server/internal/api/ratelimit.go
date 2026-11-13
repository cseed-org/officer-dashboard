package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuthRateLimit   = 30 // /auth/* requests per minute per client IP
	defaultUploadRateLimit = 30 // upload and import requests per minute per client IP
	// maxLimiterKeys bounds each limiter's memory: past it, an arbitrary bucket is dropped
	// for each new client (at worst that client starts over with a full bucket).
	maxLimiterKeys = 10_000
)

// DefaultTrustedProxies is loopback: the supported single-host deployments put Caddy or
// cloudflared on this machine.
func DefaultTrustedProxies() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"127.0.0.0/8", "::1/128"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}

// ParseTrustedProxies reads TRUSTED_PROXIES: comma-separated CIDRs or bare IPs.
func ParseTrustedProxies(v string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			ip := net.ParseIP(f)
			if ip == nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP or CIDR", f)
			}
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(f)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP or CIDR", f)
		}
		out = append(out, n)
	}
	return out, nil
}

func orDefault(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

// rateLimiter is a per-key token bucket: `perMinute` requests, refilled continuously.
// perMinute < 0 disables it. Idle buckets are swept so the map stays small.
type rateLimiter struct {
	perMinute float64
	now       func() time.Time

	maxKeys int

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter(perMinute int, now func() time.Time) *rateLimiter {
	return &rateLimiter{perMinute: float64(perMinute), now: now, maxKeys: maxLimiterKeys, buckets: map[string]*bucket{}}
}

// allow takes one token for key; when none is left it reports how long until one is.
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	if l.perMinute < 0 {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > 5*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.at) > 2*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[key]
	if !ok {
		for k := range l.buckets { // map order is random: drop arbitrary entries, O(1) each
			if len(l.buckets) < l.maxKeys {
				break
			}
			delete(l.buckets, k)
		}
		b = &bucket{tokens: l.perMinute, at: now}
		l.buckets[key] = b
	}
	if el := now.Sub(b.at); el > 0 {
		b.tokens = min(l.perMinute, b.tokens+el.Minutes()*l.perMinute)
		b.at = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.perMinute * float64(time.Minute))
	return false, wait
}

// clientIP is the address a request is limited by. The peer, unless it is a trusted proxy
// (TRUSTED_PROXIES, loopback by default): then X-Forwarded-For is read from the right,
// skipping trusted hops, and the first untrusted hop is the client. Each proxy appends the
// address it received from, so anything the client wrote itself sits further left and is
// never reached. IPv6 clients are keyed by their /64. A direct loopback request without the
// header (local tools, tests) is reported as local and not limited.
func clientIP(r *http.Request, trusted []*net.IPNet) (key string, local bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return host, false
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 && peer.IsLoopback() {
		return host, true
	}
	if !inNets(peer, trusted) {
		return ipKey(peer), false
	}
	hops := strings.Split(strings.Join(xff, ","), ",")
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			break // a malformed hop: trust nothing further left
		}
		client = ip
		if !inNets(ip, trusted) {
			break
		}
	}
	return ipKey(client), false
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ipKey is the bucket key: the IPv4 address, or the /64 of an IPv6 one.
func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// limitedRoute names the budget a request draws on: sign-in routes and the upload/import
// routes (expensive, and the ones a script would hammer). Everything else is unlimited.
func limitedRoute(r *http.Request) string {
	p := cleanPath(r.URL.Path)
	if strings.HasPrefix(p, "/auth/") {
		return "auth"
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return ""
	}
	seg := strings.Split(strings.TrimPrefix(p, "/"), "/")
	switch {
	case p == "/api/events/import", p == "/api/ingest/luma":
		return "upload"
	case len(seg) == 4 && seg[0] == "api" && seg[1] == "requests" && seg[3] == "attachments":
		return "upload"
	case len(seg) == 4 && seg[0] == "api" && seg[1] == "design-requests" && seg[3] == "files":
		return "upload"
	case len(seg) == 4 && seg[0] == "api" && seg[1] == "events" && seg[3] == "cover" && r.Method == http.MethodPut:
		return "upload"
	}
	return ""
}

func (s *Server) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := limitedRoute(r)
		if kind == "" {
			next.ServeHTTP(w, r)
			return
		}
		ip, local := clientIP(r, s.trustedProxies)
		if local {
			next.ServeHTTP(w, r)
			return
		}
		l := s.authLimit
		if kind == "upload" {
			l = s.uploadLimit
		}
		if ok, wait := l.allow(ip); !ok {
			secs := int(wait/time.Second) + 1
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, fail(http.StatusTooManyRequests, "rate_limited", "Too many requests; try again in a minute"))
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Too many requests; try again in a minute.\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
