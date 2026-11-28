// Package cfwrite is the only code in `dashboard setup` that can change a Cloudflare
// account. It is internal to cfprovision, so nothing else can import it: discovery,
// the plan and the command depend on cfapi.Reader, which can only GET.
//
// The client sends POST and PUT and nothing else. It has no method for any other verb,
// and its transport refuses any other verb, so a DELETE (or a PATCH, which no create in
// this version needs) cannot leave the process. Writes are never retried: a POST that
// failed with a 5xx may still have created the resource, and the next setup run finds
// it by reading the account first.
//
// Errors are *cfapi.Error values, so cfapi.IsForbidden and cfapi.HasCode work on them,
// and they never contain the token or the request body.
package cfwrite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"officerdashboard/internal/setup/cfapi"
)

// Client writes to the Cloudflare API. Its fields are unexported so the token cannot be
// read from outside, and String hides it.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another API root, such as an httptest server.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// New returns a write client authenticated with an API token.
func New(token string, opts ...Option) *Client {
	c := &Client{baseURL: cfapi.DefaultBaseURL, token: token}
	for _, o := range opts {
		o(c)
	}
	c.http = &http.Client{
		Timeout:   60 * time.Second,
		Transport: writeOnlyTransport{next: http.DefaultTransport},
		// Never follow a redirect: the Authorization header must only go to the API root,
		// and a redirected POST could be replayed somewhere else.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c
}

func (c *Client) String() string   { return "cfwrite.Client{" + c.baseURL + "}" }
func (c *Client) GoString() string { return c.String() }

// allowed are the only methods the transport lets through.
var allowed = map[string]bool{http.MethodPost: true, http.MethodPut: true}

// ErrMethodRefused is returned by the transport for any method other than POST and PUT.
var ErrMethodRefused = errors.New("cloudflare: setup write client sends only POST and PUT; refusing the request")

type writeOnlyTransport struct{ next http.RoundTripper }

func (t writeOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !allowed[r.Method] {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, ErrMethodRefused
	}
	return t.next.RoundTrip(r)
}

// Post sends body as JSON and decodes the envelope's "result" into out (nil to ignore it;
// a *json.RawMessage keeps it raw).
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.send(ctx, http.MethodPost, path, body, out)
}

// Put sends body as JSON and decodes the envelope's "result" into out.
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.send(ctx, http.MethodPut, path, body, out)
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

func (c *Client) send(ctx context.Context, method, path string, body, out any) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("cloudflare: path %q must start with /", path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: cannot encode the request body", method, path)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: invalid request", method, path)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "officer-dashboard-setup")
	res, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrMethodRefused) {
			return ErrMethodRefused
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("cloudflare: %s %s: %w (the request may or may not have reached Cloudflare)", method, path, ctxErr)
		}
		// Do not wrap err: it holds the full URL and transport detail.
		return fmt.Errorf("cloudflare: %s %s: request failed (it may or may not have reached Cloudflare)", method, path)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: reading the response failed (the request may have taken effect)", method, path)
	}
	env := &envelope{}
	jsonErr := json.Unmarshal(data, env)
	if res.StatusCode < 200 || res.StatusCode >= 300 || jsonErr != nil || !env.Success {
		e := &cfapi.Error{Method: method, Path: path}
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
		return e
	}
	if out == nil || len(env.Result) == 0 || string(env.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("cloudflare: %s %s: unexpected result shape: %w", method, path, err)
	}
	return nil
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
