package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/files"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/purchases"
	"officerdashboard/internal/seed"
	"officerdashboard/internal/store"
)

// testNow is the fixed clock of the integration tests: a Saturday inside the open October
// 2026 period of the seeded FY2027, so settlement postings land in an open period.
var testNow = time.Date(2026, 10, 31, 18, 0, 0, 0, time.UTC)

type testEnv struct {
	t        testing.TB
	db       *store.DB
	srv      *Server
	h        http.Handler
	webhooks *webhookLog
	clock    time.Time
	cfg      Config
}

type webhookLog struct {
	mu   sync.Mutex
	list []map[string]any
}

func (w *webhookLog) add(m map[string]any) { w.mu.Lock(); w.list = append(w.list, m); w.mu.Unlock() }
func (w *webhookLog) all() []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]map[string]any(nil), w.list...)
}

func newEnv(t testing.TB, mutate ...func(*Config)) *testEnv {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := seed.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	hooks := &webhookLog{}
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &m)
		hooks.add(m)
		w.WriteHeader(204)
	}))
	t.Cleanup(discord.Close)
	e := &testEnv{t: t, db: db, webhooks: hooks, clock: testNow}
	e.cfg = Config{
		DB: db, Files: files.Local{Root: t.TempDir() + "/receipts"}, Notifier: notify.NewDiscord(discord.URL),
		Now: func() time.Time { return e.clock }, Tolerance: purchases.DefaultTolerance, AppURL: "http://localhost:8090", Version: "test",
	}
	for _, m := range mutate {
		m(&e.cfg)
	}
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
	return e
}

// login creates a session row for a seeded user and returns the cookie to send.
func (e *testEnv) login(userID string) *http.Cookie {
	e.t.Helper()
	token := auth.RandomToken(32)
	err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.InsertSession(context.Background(), auth.SHA256Hex(token), userID, e.clock.Add(24*time.Hour).UnixMilli(), "test", e.clock.UnixMilli())
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return &http.Cookie{Name: e.srv.sessionCookieName(), Value: token}
}

type actor struct {
	cookie  *http.Cookie
	token   string
	cookies []*http.Cookie
	headers map[string]string
}

func (e *testEnv) as(userID string) actor { return actor{cookie: e.login(userID)} }
func (e *testEnv) director() actor        { return e.as("user-director") }
func (e *testEnv) officer() actor         { return e.as("user-priya") }

func bearer(token string) actor { return actor{token: token} }

var anon = actor{}

type reply struct {
	Status int
	Header http.Header
	Raw    []byte
	Body   map[string]any
}

// path reads a nested JSON value: r.at("request", "journalEntryIds", 0).
func (r reply) at(keys ...any) any {
	var cur any = r.Body
	for _, k := range keys {
		switch kk := k.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = m[kk]
		case int:
			a, ok := cur.([]any)
			if !ok || kk >= len(a) {
				return nil
			}
			cur = a[kk]
		}
	}
	return cur
}

func (r reply) str(keys ...any) string { s, _ := r.at(keys...).(string); return s }
func (r reply) num(keys ...any) int64 {
	f, _ := r.at(keys...).(float64)
	return int64(f)
}
func (r reply) list(keys ...any) []any { l, _ := r.at(keys...).([]any); return l }

type formBody struct {
	fields map[string]string
	files  []formFile
}
type formFile struct{ field, name, mime, content string }

func (e *testEnv) do(method, path string, body any, who actor) reply {
	e.t.Helper()
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case formBody:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range b.fields {
			_ = mw.WriteField(k, v)
		}
		for _, f := range b.files {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", `form-data; name="`+f.field+`"; filename="`+f.name+`"`)
			h.Set("Content-Type", f.mime)
			p, _ := mw.CreatePart(h)
			_, _ = p.Write([]byte(f.content))
		}
		_ = mw.Close()
		rd, ctype = &buf, mw.FormDataContentType()
	case string:
		rd, ctype = strings.NewReader(b), "application/json"
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd, ctype = bytes.NewReader(raw), "application/json"
	}
	req := httptest.NewRequest(method, "http://localhost:8090"+path, rd)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if who.cookie != nil {
		req.AddCookie(who.cookie)
	}
	for _, c := range who.cookies {
		req.AddCookie(c)
	}
	for k, v := range who.headers {
		req.Header.Set(k, v)
	}
	if who.token != "" {
		req.Header.Set("Authorization", "Bearer "+who.token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	res := rec.Result()
	raw, _ := io.ReadAll(res.Body)
	r := reply{Status: res.StatusCode, Header: res.Header, Raw: raw}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(raw, &r.Body)
	}
	if r.Status >= 500 {
		e.t.Logf("%s %s → %d %s", method, path, r.Status, raw)
	}
	return r
}

func (e *testEnv) get(path string, who actor) reply { return e.do("GET", path, nil, who) }
func (e *testEnv) post(path string, body any, who actor) reply {
	return e.do("POST", path, body, who)
}

func (e *testEnv) scalar(q string, args ...any) any {
	e.t.Helper()
	var v any
	if err := e.db.R.QueryRow(q, args...).Scan(&v); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

var purchase = map[string]any{
	"type": "club_purchase", "title": "API test", "vendor": "Test vendor", "quantity": 2, "unitCostCents": 5000, "taxShipCents": 0,
	"expenseAccountId": "acct-5100", "fundId": "fund-general", "functionalClass": "program", "justification": "Testing lifecycle",
}

func with(base map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func (e *testEnv) mustOK(r reply) reply {
	e.t.Helper()
	if r.Status != 200 {
		e.t.Fatalf("status %d: %s", r.Status, r.Raw)
	}
	return r
}

func expectStatus(t *testing.T, label string, r reply, want int) {
	t.Helper()
	if r.Status != want {
		t.Errorf("%s: status %d want %d (%s)", label, r.Status, want, r.Raw)
	}
}
