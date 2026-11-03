package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"officerdashboard/internal/web"
)

// M1: a director who files a request cannot pay it out to themselves. Settling
// (reimbursed / fulfilled) and closing their own request needs another director.
func TestRequesterCannotSettleOrCloseOwnRequest(t *testing.T) {
	e := newEnv(t)
	dir, lead := e.director(), e.as("user-leadership")
	made := e.mustOK(e.post("/api/requests", with(purchase, "type", "reimbursement", "submit", true), dir))
	id := made.str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, lead))

	mine := e.mustOK(e.get("/api/requests/"+id, dir))
	if got := mine.list("request", "allowedTransitions"); slices.Contains(got, any("reimbursed")) {
		t.Errorf("requester is offered reimbursed: %v", got)
	}
	if mine.at("request", "maxSettlementCents") != nil {
		t.Errorf("requester sees a settlement ceiling: %v", mine.at("request", "maxSettlementCents"))
	}
	expectStatus(t, "self reimburse", e.post("/api/requests/"+id+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 10000}, dir), 403)
	if s := e.scalar(`SELECT status FROM purchase_requests WHERE id = ?`, id); s != "approved" {
		t.Fatalf("status after refused self-settle: %v", s)
	}
	theirs := e.mustOK(e.get("/api/requests/"+id, lead))
	if got := theirs.list("request", "allowedTransitions"); !slices.Contains(got, any("reimbursed")) {
		t.Errorf("another director is not offered reimbursed: %v", got)
	}
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 10000}, lead))
	if got := e.mustOK(e.get("/api/requests/"+id, dir)).list("request", "allowedTransitions"); slices.Contains(got, any("closed")) {
		t.Errorf("requester is offered closed: %v", got)
	}
	expectStatus(t, "self close", e.post("/api/requests/"+id+"/transition", map[string]any{"to": "closed"}, dir), 403)
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "closed"}, lead))

	// Club purchases: the requester cannot mark their own purchase bought or close it.
	cp := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), dir)).str("request", "id")
	e.mustOK(e.post("/api/requests/"+cp+"/approve", map[string]any{}, lead))
	e.mustOK(e.post("/api/requests/"+cp+"/transition", map[string]any{"to": "fulfilling"}, dir))
	expectStatus(t, "self fulfil", e.post("/api/requests/"+cp+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9000}, dir), 403)
	e.mustOK(e.post("/api/requests/"+cp+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9000}, lead))
	expectStatus(t, "self close purchase", e.post("/api/requests/"+cp+"/transition", map[string]any{"to": "closed"}, dir), 403)
}

// M1: a director settles at most the approved estimate plus the agent tolerance; more
// than that needs leadership. The ceiling is shown to the director in the detail.
func TestDirectorSettlementIsCappedByTolerance(t *testing.T) {
	e := newEnv(t)
	dir, lead := e.director(), e.as("user-leadership")
	id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id") // 2 × $50
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, dir))
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, dir))
	if got := e.mustOK(e.get("/api/requests/"+id, dir)).num("request", "maxSettlementCents"); got != 12500 {
		t.Errorf("director ceiling %d, want 12500", got)
	}
	if v := e.mustOK(e.get("/api/requests/"+id, lead)).at("request", "maxSettlementCents"); v != nil {
		t.Errorf("leadership ceiling %v, want none", v)
	}
	over := e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 12501}, dir)
	expectStatus(t, "director over tolerance", over, 409)
	if over.str("error", "code") != "over_tolerance" {
		t.Errorf("error code %s", over.Raw)
	}
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 12501}, lead))

	id2 := e.mustOK(e.post("/api/requests", with(purchase, "type", "reimbursement", "submit", true), e.officer())).str("request", "id")
	e.mustOK(e.post("/api/requests/"+id2+"/approve", map[string]any{}, dir))
	e.mustOK(e.post("/api/requests/"+id2+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 12500}, dir))
}

