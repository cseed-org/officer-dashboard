package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAuthURLCarriesPKCEChallenge(t *testing.T) {
	d := NewDiscord("cid", "secret", "https://app.example/auth/callback")
	u, err := url.Parse(d.AuthURL("state123", "verifier-abc"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	// RFC 7636 appendix B test vector.
	if got := Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Errorf("S256 challenge = %q", got)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != Challenge("verifier-abc") ||
		q.Get("state") != "state123" || q.Get("scope") != "identify" || q.Get("client_id") != "cid" ||
		q.Get("redirect_uri") != "https://app.example/auth/callback" || q.Get("response_type") != "code" {
		t.Errorf("query %v", q)
	}
}

func TestExchangeSendsVerifierAndReturnsProfile(t *testing.T) {
	var form url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		if u, p, ok := r.BasicAuth(); !ok || u != "cid" || p != "secret" {
			http.Error(w, "bad client auth", 401)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer"}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-1" {
			http.Error(w, "no", 401)
			return
		}
		_, _ = w.Write([]byte(`{"id":"100000000000000042","username":"jo","global_name":"Jo Lee","avatar":"abc"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	d := NewDiscord("cid", "secret", "https://app.example/auth/callback")
	d.TokenURL, d.ProfileURL = srv.URL+"/token", srv.URL+"/me"
	p, err := d.Exchange(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("code") != "code-1" || form.Get("code_verifier") != "verifier-1" || form.Get("grant_type") != "authorization_code" {
		t.Errorf("form %v", form)
	}
	if p.ID != "100000000000000042" || p.DisplayName() != "Jo Lee" || p.AvatarURL() != "https://cdn.discordapp.com/avatars/100000000000000042/abc.png?size=128" {
		t.Errorf("profile %+v", p)
	}
	d.TokenURL = srv.URL + "/missing"
	if _, err := d.Exchange(context.Background(), "x", "y"); err == nil {
		t.Error("a failed exchange must error")
	}
}
