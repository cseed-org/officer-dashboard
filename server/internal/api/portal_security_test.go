package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// pausedMutation holds a JSON body read after the handler's initial portal/director
// authentication, commits the competing change, then lets the mutation continue.
func pausedMutation(t *testing.T, e *testEnv, method, path string, body any, who actor, competingChange func()) int {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	bodyReader := &gatedReadCloser{data: raw, entered: entered, release: release}
	req := httptest.NewRequest(method, "http://localhost:8090"+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if who.cookie != nil {
		req.AddCookie(who.cookie)
	}
	for _, cookie := range who.cookies {
		req.AddCookie(cookie)
	}
	for k, v := range who.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.h.ServeHTTP(rec, req)
	}()
	<-entered
	released := false
	defer func() {
		if !released {
			close(release)
			<-done
		}
	}()
	competingChange()
	close(release)
	released = true
	<-done
	return rec.Code
}

type gatedReadCloser struct {
	once    sync.Once
	data    []byte
	entered chan struct{}
	release chan struct{}
}

func (r *gatedReadCloser) Read(p []byte) (int, error) {
	r.once.Do(func() {
		close(r.entered)
		<-r.release
	})
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *gatedReadCloser) Close() error { return nil }

func TestPortalProfileRequiresLiveSessionAtCommit(t *testing.T) {
	e := newEnv(t)
	builder := e.portalLogin("lp-account-alex")
	before := e.scalar(`SELECT name FROM buildspace_launchpad_people WHERE id='bs-person-01'`)
	status := pausedMutation(t, e, http.MethodPatch, "/api/portal/builder/profile", map[string]any{
		"name": "Changed after logout", "pronouns": "", "designation": "Founder", "bio": "",
	}, builder, func() {
		e.exec(`DELETE FROM buildspace_launchpad_sessions WHERE account_id='lp-account-alex'`)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("profile after logout = %d, want 401", status)
	}
	if got := e.scalar(`SELECT name FROM buildspace_launchpad_people WHERE id='bs-person-01'`); got != before {
		t.Fatalf("profile changed from %v to %v after logout", before, got)
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='buildspace_launchpad.portal.profile_saved'`).(int64); n != 0 {
		t.Fatalf("profile after logout wrote %d audit rows", n)
	}
}

func TestPortalProfileAndProjectRejectNewlyArchivedBuilder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		body   map[string]any
		target string
		action string
	}{
		{"profile", "/api/portal/builder/profile", map[string]any{"name": "Archived edit", "pronouns": "", "designation": "Founder", "bio": ""}, `SELECT name FROM buildspace_launchpad_people WHERE id='bs-person-01'`, "buildspace_launchpad.portal.profile_saved"},
		{"project", "/api/portal/builder/project", map[string]any{"name": "Archived edit", "description": "", "track": "Software", "stage": "Building", "links": []string{}}, `SELECT name FROM buildspace_launchpad_projects WHERE id='bs-moss'`, "buildspace_launchpad.portal.project_saved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			builder := e.portalLogin("lp-account-alex")
			before := e.scalar(tc.target)
			status := pausedMutation(t, e, http.MethodPatch, tc.path, tc.body, builder, func() {
				e.exec(`UPDATE buildspace_launchpad_people SET archived_at=1 WHERE id='bs-person-01'`)
			})
			if status != http.StatusForbidden {
				t.Fatalf("%s after archive = %d, want 403", tc.name, status)
			}
			if got := e.scalar(tc.target); got != before {
				t.Fatalf("%s changed from %v to %v after archive", tc.name, before, got)
			}
			if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action=?`, tc.action).(int64); n != 0 {
				t.Fatalf("%s after archive wrote %d audit rows", tc.name, n)
			}
		})
	}
}

func TestPortalBuilderActivityRequiresLiveSessionAtCommit(t *testing.T) {
	e := newEnv(t)
	builder := e.portalLogin("lp-account-alex")
	body := map[string]any{"kind": "weekly", "date": "2026-10-31", "body": "Written after disable", "status": "on-track"}
	status := pausedMutation(t, e, http.MethodPost, "/api/portal/builder/activity", body, builder, func() {
		e.exec(`UPDATE buildspace_launchpad_accounts SET status='disabled' WHERE id='lp-account-alex'`)
		e.exec(`DELETE FROM buildspace_launchpad_sessions WHERE account_id='lp-account-alex'`)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("builder activity after disable = %d, want 401", status)
	}
	if n := e.scalar(`SELECT count(*) FROM buildspace_launchpad_activity WHERE body='Written after disable'`).(int64); n != 0 {
		t.Fatal("builder activity committed after disable")
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='buildspace_launchpad.activity.created' AND after_json LIKE '%Written after disable%'`).(int64); n != 0 {
		t.Fatal("builder activity after disable was audited as successful")
	}
}

func TestPortalOnboardingRequiresActiveAccountAndSessionAtCommit(t *testing.T) {
	e := newEnv(t)
	builder := e.activate("lp-invite-sky", "300000000000000010")
	body := map[string]any{"name": "Sky Morales", "pronouns": "", "designation": "Founder", "bio": "", "project": map[string]any{"mode": "join", "projectId": "bs-sprout"}}
	status := pausedMutation(t, e, http.MethodPost, "/api/portal/builder/onboarding", body, builder, func() {
		e.exec(`UPDATE buildspace_launchpad_accounts SET status='disabled' WHERE id='lp-invite-sky'`)
		e.exec(`DELETE FROM buildspace_launchpad_sessions WHERE account_id='lp-invite-sky'`)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("onboarding after disable = %d, want 401", status)
	}
	if got := e.scalar(`SELECT count(*) FROM buildspace_launchpad_accounts WHERE id='lp-invite-sky' AND person_id IS NULL AND onboarded_at IS NULL`); got != int64(1) {
		t.Fatal("disabled account finished onboarding")
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='buildspace_launchpad.portal.onboarded' AND entity_type='buildspace_launchpad_person'`).(int64); n != 0 {
		t.Fatal("onboarding after disable wrote an audit row")
	}
}

func TestPortalMentorActivityRequiresActiveAccountAndSessionAtCommit(t *testing.T) {
	e := newEnv(t)
	mentor := e.portalLogin("lp-account-maya")
	body := map[string]any{"projectId": "bs-moss", "personId": "bs-person-02", "date": "2026-10-31", "body": "Mentor note after disable", "status": "on-track", "visibility": "team"}
	status := pausedMutation(t, e, http.MethodPost, "/api/portal/mentor/activity", body, mentor, func() {
		e.exec(`UPDATE buildspace_launchpad_accounts SET status='disabled' WHERE id='lp-account-maya'`)
		e.exec(`DELETE FROM buildspace_launchpad_sessions WHERE account_id='lp-account-maya'`)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("mentor activity after disable = %d, want 401", status)
	}
	if n := e.scalar(`SELECT count(*) FROM buildspace_launchpad_activity WHERE body='Mentor note after disable'`).(int64); n != 0 {
		t.Fatal("mentor activity committed after disable")
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='buildspace_launchpad.activity.created' AND after_json LIKE '%Mentor note after disable%'`).(int64); n != 0 {
		t.Fatal("mentor activity after disable was audited as successful")
	}
}
