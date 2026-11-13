// Package cloudflare integrates the account's GraphQL analytics and membership D1.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNotConnected = errors.New("Cloudflare is not connected")

// ErrRejected means Cloudflare definitively refused an operation (not a transport ambiguity).
var ErrRejected = errors.New("Cloudflare rejected the operation")

type Config struct{ Token, AccountID, ZoneID, DatabaseID string }

func FromEnv(get func(string) string) Config {
	return Config{get("CLOUDFLARE_API_TOKEN"), get("CLOUDFLARE_ACCOUNT_ID"), get("CLOUDFLARE_ZONE_ID"), get("D1_DATABASE_ID")}
}

type Client struct {
	Config  Config
	HTTP    *http.Client
	BaseURL string
}

func New(cfg Config) *Client {
	return &Client{Config: cfg, HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: "https://api.cloudflare.com/client/v4"}
}
func (c *Client) D1Connected() bool {
	return c != nil && c.Config.Token != "" && c.Config.AccountID != "" && c.Config.DatabaseID != ""
}
func (c *Client) AnalyticsConnected() bool {
	return c != nil && c.Config.Token != "" && c.Config.AccountID != ""
}
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("Cloudflare request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode >= 400 && res.StatusCode < 500 {
			return fmt.Errorf("%w: HTTP %d", ErrRejected, res.StatusCode)
		}
		return fmt.Errorf("Cloudflare returned HTTP %d", res.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out); err != nil {
		return errors.New("Cloudflare returned an invalid response")
	}
	return nil
}

type Result struct {
	Success bool              `json:"success"`
	Results []json.RawMessage `json:"results"`
	Meta    struct {
		Changes int `json:"changes"`
	} `json:"meta"`
}

func (c *Client) Query(ctx context.Context, sql string, params ...any) ([]Result, error) {
	if !c.D1Connected() {
		return nil, ErrNotConnected
	}
	if params == nil {
		params = []any{}
	}
	var out struct {
		Success bool     `json:"success"`
		Result  []Result `json:"result"`
	}
	err := c.post(ctx, "/accounts/"+url.PathEscape(c.Config.AccountID)+"/d1/database/"+url.PathEscape(c.Config.DatabaseID)+"/query", map[string]any{"sql": sql, "params": params}, &out)
	if err != nil {
		return nil, err
	}
	if !out.Success || len(out.Result) == 0 {
		return nil, fmt.Errorf("%w: D1 query failed", ErrRejected)
	}
	for _, r := range out.Result {
		if !r.Success {
			return nil, fmt.Errorf("%w: D1 statement failed", ErrRejected)
		}
	}
	return out.Result, nil
}

// Person mirrors the source schema exactly; nullable source columns stay nullable.
type Person struct {
	ID             string  `json:"person_id"`
	UWEmail        *string `json:"uw_email"`
	FirstName      string  `json:"first_name"`
	LastName       string  `json:"last_name"`
	PreferredName  *string `json:"preferred_name"`
	Major          *string `json:"major"`
	GradYear       *string `json:"grad_year"`
	Pronouns       *string `json:"pronouns"`
	Demographics   *string `json:"demographics"`
	CSEmail        *string `json:"cs_email"`
	JoinDate       string  `json:"join_date"`
	Campus         *string `json:"campus"`
	PersonalEmail  *string `json:"personal_email"`
	Agreement      int     `json:"agreed_to_membership_agreement"`
	HasBeenOfficer int     `json:"has_been_cseed_officer"`
}
type Submission struct {
	ID         string `json:"submission_id"`
	PersonID   string `json:"person_id"`
	Payload    string `json:"payload_json"`
	ReceivedAt string `json:"received_at"`
}

// Pull pages by primary key: no offset scans and no API result-size truncation.
func (c *Client) Pull(ctx context.Context) ([]Person, []Submission, error) {
	people := []Person{}
	submissions := []Submission{}
	for _, table := range []string{"People", "Membership_submissions"} {
		key := "person_id"
		if table != "People" {
			key = "submission_id"
		}
		cursor := ""
		for {
			res, err := c.Query(ctx, "SELECT * FROM "+table+" WHERE "+key+" > ? ORDER BY "+key+" LIMIT 250", cursor)
			if err != nil {
				return nil, nil, err
			}
			rows := res[0].Results
			for _, raw := range rows {
				if table == "People" {
					var p Person
					if err = json.Unmarshal(raw, &p); err != nil {
						return nil, nil, err
					}
					if p.ID <= cursor {
						return nil, nil, errors.New("D1 returned unordered people")
					}
					cursor = p.ID
					people = append(people, p)
				} else {
					var s Submission
					if err = json.Unmarshal(raw, &s); err != nil {
						return nil, nil, err
					}
					if s.ID <= cursor {
						return nil, nil, errors.New("D1 returned unordered submissions")
					}
					cursor = s.ID
					submissions = append(submissions, s)
				}
			}
			if len(rows) < 250 {
				break
			}
		}
	}
	// A signup can appear between primary-key pages. Fetch missing parents before
	// replacing the local mirror, rather than failing the complete pull's FK check.
	known := map[string]bool{}
	for _, p := range people {
		known[p.ID] = true
	}
	for _, submission := range submissions {
		if known[submission.PersonID] {
			continue
		}
		res, err := c.Query(ctx, "SELECT * FROM People WHERE person_id=?", submission.PersonID)
		if err != nil {
			return nil, nil, err
		}
		if len(res[0].Results) != 1 {
			return nil, nil, errors.New("D1 submission parent missing; retry sync")
		}
		var p Person
		if err = json.Unmarshal(res[0].Results[0], &p); err != nil {
			return nil, nil, err
		}
		if p.ID != submission.PersonID {
			return nil, nil, errors.New("D1 returned mismatched submission parent")
		}
		people = append(people, p)
		known[p.ID] = true
	}
	return people, submissions, nil
}

// GraphQL returns the scoped dataset without exposing credentials or raw API errors.
func (c *Client) GraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	if !c.AnalyticsConnected() {
		return nil, ErrNotConnected
	}
	var out struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := c.post(ctx, "/graphql", map[string]any{"query": query, "variables": variables}, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 || len(out.Data) == 0 || string(out.Data) == "null" {
		return nil, errors.New("Cloudflare analytics query failed (check token permissions and dataset access)")
	}
	return out.Data, nil
}

// AnalyticsConnectedFor gates zone datasets separately from account datasets.
func (c *Client) AnalyticsConnectedFor(kind string) bool {
	return c.AnalyticsConnected() && (kind != "traffic-security" || c.Config.ZoneID != "")
}
