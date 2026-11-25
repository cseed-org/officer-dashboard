package api

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestLogoutRejectsCrossSiteWithoutSessionAndOversizedBody(t *testing.T) {
	e := newEnv(t)
	for _, headers := range []map[string]string{{"Origin": "https://evil.example"}, {"Sec-Fetch-Site": "cross-site"}} {
		r := e.post("/auth/logout", url.Values{}, actor{headers: headers})
		expectStatus(t, "cross-site logout", r, 403)
		if len(r.Header.Values("Set-Cookie")) != 0 {
			t.Fatal("refused logout cleared cookies")
		}
	}
	who := e.officer()
	expectStatus(t, "oversized logout", e.post("/auth/logout", url.Values{"next": {strings.Repeat("x", 8192)}}, who), 400)
	expectStatus(t, "oversized logout preserved session", e.get("/api/me", who), 200)
}

func TestOfficerUpdateReloadsDirectorAtCommit(t *testing.T) {
	e := newEnv(t)
	status := pausedMutation(t, e, "PATCH", "/api/officers/user-priya", map[string]any{"title": "Unauthorized"}, e.director(), func() { demoteDirector(e) })
	if status != 403 {
		t.Fatalf("stale director update = %d", status)
	}
}

func TestLegacyProfileCannotSelfAssignDesign(t *testing.T) {
	e := newEnv(t)
	e.exec(`DELETE FROM officer_profiles WHERE user_id='user-priya'`)
	expectStatus(t, "self-assigned design", e.do("PUT", "/api/me/profile", map[string]any{"phone": "2065550100", "birthday": "01-15", "subteamId": "team-design"}, e.officer()), 403)
	expectStatus(t, "ordinary team", e.do("PUT", "/api/me/profile", map[string]any{"phone": "2065550100", "birthday": "01-15", "subteamId": "team-events"}, e.officer()), 200)
}

func TestConsentLabelsSelfDeclaredNameAndDestination(t *testing.T) {
	e := newEnv(t)
	client := registerClient(t, e, "Trusted-looking name")
	_, challenge := pkcePair()
	r := e.get("/oauth/authorize?"+authorizeForm(client, challenge, "allow").Encode(), e.officer())
	expectStatus(t, "consent", r, 200)
	if !strings.Contains(string(r.Raw), "self-declared") || !strings.Contains(string(r.Raw), "127.0.0.1") {
		t.Fatal("consent must label unverified identity and destination")
	}
}

func TestOrgSyncStatusDoesNotExposeProviderError(t *testing.T) {
	e := newEnv(t)
	e.exec(`INSERT INTO org_google_account VALUES(1,'subject','account@example.invalid','refresh','access',0,'user-leadership')`)
	e.srv.recordOrgSync(errors.New("private@example.invalid credential=secret"))
	r := e.get("/api/settings/org-google/status", e.as("user-leadership"))
	expectStatus(t, "status", r, 200)
	if strings.Contains(string(r.Raw), "private@") || strings.Contains(string(r.Raw), "credential=") {
		t.Fatal("provider detail leaked")
	}
}

func TestRegistrationGlobalBudgetAndRefreshIsolation(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AuthRateLimit = 2 })
	body := map[string]any{"client_name": "Client", "redirect_uris": []string{"https://client.example/callback"}}
	for i := 0; i < 60; i++ {
		expectStatus(t, "distributed registration", e.post("/oauth/register", body, actor{remote: fmt.Sprintf("198.51.100.%d:1234", i+1)}), 201)
	}
	expectStatus(t, "global cap", e.post("/oauth/register", body, actor{remote: "203.0.113.1:1234"}), 429)
	other := newEnv(t, func(c *Config) { c.AuthRateLimit = 2 })
	for i := 0; i < 2; i++ {
		other.get("/auth/login", anon)
	}
	expectStatus(t, "refresh has own budget", other.post("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"invalid"}}, anon), 400)
	expectStatus(t, "code shares auth budget", other.post("/oauth/token", url.Values{"grant_type": {"authorization_code"}}, anon), 429)
}
