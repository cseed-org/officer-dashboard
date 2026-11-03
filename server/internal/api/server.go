// Package api is the HTTP layer: the JSON API of v1 (docs/api.md), the auth routes and
// static file serving. Handlers hold the rules; SQL lives in the store package.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/cfsync"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/files"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/notion"
	"officerdashboard/internal/purchases"
	"officerdashboard/internal/store"
	"officerdashboard/internal/web"
)

// Config wires the server to its collaborators.
type Config struct {
	Cloudflare *cfsync.Service
	DB         *store.DB
	Files      files.Store
	Notifier   notify.Notifier
	// DM sends invite DMs through the Discord bot; nil when DISCORD_BOT_TOKEN is unset,
	// and invites fall back to copy-link only.
	DM notify.DirectMessenger
	// Mailer sends .ics invitations; nil when SMTP is not configured.
	Mailer calendar.Mailer
	// Google syncs officers' Google Calendars; nil when the OAuth client is not configured.
	Google *calendar.Google
	// Notion drafts each event's Notion page. TODO(notion): only the stub exists so far.
	Notion notion.NotionSync
	// Now is the clock; tests fix it.
	Now       func() time.Time
	Tolerance purchases.Tolerance
	Discord   *auth.Discord // nil when Discord sign-in is not configured
	AppURL    string
	// DevAuth enables /auth/dev, but only in binaries built with the `dev` tag.
	DevAuth bool
	// DevAuthKey, when set, gates /auth/dev behind ?key=<DevAuthKey> (or the dev_key cookie).
	// Dev builds only; never logged.
	DevAuthKey string
	// AuthRateLimit and UploadRateLimit are the requests per minute one client IP may make
	// to /auth/* and to the upload/import routes. 0 uses the defaults; negative disables.
	AuthRateLimit, UploadRateLimit int
	// Static serves the front end for every non-API path; nil answers 404 JSON.
	Static  http.Handler
	Version string
	Log     *slog.Logger
}

// Server is the application.
type Server struct {
	cfg    Config
	db     *store.DB
	log    *slog.Logger
	secure bool
	host   string // APP_URL host, for the Origin check

	authLimit, uploadLimit *rateLimiter

	sealer     *calendar.Sealer
	bg         sync.WaitGroup // background calendar deliveries
	calMu      sync.Mutex     // guards calQueue and calRunning
	calQueue   []calJob       // FIFO; one worker drains it
	calRunning bool
}