// M2: a service token acts with its creator's authority, so it stops working when the
// creator is deactivated or demoted below director, and those changes revoke it outright.
func TestServiceTokenFollowsItsCreator(t *testing.T) {
	e := newEnv(t)
	lead := e.as("user-leadership")
	issue := func(userID string) (id, secret string) {
		t.Helper()
		e.mustOK(e.do("PATCH", "/api/admin/users/"+userID, map[string]any{"role": "director"}, lead))
		r := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "agent " + userID, "scopes": []string{"agent:jobs"}}, e.as(userID)))
		id, secret = r.str("token", "id"), r.str("plaintext")
		expectStatus(t, "fresh token", e.get("/api/agent/jobs", bearer(secret)), 200)
		return id, secret
	}

	demotedID, demoted := issue("user-marcus")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"role": "officer"}, lead))
	expectStatus(t, "creator demoted", e.get("/api/agent/jobs", bearer(demoted)), 401)
	if e.scalar(`SELECT revoked_at IS NOT NULL FROM service_tokens WHERE id = ?`, demotedID) != int64(1) {
		t.Error("demotion did not revoke the creator's token")
	}

	goneID, gone := issue("user-ava")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-ava", map[string]any{"active": false}, lead))
	expectStatus(t, "creator deactivated", e.get("/api/agent/jobs", bearer(gone)), 401)
	if e.scalar(`SELECT revoked_at IS NOT NULL FROM service_tokens WHERE id = ?`, goneID) != int64(1) {
		t.Error("deactivation did not revoke the creator's token")
	}

	// Defence in depth: even an unrevoked token is refused once its creator is not an
	// active director (e.g. a role changed outside the API).
	_, kept := issue("user-riley")
	if _, err := e.db.W.Exec(`UPDATE users SET active = 0 WHERE id = 'user-riley'`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, "inactive creator, unrevoked token", e.get("/api/agent/jobs", bearer(kept)), 401)
}

// Sessions slide while used, but never outlive 90 days from sign-in.
func TestSessionsHaveAnAbsoluteLifetime(t *testing.T) {
	e := newEnv(t)
	start := e.clock
	who := e.officer()
	e.mustOK(e.get("/api/me", who)) // renews the short test session to the full 30 days
	for day := 10; day < 90; day += 10 {
		e.clock = start.Add(time.Duration(day) * 24 * time.Hour)
		r := e.get("/api/me", who)
		expectStatus(t, fmt.Sprintf("day %d", day), r, 200)
		if ck := cookieNamed(setCookies(r), e.srv.sessionCookieName()); ck != nil && ck.Expires.After(start.Add(sessionMaxAge)) {
			t.Errorf("day %d: renewed past the absolute cap: %v", day, ck.Expires)
		}
	}
	e.clock = start.Add(sessionMaxAge)
	expectStatus(t, "day 90", e.get("/api/me", who), 401)
	if n := e.scalar(`SELECT count(*) FROM sessions WHERE user_id = 'user-priya' AND expires_at > ?`, start.Add(sessionMaxAge).UnixMilli()); n != int64(0) {
		t.Errorf("%v sessions outlive the cap", n)
	}
}

// Signing in again replaces the browser's previous session instead of leaving it alive.
func TestReLoginReplacesTheOldSession(t *testing.T) {
	e := discordEnv(t, `{"id":"100000000000000002","username":"priya"}`, nil)
	old := e.login("user-priya")
	cs, state := startLogin(t, e, "/")
	cb := e.get("/auth/callback?code=good-code&state="+state, actor{cookies: append(cs, old)})
	if cb.Status != 302 || cookieNamed(setCookies(cb), e.srv.sessionCookieName()) == nil {
		t.Fatalf("callback → %d", cb.Status)
	}
	expectStatus(t, "old session", e.get("/api/me", actor{cookie: old}), 401)
	if n := e.scalar(`SELECT count(*) FROM sessions WHERE user_id = 'user-priya'`); n != int64(1) {
		t.Errorf("%v sessions, want only the new one", n)
	}
}

