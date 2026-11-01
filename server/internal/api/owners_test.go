package api

import "testing"

func TestOwnersRequireOwnershipAndProtectOwnerAccounts(t *testing.T) {
	e := newEnv(t)
	owner := e.as("user-leadership")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-ava", map[string]any{"role": "leadership"}, owner))
	nonowner := e.as("user-ava")
	for _, who := range []actor{e.officer(), e.director(), nonowner} {
		expectStatus(t, "add owner forbidden", e.post("/api/admin/owners/user-priya", nil, who), 403)
		expectStatus(t, "remove owner forbidden", e.do("DELETE", "/api/admin/owners/user-leadership", nil, who), 403)
		expectStatus(t, "transfer forbidden", e.post("/api/admin/owners/transfer", map[string]any{"toUserId": "user-priya"}, who), 403)
		expectStatus(t, "demote owner forbidden", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"role": "officer"}, who), 403)
		expectStatus(t, "deactivate owner forbidden", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"active": false}, who), 403)
	}
	expectStatus(t, "remove last owner", e.do("DELETE", "/api/admin/owners/user-leadership", nil, owner), 409)
	added := e.mustOK(e.post("/api/admin/owners/user-priya", nil, owner))
	if added.at("user", "isOwner") != true || added.str("user", "role") != "leadership" {
		t.Fatal(string(added.Raw))
	}
	e.mustOK(e.do("DELETE", "/api/admin/owners/user-priya", nil, owner))
	expectStatus(t, "removed coowner forbidden", e.post("/api/admin/owners/user-marcus", nil, e.as("user-priya")), 403)
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action IN ('owner.added','owner.removed')`); n != int64(2) {
		t.Fatalf("audit count %v", n)
	}
}

func TestOwnershipTransferIsAtomicAndCallerRemainsLeadership(t *testing.T) {
	e := newEnv(t)
	owner := e.as("user-leadership")
	// Force a failure after both ownership writes: the entire transaction must roll back.
	if _, err := e.db.W.Exec(`CREATE TRIGGER fail_owner_audit BEFORE INSERT ON audit_log WHEN NEW.action='owner.transferred' BEGIN SELECT RAISE(ABORT,'constraint: test audit failure'); END;`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, "failed audit", e.post("/api/admin/owners/transfer", map[string]any{"toUserId": "user-priya"}, owner), 409)
	if n := e.scalar(`SELECT count(*) FROM users WHERE id='user-leadership' AND is_owner=1`); n != int64(1) {
		t.Fatal("caller owner flag not rolled back")
	}
	if n := e.scalar(`SELECT count(*) FROM users WHERE id='user-priya' AND is_owner=0 AND role='officer'`); n != int64(1) {
		t.Fatal("target promotion not rolled back")
	}
	if _, err := e.db.W.Exec(`DROP TRIGGER fail_owner_audit`); err != nil {
		t.Fatal(err)
	}
	e.mustOK(e.post("/api/admin/owners/transfer", map[string]any{"toUserId": "user-priya"}, owner))
	if n := e.scalar(`SELECT count(*) FROM users WHERE id='user-leadership' AND is_owner=0 AND role='leadership' AND active=1`); n != int64(1) {
		t.Fatal("caller lost leadership")
	}
	if n := e.scalar(`SELECT count(*) FROM users WHERE id='user-priya' AND is_owner=1 AND role='leadership' AND active=1`); n != int64(1) {
		t.Fatal("target not owner")
	}
	expectStatus(t, "former owner forbidden", e.post("/api/admin/owners/user-marcus", nil, owner), 403)
	e.mustOK(e.get("/api/me", owner)) // transfer does not end caller's unchanged leadership session
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action='owner.transferred'`); n != int64(2) {
		t.Fatalf("audit count %v", n)
	}
	expectStatus(t, "inactive target", e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"active": false}, e.as("user-priya")), 200)
	expectStatus(t, "inactive coowner target", e.post("/api/admin/owners/user-marcus", nil, e.as("user-priya")), 400)
	expectStatus(t, "inactive transfer target", e.post("/api/admin/owners/transfer", map[string]any{"toUserId": "user-marcus"}, e.as("user-priya")), 400)
}

func TestOwnerCanDemoteCoownerButCannotDemoteLastOwner(t *testing.T) {
	e := newEnv(t)
	owner := e.as("user-leadership")
	e.mustOK(e.post("/api/admin/owners/user-priya", nil, owner))
	demoted := e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"role": "officer"}, owner))
	if demoted.at("user", "isOwner") != false || demoted.str("user", "role") != "officer" {
		t.Fatal(string(demoted.Raw))
	}
	expectStatus(t, "last owner demotion blocked", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"role": "director"}, owner), 409)
	e.mustOK(e.post("/api/admin/owners/user-priya", nil, owner))
	deactivated := e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"active": false}, owner))
	if deactivated.at("user", "isOwner") != false || deactivated.at("user", "active") != false {
		t.Fatal(string(deactivated.Raw))
	}
}
