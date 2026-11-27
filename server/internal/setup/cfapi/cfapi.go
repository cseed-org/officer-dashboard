// Package cfapi is the thin Cloudflare REST client that `dashboard setup` uses.
//
// In this version it can only read: Reader has no method that takes an HTTP method,
// the Client builds every request with GET, and its transport refuses anything else.
// Discovery depends on Reader, never on *Client, so a fake can stand in for tests.
//
// It is separate from internal/cloudflare (the runtime analytics and D1 client) on
// purpose: the setup credential is a different, short-lived token with wider rights.
//
// Errors never contain the token: it travels only in the Authorization header, and any
// text Cloudflare returns is scrubbed of it before it reaches an error message.
package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Cloudflare API v4 root
// (https://developers.cloudflare.com/api/, "https://api.cloudflare.com/client/v4").
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// Reader is the read-only view of the Cloudflare API. Paths are relative to the API
// root and start with "/", for example "/zones" or "/accounts/<id>/cfd_tunnel".
type Reader interface {
	// Get fetches one object and decodes the envelope's "result" into out.
	Get(ctx context.Context, path string, query url.Values, out any) error
	// List fetches every page of a list endpoint and calls each once per element of
	// "result", in order. It stops at the first error each returns.
	List(ctx context.Context, path string, query url.Values, each func(json.RawMessage) error) error
}

// ListAll is List decoded into a slice of T.
func ListAll[T any](ctx context.Context, r Reader, path string, query url.Values) ([]T, error) {
	var out []T
	err := r.List(ctx, path, query, func(raw json.RawMessage) error {
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("cloudflare: GET %s: unexpected result shape: %w", path, err)
		}
		out = append(out, v)
		return nil
	})
	return out, err
}

// Error is a request Cloudflare answered with a failure, or an envelope with
// "success": false. Status is 0 when the failure was in the envelope only.
type Error struct {
	Method, Path string
	Status       int
	Codes        []int
	Messages     []string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cloudflare: %s %s", e.Method, e.Path)
	if e.Status != 0 {
		fmt.Fprintf(&b, ": HTTP %d", e.Status)
	}
	for i, m := range e.Messages {
		if i < len(e.Codes) && e.Codes[i] != 0 {
			fmt.Fprintf(&b, ": [%d] %s", e.Codes[i], m)
		} else {
			fmt.Fprintf(&b, ": %s", m)
		}
	}
	if e.Status == 0 && len(e.Messages) == 0 {
		b.WriteString(": request was not successful")
	}
	return b.String()
}

// IsNotFound reports whether err is a Cloudflare 404.
func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// IsForbidden reports whether err is a Cloudflare 401 or 403 for a valid token: the
// token lacks a permission. A live account answered a missing permission with 401 code
// 1001, and an invalid token with 401 code 1000, which IsInvalidToken reports instead.
func IsForbidden(err error) bool {
	return (statusIs(err, http.StatusForbidden) || statusIs(err, http.StatusUnauthorized)) && !IsInvalidToken(err)
}

// CodeInvalidToken is Cloudflare's error code for a token it does not accept.
const CodeInvalidToken = 1000

// IsInvalidToken reports whether Cloudflare rejected the token itself (code 1000).
func IsInvalidToken(err error) bool { return HasCode(err, CodeInvalidToken) }

// HasCode reports whether err is a Cloudflare error carrying code.
func HasCode(err error, code int) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	for _, c := range e.Codes {
		if c == code {
			return true
		}
	}
	return false
}

func statusIs(err error, status int) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == status
}