// New builds a server from its config.
func New(cfg Config) *Server {
	if cfg.Cloudflare == nil {
		cfg.Cloudflare = &cfsync.Service{Client: cloudflare.New(cloudflare.Config{}), DB: cfg.DB, Log: cfg.Log}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Notifier == nil {
		cfg.Notifier = notify.Console{Log: cfg.Log}
	}
	if cfg.Notion == nil {
		cfg.Notion = notion.Stub{}
	}
	if cfg.Files == nil {
		cfg.Files = files.Unavailable{}
	}
	if cfg.Version == "" {
		cfg.Version = "0.1.0"
	}
	s := &Server{cfg: cfg, db: cfg.DB, log: cfg.Log}
	s.authLimit = newRateLimiter(orDefault(cfg.AuthRateLimit, defaultAuthRateLimit), cfg.Now)
	s.uploadLimit = newRateLimiter(orDefault(cfg.UploadRateLimit, defaultUploadRateLimit), cfg.Now)
	if cfg.Google != nil {
		s.sealer = calendar.NewSealer(cfg.Google.ClientSecret)
	}
	if u, err := url.Parse(cfg.AppURL); err == nil {
		s.secure = u.Scheme == "https"
		s.host = u.Host
	}
	return s
}

func (s *Server) now() time.Time { return s.cfg.Now() }

// WaitBackground blocks until background calendar deliveries finish (tests, shutdown).
func (s *Server) WaitBackground() { s.bg.Wait() }

// WaitBackgroundFor waits at most d for background deliveries; it reports whether they finished.
func (s *Server) WaitBackgroundFor(d time.Duration) bool {
	done := make(chan struct{})
	go func() { s.bg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
func (s *Server) nowMs() int64 { return s.now().UnixMilli() }

// Ctx carries one request through a handler.
type Ctx struct {
	S *Server
	W http.ResponseWriter
	R *http.Request
	// User is the signed-in user (session cookie); nil for anonymous and service-token calls.
	User *store.User
	// Token is the authenticated service token, for /api/ingest and /api/agent.
	Token *store.ServiceToken
	// SessionID is the sha256 of the session cookie, used by logout.
	SessionID string
}

func (c *Ctx) Context() context.Context { return c.R.Context() }

// officer requires a signed-in user.
func (c *Ctx) officer() (*store.User, error) {
	if c.User == nil {
		return nil, fail(401, "unauthenticated", "Sign in first")
	}
	return c.User, nil
}

// director requires a signed-in director.
func (c *Ctx) director() (*store.User, error) {
	u, err := c.officer()
	if err != nil {
		return nil, err
	}
	if !auth.HasRole(u.Role, auth.RoleDirector) {
		return nil, forbidden("Requires director role")
	}
	return u, nil
}

type handlerFunc func(c *Ctx) error

// Handler returns the full http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	var h http.Handler = mux
	h = s.withAuth(h)
	h = s.withRateLimit(h)
	h = s.withAPIHygiene(h)
	h = s.withRecover(h)
	return h
}

func (s *Server) wrap(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &Ctx{S: s, W: w, R: r}
		if v, ok := r.Context().Value(principalKey{}).(principal); ok {
			c.User, c.SessionID = v.user, v.sessionID
		}
		if err := fn(c); err != nil {
			writeError(w, toAPIError(err, s.log))
		}
	}
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic serving request", "path", r.URL.Path, "panic", v)
				writeError(w, fail(500, "internal", "Operation failed"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withAPIHygiene normalises trailing slashes on API paths and adds security headers.
func (s *Server) withAPIHygiene(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// Nothing but a page may load or run anything; the static handler replaces this with
		// the page policy (script hashes) on HTML documents.
		h.Set("Content-Security-Policy", apiCSP)
		h.Set("Permissions-Policy", web.PermissionsPolicy)
		// A body is never sent untyped: Go would sniff it, and a sniffed text/html body is a
		// reflected-XSS vector. Every handler sets its real type with Set, which replaces this
		// default; one that forgot sends inert bytes. Setting it up front (rather than wrapping
		// the writer) keeps response bytes flowing straight to the real ResponseWriter.
		h.Set("Content-Type", "application/octet-stream")
		if s.secure {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if p := r.URL.Path; len(p) > 5 && strings.HasPrefix(p, "/api/") && strings.HasSuffix(p, "/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimRight(p, "/")
			r = r2
		}
		next.ServeHTTP(w, r)
	})
}

// apiCSP is the policy of every non-page response (JSON, files, redirects, errors).
const apiCSP = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// requireLeadership protects role assignment, including promotions to leadership.
func (c *Ctx) requireLeadership() (*store.User, error) {
	u, err := c.officer()
	if err != nil {
		return nil, err
	}
	if !auth.HasRole(u.Role, auth.RoleLeadership) {
		return nil, forbidden("Requires leadership role")
	}
	return u, nil
}

// requireLeadershipIn re-reads the caller inside the write transaction, so a
// role change committed while this request was in flight is honoured.
func (c *Ctx) requireLeadershipIn(q *store.Queries) (*store.User, error) {
	if _, err := c.requireLeadership(); err != nil {
		return nil, err
	}
	u, err := q.UserByID(c.Context(), c.User.ID)
	if err != nil {
		return nil, err
	}
	if !u.Active || !auth.HasRole(u.Role, auth.RoleLeadership) {
		return nil, forbidden("Requires leadership role")
	}
	return u, nil
}

// CloudflareDaily runs the daily job until shutdown.
func (s *Server) CloudflareDaily(ctx context.Context) { s.cfg.Cloudflare.Daily(ctx) }
