// Package cfwrite is the only code in `dashboard setup` that can change a Cloudflare
// account. It is internal to cfprovision, so nothing else can import it: discovery,
// the plan and the command depend on cfapi.Reader, which can only GET.
//
// The client sends POST and PUT and nothing else (PutModule is a PUT whose body is a Worker
// upload, multipart instead of JSON). It has no method for any other verb,
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
	"mime/multipart"
	"net/http"
	"net/textproto"
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

// Module is one file part of a Worker upload.
type Module struct {
	Name        string // the part and file name, which metadata main_module names
	ContentType string // "application/javascript+module" for an ES module
	Body        []byte
}

// PutModule uploads a Worker: one PUT of multipart/form-data with a JSON "metadata" part
// and one part per module ("use multipart/form-data with a JSON metadata part whose
// main_module field names the entrypoint file. Upload each module as a separate file
// part; JavaScript ES module parts use the application/javascript+module content type":
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/update/).
// The metadata can hold a secret binding, so, as for every write, no error ever includes
// the body.
func (c *Client) PutModule(ctx context.Context, path string, metadata any, modules []Module, out any) error {
	meta, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("cloudflare: PUT %s: cannot encode the metadata", path)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="metadata"`)
	h.Set("Content-Type", "application/json")
	part, err := mw.CreatePart(h)
	if err == nil {
		_, err = part.Write(meta)
	}
	for _, m := range modules {
		if err != nil {
			break
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, m.Name, m.Name))
		h.Set("Content-Type", m.ContentType)
		if part, err = mw.CreatePart(h); err == nil {
			_, err = part.Write(m.Body)
		}
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		return fmt.Errorf("cloudflare: PUT %s: cannot encode the upload", path)
	}
	return c.do(ctx, http.MethodPut, path, buf.Bytes(), mw.FormDataContentType(), out)
}

func (c *Client) send(ctx context.Context, method, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: cannot encode the request body", method, path)
	}
	return c.do(ctx, method, path, raw, "application/json", out)
}

func (c *Client) do(ctx context.Context, method, path string, raw []byte, contentType string, out any) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("cloudflare: path %q must start with /", path)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: invalid request", method, path)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)
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