func TestHealthShowsTheVersionOnlyToSignedInUsers(t *testing.T) {
	e := newEnv(t)
	anonHealth := e.mustOK(e.get("/api/health", anon))
	if anonHealth.at("version") != nil || anonHealth.at("ok") != true {
		t.Errorf("anonymous health %s", anonHealth.Raw)
	}
	if v := e.mustOK(e.get("/api/health", e.officer())).str("version"); v != "test" {
		t.Errorf("signed-in health version %q", v)
	}
}

// Receipts are financial records: the requester and directors may download them, other
// officers may not.
func TestReceiptDownloadIsLimitedToRequesterAndDirectors(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/requests", purchase, e.officer())).str("request", "id")
	att := e.do("POST", "/api/requests/"+id+"/attachments", formBody{
		fields: map[string]string{"kind": "receipt"}, files: []formFile{{"file", "r.pdf", "application/pdf", "%PDF-1.4\nR"}},
	}, e.officer())
	url := att.str("attachment", "url")
	expectStatus(t, "requester", e.get(url, e.officer()), 200)
	expectStatus(t, "director", e.get(url, e.director()), 200)
	expectStatus(t, "another officer", e.get(url, e.as("user-marcus")), 403)
}

// Owner changes answer with a clear message instead of a raw constraint error: a missing
// user, an inactive one, or the last owner. (Making a non-leadership user an owner promotes
// them to leadership on purpose; both UIs say so in the confirmation.)
func TestOwnerChangeErrorsAreFriendly(t *testing.T) {
	e := newEnv(t)
	owner := e.as("user-leadership")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"active": false}, owner))
	for _, c := range []struct {
		label, method, path string
		body                any
		status              int
		msg                 string
	}{
		{"unknown user", "POST", "/api/admin/owners/nope", nil, 404, "Record not found"},
		{"inactive user", "POST", "/api/admin/owners/user-marcus", nil, 400, "Owner must be active"},
		{"last owner", "DELETE", "/api/admin/owners/user-leadership", nil, 409, "Cannot remove the last owner"},
		{"transfer to inactive", "POST", "/api/admin/owners/transfer", map[string]any{"toUserId": "user-marcus"}, 400, "Owner must be active"},
	} {
		r := e.do(c.method, c.path, c.body, owner)
		expectStatus(t, c.label, r, c.status)
		if r.str("error", "message") != c.msg {
			t.Errorf("%s: message %s", c.label, r.Raw)
		}
	}
	promoted := e.mustOK(e.post("/api/admin/owners/user-priya", nil, owner))
	if promoted.str("user", "role") != "leadership" {
		t.Errorf("co-owner not promoted: %s", promoted.Raw)
	}
}

// M5: any officer may page through members, but reading past the first page is
// recorded in the audit log (bulk reads leave a trace; the first page does not).
func TestMemberPagingPastTheFirstPageIsAudited(t *testing.T) {
	e := newEnv(t)
	e.mustOK(e.get("/api/members?limit=50", e.officer()))
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action = 'members.paged'`); n != int64(0) {
		t.Errorf("first page audited: %v", n)
	}
	e.mustOK(e.get("/api/members?limit=50&offset=50&q=ada", e.officer()))
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action = 'members.paged' AND actor_user_id = 'user-priya'`); n != int64(1) {
		t.Errorf("paged read audits %v, want 1", n)
	}
	after := e.scalar(`SELECT after_json FROM audit_log WHERE action = 'members.paged'`)
	if !strings.Contains(fmt.Sprint(after), `"offset":50`) {
		t.Errorf("audit row %v", after)
	}
}

