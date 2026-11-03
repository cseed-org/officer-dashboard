package api

import (
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
)

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

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter(perMinute int, now func() time.Time) *rateLimiter {
	return &rateLimiter{perMinute: float64(perMinute), now: now, buckets: map[string]*bucket{}}
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

// clientIP is the peer address, or, when the peer is a reverse proxy on this machine (the
// supported deployments put Caddy or cloudflared on loopback), the last X-Forwarded-For hop,
// which that proxy appended. A client cannot spoof it: anything it sends comes earlier in
// the list. Direct loopback requests without the header are local tools and tests.
func clientIP(r *http.Request) (ip string, local bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !peer.IsLoopback() {
		return host, false
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return host, true
	}
	hops := strings.Split(xff[len(xff)-1], ",")
	if last := strings.TrimSpace(hops[len(hops)-1]); net.ParseIP(last) != nil {
		return last, false
	}
	return host, false
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
		ip, local := clientIP(r)
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
