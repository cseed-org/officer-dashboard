package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRejectCloudflareErrors(t *testing.T) {
	for _, body := range []string{`{"success":false,"result":[]}`, `{"success":true,"result":[{"success":false}]}`, `{"success":true,"result":[]}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			c := New(Config{Token: "secret", AccountID: "a", DatabaseID: "d"})
			c.BaseURL = srv.URL
			if _, err := c.Query(context.Background(), "SELECT 1"); err == nil {
				t.Fatal("accepted failed response")
			}
		})
	}
}
func TestGraphQLErrorsAtHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"viewer":{}},"errors":[{"message":"permission"}]}`))
	}))
	defer srv.Close()
	c := New(Config{Token: "secret", AccountID: "a", ZoneID: "z"})
	c.BaseURL = srv.URL
	if _, err := c.GraphQL(context.Background(), "query{}", nil); err == nil {
		t.Fatal("accepted partial error response")
	}
}

func TestPullFetchesParentOfConcurrentSignup(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			SQL string `json:"sql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var rows any = []any{}
		if strings.Contains(body.SQL, "Membership_submissions") {
			rows = []Submission{{ID: "s1", PersonID: "new-person", Payload: "{}", ReceivedAt: "2026-11-01"}}
		}
		if strings.Contains(body.SQL, "WHERE person_id=?") {
			rows = []Person{{ID: "new-person", FirstName: "New", LastName: "Signup", JoinDate: "2026-11-01"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{map[string]any{"success": true, "results": rows}}})
	}))
	defer srv.Close()
	c := New(Config{Token: "secret", AccountID: "a", DatabaseID: "d"})
	c.BaseURL = srv.URL
	people, submissions, err := c.Pull(context.Background())
	if err != nil || len(people) != 1 || len(submissions) != 1 || calls != 3 {
		t.Fatalf("concurrent signup: %d %d %d %v", len(people), len(submissions), calls, err)
	}
}