// Sign-in, dev login and upload/import routes are rate limited per client IP. Behind the
// local reverse proxy (a loopback peer) the client is the last X-Forwarded-For hop.
func TestAuthAndUploadRoutesAreRateLimitedPerIP(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AuthRateLimit, c.UploadRateLimit = 3, 2 })
	a := actor{remote: "198.51.100.7:5000"}
	for i := 0; i < 3; i++ {
		expectStatus(t, "within the limit", e.get("/auth/login", a), 503) // Discord not configured
	}
	r := e.get("/auth/login", a)
	expectStatus(t, "over the limit", r, 429)
	if r.Header.Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
	// A client-supplied X-Forwarded-For is ignored from a non-loopback peer.
	spoof := actor{remote: "198.51.100.7:5001", headers: map[string]string{"X-Forwarded-For": "203.0.113.9"}}
	expectStatus(t, "spoofed XFF", e.get("/auth/login", spoof), 429)
	expectStatus(t, "another client", e.get("/auth/login", actor{remote: "198.51.100.8:5000"}), 503)
	// Behind the proxy, each forwarded client has its own budget.
	viaProxy := func(ip string) actor {
		return actor{remote: "127.0.0.1:40000", headers: map[string]string{"X-Forwarded-For": "10.9.9.9, " + ip}}
	}
	for i := 0; i < 3; i++ {
		e.get("/auth/login", viaProxy("203.0.113.1"))
	}
	expectStatus(t, "proxied client over", e.get("/auth/login", viaProxy("203.0.113.1")), 429)
	expectStatus(t, "other proxied client", e.get("/auth/login", viaProxy("203.0.113.2")), 503)

	// Uploads: a separate, smaller budget; JSON 429 under /api.
	id := e.mustOK(e.post("/api/requests", purchase, e.officer())).str("request", "id")
	up := func() reply {
		who := e.officer()
		who.remote = "198.51.100.20:1"
		return e.do("POST", "/api/requests/"+id+"/attachments", formBody{files: []formFile{{"file", "r.pdf", "application/pdf", "%PDF-1.4\nR"}}}, who)
	}
	up()
	up()
	over := up()
	expectStatus(t, "uploads over the limit", over, 429)
	if over.str("error", "code") != "rate_limited" {
		t.Errorf("429 body %s", over.Raw)
	}
	// Ordinary API reads are not limited.
	for i := 0; i < 5; i++ {
		expectStatus(t, "reads", e.get("/api/requests/"+id, actor{cookie: e.login("user-priya"), remote: "198.51.100.20:1"}), 200)
	}
}

// M3 / CodeQL #1: every response is typed and nosniff. Pages get the front end's CSP with
// script hashes; everything else (JSON, files, errors, redirects) gets a CSP that allows
// nothing, so a response that is somehow rendered as a document cannot run script.
func TestEveryResponseIsTypedAndCarriesACSP(t *testing.T) {
	site := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><script>document.documentElement.dataset.x='1'</script><h1>home</h1>")},
		"404.html":   {Data: []byte("<h1>nope</h1>")},
	}
	e := newEnv(t, func(c *Config) { c.Static = web.Handler(site, web.Options{}) })
	home := e.get("/", anon)
	if csp := home.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' 'sha256-") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("home CSP %q", csp)
	}
	if home.Header.Get("Permissions-Policy") == "" {
		t.Error("home lacks Permissions-Policy")
	}
	for _, c := range []struct {
		label string
		r     reply
	}{
		{"api json", e.get("/api/health", anon)},
		{"api 404", e.get("/api/nope", anon)},
		{"api 401", e.get("/api/me", anon)},
		{"redirect", e.get("/speakers?x=1", anon)},
		{"auth 503", e.get("/auth/login", anon)},
		{"page 404", e.get("/nope", anon)},
	} {
		h := c.r.Header
		if h.Get("Content-Type") == "" && len(c.r.Raw) > 0 {
			t.Errorf("%s: body without Content-Type", c.label)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: headers %v", c.label, h)
		}
		if !strings.HasPrefix(h.Get("Content-Type"), "text/html") && !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("%s: non-page CSP %q", c.label, h.Get("Content-Security-Policy"))
		}
	}
	// A handler that forgets its Content-Type gets bytes, not a sniffed document.
	rec := httptest.NewRecorder()
	withContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><script>alert(1)</script>"))
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("untyped body served as %q", ct)
	}
	rec = httptest.NewRecorder()
	withContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("<html><script>alert(1)</script>"))
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("untyped body after WriteHeader served as %q", ct)
	}
	// The access log's writer does the same for the outermost layer.
	rec = httptest.NewRecorder()
	AccessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><script>alert(1)</script>"))
	}), slog.New(slog.NewTextHandler(io.Discard, nil))).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("access log passthrough served %q", ct)
	}
}
