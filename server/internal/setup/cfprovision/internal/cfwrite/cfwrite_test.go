package cfwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"officerdashboard/internal/setup/cfapi"
)

const secretToken = "tok_WRITE-secret-value-456"

func server(t *testing.T, h http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s, &seen
}

func TestMethodSetHasNoDelete(t *testing.T) {
	// A new method (Delete, Patch, Do…) must be a deliberate change that updates this
	// list. The brief for this version allows POST and PUT only.
	var got []string
	ty := reflect.TypeOf(&Client{})
	for i := 0; i < ty.NumMethod(); i++ {
		got = append(got, ty.Method(i).Name)
	}
	sort.Strings(got)
	if want := []string{"GoString", "Post", "Put", "String"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Client methods = %v, want %v", got, want)
	}
}

func TestTransportRefusesEverythingButPostAndPut(t *testing.T) {
	s, seen := server(t, func(w http.ResponseWriter, r *http.Request) {})
	c := New(secretToken, WithBaseURL(s.URL))
	for _, m := range []string{http.MethodDelete, http.MethodPatch, http.MethodGet, http.MethodHead, "PURGE"} {
		req, _ := http.NewRequest(m, s.URL+"/zones", strings.NewReader("{}"))
		if _, err := c.http.Do(req); err == nil || !strings.Contains(err.Error(), "only POST and PUT") {
			t.Fatalf("%s: err = %v, want refusal", m, err)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestPostSendsJSONAndDecodesResult(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secretToken || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		fmt.Fprintf(w, `{"success":true,"errors":[],"result":{"id":"g-1","name":%q,"unknown_field":[1,2]}}`, body["name"])
	})
	var out struct{ ID, Name string }
	if err := New(secretToken, WithBaseURL(s.URL)).Post(context.Background(), "/accounts/a/access/groups", map[string]any{"name": "Officers"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "g-1" || out.Name != "Officers" {
		t.Fatalf("got %+v", out)
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	for _, status := range []int{500, 502, 503, 429} {
		s, seen := server(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		err := New(secretToken, WithBaseURL(s.URL)).Post(context.Background(), "/accounts/a/cfd_tunnel", map[string]any{}, nil)
		if err == nil || len(*seen) != 1 {
			t.Fatalf("HTTP %d: %d requests, err %v", status, len(*seen), err)
		}
	}
}

func TestPermissionErrorIsRecognised(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":1001,"message":"Not authorized"}]}`)
	})
	err := New(secretToken, WithBaseURL(s.URL)).Put(context.Background(), "/accounts/a/access/organizations", map[string]any{}, nil)
	if !cfapi.IsForbidden(err) || !cfapi.HasCode(err, 1001) || cfapi.IsInvalidToken(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorsNeverContainTheTokenOrBody(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"echo header": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"success":false,"errors":[{"code":1,"message":"bad %s"}]}`, r.Header.Get("Authorization"))
		},
		"html 502": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(502)
			b, _ := io.ReadAll(r.Body)
			fmt.Fprintf(w, "<html>%s %s</html>", secretToken, b)
		},
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.invalid/?t="+secretToken, http.StatusTemporaryRedirect)
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			s, seen := server(t, h)
			err := New(secretToken, WithBaseURL(s.URL)).Post(context.Background(), "/zones/z/dns_records", map[string]any{"comment": "BODY-MARKER"}, nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			text := fmt.Sprintf("%v %+v %#v", err, err, err)
			if strings.Contains(text, secretToken) || strings.Contains(text, "BODY-MARKER") {
				t.Fatalf("leak: %s", text)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests %v (redirects must not be followed)", *seen)
			}
		})
	}
	c := New(secretToken)
	if s := fmt.Sprintf("%v %+v %#v %s", c, c, c, c); strings.Contains(s, secretToken) {
		t.Fatalf("formatting the client leaks the token: %s", s)
	}
}