// Client is a Reader over HTTP. Its fields are unexported so the token cannot be
// set or read from outside, and String hides it from %v and %+v.
type Client struct {
	baseURL  string
	token    string
	http     *http.Client
	maxPages int
	retries  []time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another API root, such as an httptest server.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient replaces the HTTP client. Its transport is still wrapped so that
// only GET requests leave the process, and redirects are still refused.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithRetry sets the waits before each retry of a request that failed with a 5xx, a
// 429 or a transport error. The default is 0.5s, 1s, 2s; no waits means no retry.
// Retrying is safe because every request is a GET.
func WithRetry(waits ...time.Duration) Option { return func(c *Client) { c.retries = waits } }

// New returns a read-only client authenticated with an API token.
func New(token string, opts ...Option) *Client {
	c := &Client{baseURL: DefaultBaseURL, token: token, http: &http.Client{Timeout: 30 * time.Second}, maxPages: 200,
		retries: []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	h := *c.http
	h.Transport = readOnlyTransport{next: h.Transport}
	// Never follow a redirect: the Authorization header must only go to the API root.
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.http = &h
	return c
}

func (c *Client) String() string   { return "cfapi.Client{" + c.baseURL + "}" }
func (c *Client) GoString() string { return c.String() }

// ErrNotReadOnly is returned by the transport for any method other than GET.
var ErrNotReadOnly = errors.New("cloudflare: setup client is read-only; refusing a non-GET request")

type readOnlyTransport struct{ next http.RoundTripper }

func (t readOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, ErrNotReadOnly
	}
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}

// envelope is the v4 response wrapper (https://developers.cloudflare.com/api/:
// "success", "errors", "messages", "result", "result_info").
type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *struct {
		Page       int `json:"page"`
		PerPage    int `json:"per_page"`
		Count      int `json:"count"`
		TotalCount int `json:"total_count"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// Get implements Reader.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	env, err := c.get(ctx, path, query)
	if err != nil {
		return err
	}
	if out == nil || len(env.Result) == 0 || string(env.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("cloudflare: GET %s: unexpected result shape: %w", path, err)
	}
	return nil
}

// List implements Reader. It pages with page/per_page, which every list endpoint used
// by setup accepts. per_page 20 is inside every endpoint's documented range (zones allow
// 5-50, D1 10-10000) and small, because a live Access apps list answered 504 at 50. It
// stops when result_info says there are no more pages, or when there is no result_info.
func (c *Client) List(ctx context.Context, path string, query url.Values, each func(json.RawMessage) error) error {
	q := url.Values{}
	for k, v := range query {
		q[k] = append([]string(nil), v...)
	}
	if q.Get("per_page") == "" {
		q.Set("per_page", "20")
	}
	seen := 0
	for page := 1; ; page++ {
		if page > c.maxPages {
			return fmt.Errorf("cloudflare: GET %s: more than %d pages; refusing to continue", path, c.maxPages)
		}
		q.Set("page", strconv.Itoa(page))
		env, err := c.get(ctx, path, q)
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if len(env.Result) > 0 && string(env.Result) != "null" {
			if err := json.Unmarshal(env.Result, &items); err != nil {
				return fmt.Errorf("cloudflare: GET %s: expected a list result: %w", path, err)
			}
		}
		for _, it := range items {
			if err := each(it); err != nil {
				return err
			}
		}
		seen += len(items)
		ri := env.ResultInfo
		switch {
		case len(items) == 0 || ri == nil:
			return nil
		case ri.TotalPages > 0:
			if page >= ri.TotalPages {
				return nil
			}
		case ri.TotalCount > 0:
			if seen >= ri.TotalCount {
				return nil
			}
		default:
			// No totals (some endpoints return only count/page/per_page): continue
			// while pages come back full.
			if ri.PerPage == 0 || len(items) < ri.PerPage {
				return nil
			}
		}
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (*envelope, error) {
	for attempt := 0; ; attempt++ {
		env, retry, err := c.getOnce(ctx, path, query)
		if !retry || attempt >= len(c.retries) {
			return env, err
		}
		t := time.NewTimer(c.retries[attempt])
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, fmt.Errorf("cloudflare: GET %s: %w", path, ctx.Err())
		case <-t.C:
		}
	}
}

// getOnce sends one request. retry is true for a failure worth repeating: a 5xx, a 429
// or a transport error.
func (c *Client) getOnce(ctx context.Context, path string, query url.Values) (env *envelope, retry bool, err error) {
	if !strings.HasPrefix(path, "/") {
		return nil, false, fmt.Errorf("cloudflare: path %q must start with /", path)
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("cloudflare: GET %s: invalid request", path)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "officer-dashboard-setup")
	res, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrNotReadOnly) {
			return nil, false, ErrNotReadOnly
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, fmt.Errorf("cloudflare: GET %s: %w", path, ctxErr)
		}
		// Do not wrap err: it holds the full URL and transport detail; keep the message plain.
		return nil, true, fmt.Errorf("cloudflare: GET %s: request failed", path)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, true, fmt.Errorf("cloudflare: GET %s: reading the response failed", path)
	}
	env = &envelope{}
	jsonErr := json.Unmarshal(raw, env)
	if res.StatusCode < 200 || res.StatusCode >= 300 || jsonErr != nil || !env.Success {
		e := &Error{Method: http.MethodGet, Path: path}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			e.Status = res.StatusCode
		}
		if jsonErr == nil {
			for _, x := range env.Errors {
				e.Codes = append(e.Codes, x.Code)
				e.Messages = append(e.Messages, c.scrub(x.Message))
			}
		} else if e.Status == 0 {
			e.Messages = []string{"response was not valid JSON"}
		}
		return nil, e.Status >= 500 || e.Status == http.StatusTooManyRequests, e
	}
	return env, false, nil
}

func (c *Client) scrub(s string) string {
	if c.token != "" {
		s = strings.ReplaceAll(s, c.token, "[redacted]")
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
