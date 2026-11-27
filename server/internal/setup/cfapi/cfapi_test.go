package cfapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const secretToken = "tok_SUPER-secret-value-123"

func server(t *testing.T, h http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var methods []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s, &methods
}

func TestClientMethodSetIsReadOnly(t *testing.T) {
	// Adding a method to Client (Post, Do, Delete…) must be a deliberate change that
	// updates this list: discovery has to stay structurally unable to write.
	var got []string
	ty := reflect.TypeOf(&Client{})
	for i := 0; i < ty.NumMethod(); i++ {
		got = append(got, ty.Method(i).Name)
	}
	sort.Strings(got)
	want := []string{"Get", "GoString", "List", "String"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Client methods = %v, want %v", got, want)
	}
	ri := reflect.TypeOf((*Reader)(nil)).Elem()
	got = nil
	for i := 0; i < ri.NumMethod(); i++ {
		got = append(got, ri.Method(i).Name)
	}
	if !reflect.DeepEqual(got, []string{"Get", "List"}) {
		t.Fatalf("Reader methods = %v", got)
	}
}

func TestTransportRefusesNonGET(t *testing.T) {
	s, methods := server(t, func(w http.ResponseWriter, r *http.Request) {})
	c := New(secretToken, WithBaseURL(s.URL))
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(m, s.URL+"/zones", strings.NewReader("{}"))
		if _, err := c.http.Do(req); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("%s: err = %v, want read-only refusal", m, err)
		}
	}
	if len(*methods) != 0 {
		t.Fatalf("server saw %v", *methods)
	}
}

func TestGetDecodesResultAndSendsToken(t *testing.T) {
	s, methods := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secretToken {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/accounts/a1/access/organizations" {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"success":false,"errors":[{"code":7003,"message":"not found"}]}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"errors":[],"result":{"auth_domain":"club.cloudflareaccess.com"}}`)
	})
	c := New(secretToken, WithBaseURL(s.URL))
	var org struct {
		AuthDomain string `json:"auth_domain"`
	}
	if err := c.Get(context.Background(), "/accounts/a1/access/organizations", nil, &org); err != nil {
		t.Fatal(err)
	}
	if org.AuthDomain != "club.cloudflareaccess.com" {
		t.Fatalf("got %+v", org)
	}
	err := c.Get(context.Background(), "/nope", nil, &org)
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
	if !strings.Contains(err.Error(), "[7003] not found") {
		t.Fatalf("err = %q", err)
	}
	for _, m := range *methods {
		if m != http.MethodGet {
			t.Fatalf("sent %s", m)
		}
	}
}

func TestListFollowsPages(t *testing.T) {
	// 7 items, 3 per page: result_info with total_pages, with only total_count, and
	// with only count/per_page must all yield every item exactly once.
	for _, style := range []string{"total_pages", "total_count", "count_only"} {
		t.Run(style, func(t *testing.T) {
			s, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if r.URL.Query().Get("name") != "x" {
					t.Errorf("query lost: %v", r.URL.Query())
				}
				per := 3
				var items []int
				for i := (page-1)*per + 1; i <= page*per && i <= 7; i++ {
					items = append(items, i)
				}
				info := map[string]int{"page": page, "per_page": per, "count": len(items)}
				switch style {
				case "total_pages":
					info["total_pages"] = 3
				case "total_count":
					info["total_count"] = 7
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": items, "result_info": info})
			})
			got, err := ListAll[int](context.Background(), New(secretToken, WithBaseURL(s.URL)), "/things", url.Values{"name": {"x"}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5, 6, 7}) {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestListWithoutResultInfoIsOnePage(t *testing.T) {
	calls := 0
	s, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"success":true,"result":[{"id":"dashboard-backup-sink"}]}`)
	})
	got, err := ListAll[struct{ ID string }](context.Background(), New(secretToken, WithBaseURL(s.URL)), "/accounts/a/workers/scripts", nil)
	if err != nil || len(got) != 1 || calls != 1 {
		t.Fatalf("got %v, %v after %d calls", got, err, calls)
	}
}

func TestErrorsNeverContainTheToken(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"403 echoing token": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			fmt.Fprintf(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error for %s"}]}`, r.Header.Get("Authorization"))
		},
		"500 html": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(502)
			fmt.Fprint(w, "<html>bad gateway "+secretToken+"</html>")
		},
		"200 not json": func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "nope "+secretToken) },
		"success false": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"success":false,"errors":[{"code":1,"message":"%s"}]}`, secretToken)
		},
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.invalid/?t="+secretToken, http.StatusFound)
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			s, methods := server(t, h)
			err := New(secretToken, WithBaseURL(s.URL)).Get(context.Background(), "/zones", nil, &struct{}{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), secretToken) {
				t.Fatalf("token leaked: %v", err)
			}
			if len(*methods) != 1 {
				t.Fatalf("requests = %v (redirects must not be followed)", *methods)
			}
		})
	}
	t.Run("transport failure", func(t *testing.T) {
		s := httptest.NewServer(http.NotFoundHandler())
		s.Close()
		err := New(secretToken, WithBaseURL(s.URL)).Get(context.Background(), "/zones", nil, nil)
		if err == nil || strings.Contains(err.Error(), secretToken) {
			t.Fatalf("err = %v", err)
		}
	})
	c := New(secretToken)
	if s := fmt.Sprintf("%v %+v %#v %s", c, c, c, c); strings.Contains(s, secretToken) {
		t.Fatalf("formatting the client leaks the token: %s", s)
	}
}

func TestForbiddenIsRecognised(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
	})
	err := New(secretToken, WithBaseURL(s.URL)).List(context.Background(), "/zones", nil, func(json.RawMessage) error { return nil })
	if !IsForbidden(err) || IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
}
