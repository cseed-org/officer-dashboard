package api

import (
	"net/http"
	"testing"
)

func pausedDirectorRequest(t *testing.T, e *testEnv, method, path string, body any, competingChange func()) reply {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	oldHook := directorWriteHook
	directorWriteHook = func() {
		close(entered)
		<-release
	}
	t.Cleanup(func() { directorWriteHook = oldHook })
	result := make(chan reply, 1)
	go func() { result <- e.do(method, path, body, e.director()) }()
	<-entered
	released := false
	defer func() {
		if !released {
			close(release)
			<-result
		}
	}()
	competingChange()
	close(release)
	released = true
	r := <-result
	directorWriteHook = oldHook
	return r
}

func demoteDirector(e *testEnv) {
	e.exec(`UPDATE users SET role='officer' WHERE id='user-director'`)
}

func TestAdminUserUpdateReloadsDirectorAuthority(t *testing.T) {
	e := newEnv(t)
	before := e.scalar(`SELECT name FROM users WHERE id='user-priya'`)
	r := pausedDirectorRequest(t, e, http.MethodPatch, "/api/admin/users/user-priya", map[string]any{"name": "Stale director edit"}, func() {
		demoteDirector(e)
	})
	if r.Status != http.StatusForbidden {
		t.Fatalf("demoted director updated a user: %d %s", r.Status, r.Raw)
	}
	if got := e.scalar(`SELECT name FROM users WHERE id='user-priya'`); got != before {
		t.Fatalf("target changed from %v to %v", before, got)
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='user.updated' AND entity_id='user-priya'`).(int64); n != 0 {
		t.Fatalf("demoted director wrote %d success audits", n)
	}
}

func TestAdminTokenCreateReloadsActiveDirectorAuthority(t *testing.T) {
	e := newEnv(t)
	r := pausedDirectorRequest(t, e, http.MethodPost, "/api/admin/tokens", map[string]any{"name": "stale-authority-token", "scopes": []string{"ingest:luma"}}, func() {
		e.exec(`UPDATE users SET active=0 WHERE id='user-director'`)
	})
	if r.Status != http.StatusForbidden {
		t.Fatalf("deactivated director created a token: %d %s", r.Status, r.Raw)
	}
	if n := e.scalar(`SELECT count(*) FROM service_tokens WHERE name='stale-authority-token'`).(int64); n != 0 {
		t.Fatal("credential was created across the authority revocation boundary")
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='token.created' AND before_json IS NULL AND after_json LIKE '%stale-authority-token%'`).(int64); n != 0 {
		t.Fatal("deactivated director's token creation was audited as successful")
	}

	// Restoring authority permits a new request; it does not retroactively commit the old one.
	e.exec(`UPDATE users SET active=1, role='director' WHERE id='user-director'`)
	if n := e.scalar(`SELECT count(*) FROM service_tokens WHERE name='stale-authority-token'`).(int64); n != 0 {
		t.Fatal("stale token appeared after authority restoration")
	}
	e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "restored-authority-token", "scopes": []string{"ingest:luma"}}, e.director()))
	if n := e.scalar(`SELECT count(*) FROM service_tokens WHERE created_by='user-director' AND name IN ('stale-authority-token','restored-authority-token')`).(int64); n != 1 {
		t.Fatalf("credentials across revocation/restoration = %d, want only the new one", n)
	}
}

func TestAdminTokenRevokeReloadsDirectorAuthority(t *testing.T) {
	e := newEnv(t)
	director := e.director()
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "keep-token", "scopes": []string{"ingest:luma"}}, director))
	tokenID := issued.str("token", "id")
	r := pausedDirectorRequest(t, e, http.MethodDelete, "/api/admin/tokens/"+tokenID, nil, func() {
		demoteDirector(e)
	})
	if r.Status != http.StatusForbidden {
		t.Fatalf("demoted director revoked a token: %d %s", r.Status, r.Raw)
	}
	if got := e.scalar(`SELECT revoked_at IS NULL FROM service_tokens WHERE id=?`, tokenID); got != int64(1) {
		t.Fatal("token was revoked after caller demotion")
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='token.revoked' AND entity_id=?`, tokenID).(int64); n != 0 {
		t.Fatal("demoted director's token revocation was audited as successful")
	}
}

func TestInviteRevokeReloadsDirectorAuthority(t *testing.T) {
	e := newEnv(t)
	director := e.director()
	invite := e.mustOK(e.post("/api/invites", inviteBody("link"), director))
	inviteID := invite.str("invite", "id")
	r := pausedDirectorRequest(t, e, http.MethodDelete, "/api/invites/"+inviteID, nil, func() {
		demoteDirector(e)
	})
	if r.Status != http.StatusForbidden {
		t.Fatalf("demoted director revoked an invite: %d %s", r.Status, r.Raw)
	}
	if n := e.scalar(`SELECT count(*) FROM invites WHERE id=? AND accepted_at IS NULL`, inviteID).(int64); n != 1 {
		t.Fatal("pending invite was revoked after caller demotion")
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='invite.revoked' AND entity_id=?`, inviteID).(int64); n != 0 {
		t.Fatal("demoted director's invite revocation was audited as successful")
	}
}
