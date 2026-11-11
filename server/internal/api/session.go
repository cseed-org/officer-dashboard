package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

const (
	sessionTTL    = 30 * 24 * time.Hour
	renewWhenLeft = 15 * 24 * time.Hour
	// sessionMaxAge caps a session however often it is renewed: everyone signs in again
	// 30 days after signing in, however active. Equal to sessionTTL, so the renewal below
	// never moves the expiry past created_at + 30 days (it only matters if the TTL shrinks).
	sessionMaxAge    = 30 * 24 * time.Hour
	oauthStateName   = "cseed_oauth_state"
	oauthVerifier    = "cseed_oauth_verifier"
	nextCookieName   = "cseed_next"
	inviteCookieName = "cseed_invite"
	googleStateName  = "cseed_google_state"
	googleVerifier   = "cseed_google_verifier"
	googleNextName   = "cseed_google_next"
)

type principalKey struct{}

type principal struct {
	user      *store.User
	sessionID string
}

// sessionCookieName: `__Host-` pins the cookie to this origin over HTTPS; the plain name
// is used on http://localhost.
func (s *Server) sessionCookieName() string {
	return s.hostPrefixed("cseed_session")
}

// hostPrefixed adds the `__Host-` prefix over HTTPS so a sibling subdomain or a plain-http
// attacker cannot plant or shadow the cookie. Plain names are kept on http://localhost.
func (s *Server) hostPrefixed(name string) string {
	if s.secure {
		return "__Host-" + name
	}
	return name
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(0, 0)})
}

// newSession signs this browser in as userID, replacing the session it already had (if any)
// so a re-login never leaves the old token alive.
func (c *Ctx) newSession(ctx context.Context, userID string) error {
	return c.S.createSession(ctx, c.W, userID, c.R.UserAgent(), c.SessionID)
}

// createSession stores a session and sets the cookie. Only sha256(token) is kept. replaces,
// when set, is the hash of the browser's previous session, deleted in the same transaction.
func (s *Server) createSession(ctx context.Context, w http.ResponseWriter, userID, userAgent, replaces string) error {
	token := auth.RandomToken(32)
	expires := s.now().Add(min(sessionTTL, sessionMaxAge)) // never past created_at + sessionMaxAge
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	err := s.db.Write(ctx, func(q *store.Queries) error {
		if replaces != "" {
			if err := q.DeleteSession(ctx, replaces); err != nil {
				return err
			}
		}
		return q.InsertSession(ctx, auth.SHA256Hex(token), userID, expires.UnixMilli(), userAgent, s.nowMs())
	})
	if err != nil {
		return err
	}
	s.setCookie(w, s.sessionCookieName(), token, expires, 0)
	return nil
}

// withAuth attaches the session user, then enforces the path's minimum role: API paths
// answer 401/403 JSON. Static pages stay public; the data behind them is API-protected.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p principal
		if ck, err := r.Cookie(s.sessionCookieName()); err == nil && ck.Value != "" {
			id := auth.SHA256Hex(ck.Value)
			sess, err := s.db.Read().SessionByHash(r.Context(), id, s.nowMs())
			hardStop := time.UnixMilli(0)
			if err == nil {
				hardStop = time.UnixMilli(sess.CreatedAt).Add(sessionMaxAge)
				if !s.now().Before(hardStop) {
					// Past the absolute lifetime: the session is over however recently used.
					_ = s.db.Write(r.Context(), func(q *store.Queries) error { return q.DeleteSession(r.Context(), id) })
					err = store.ErrNotFound
				}
			}
			switch {
			case err == nil:
				p = principal{user: &sess.User, sessionID: id}
				// Slide the expiry while used, never past hardStop (created_at + sessionMaxAge),
				// and write only when it actually moves later.
				if time.UnixMilli(sess.ExpiresAt).Sub(s.now()) < renewWhenLeft {
					exp := s.now().Add(sessionTTL)
					if exp.After(hardStop) {
						exp = hardStop
					}
					if exp.UnixMilli() > sess.ExpiresAt {
						if werr := s.db.Write(r.Context(), func(q *store.Queries) error { return q.ExtendSession(r.Context(), id, exp.UnixMilli()) }); werr == nil {
							s.setCookie(w, s.sessionCookieName(), ck.Value, exp, 0)
						}
					}
				}
			case errors.Is(err, store.ErrNotFound):
				s.clearCookie(w, s.sessionCookieName())
			case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
				writeError(w, toAPIError(err, s.log))
				return
			default:
				s.log.Error("session lookup failed", "error", err)
			}
		}
		// Decide the role on the canonical path so "//" and dot segments cannot dodge a prefix.
		path := r.URL.Path
		if strings.HasPrefix(path, "/api") {
			path = cleanPath(path)
		}
		if strings.HasPrefix(path, "/api/") {
			if required := auth.RequiredRoleForPath(path); required != "" {
				if p.user == nil {
					writeError(w, fail(401, "unauthenticated", "Sign in first"))
					return
				}
				if !auth.HasRole(p.user.Role, required) {
					writeError(w, forbidden("Requires the "+required+" role"))
					return
				}
			}
			if p.user != nil && !onboardingOpenPath(r.Method, path) {
				pending, err := s.onboardingPending(r.Context(), p.user.ID)
				if err != nil {
					writeError(w, toAPIError(err, s.log))
					return
				}
				if pending {
					writeError(w, fail(403, "onboarding_required", "Finish setting up your account first"))
					return
				}
			}
		}
		if p.user != nil && !s.originOK(r) {
			writeError(w, forbidden("Cross-origin request blocked"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// originOK blocks cross-site writes that ride a session cookie: an unsafe request whose
// Origin names another host is refused. SameSite=Lax already stops most of these; this is
// the belt to that braces. Requests without an Origin header (curl, server-to-server) pass.
func (s *Server) originOK(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) || (s.host != "" && strings.EqualFold(u.Host, s.host))
}

// cleanPath collapses repeated slashes and resolves dot segments, keeping a rooted path.
func cleanPath(p string) string {
	return path.Clean("/" + p)
}

// onboardingOpenPath lists the API an invitee may use before finishing setup: their own
// account (/api/me and everything under it: profile, prefs, calendar, onboarding), the club
// time zone the app shell reads, and health. Everything else answers 403 onboarding_required.
func onboardingOpenPath(method, p string) bool {
	switch {
	case p == "/api/me", strings.HasPrefix(p, "/api/me/"), p == "/api/health":
		return true
	case p == "/api/club/settings":
		return method == http.MethodGet || method == http.MethodHead
	}
	return false
}

// onboardingPending reports whether the user came in through an invite (or was created
// by an admin) and hasn't finished setup yet.
func (s *Server) onboardingPending(ctx context.Context, userID string) (bool, error) {
	inv, err := s.db.Read().InviteForUser(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return inv.OnboardedAt == nil, nil
}
