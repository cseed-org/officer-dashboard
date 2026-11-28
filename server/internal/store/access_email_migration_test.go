package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// upgradeFrom builds a database at every migration before `before`, the schema an
// existing install has when the unattended updater brings in the new image.
func upgradeFrom(t *testing.T, before string) (*sql.DB, func(string, ...any)) {
	t.Helper()
	db, err := sql.Open(driverName, "file:"+t.TempDir()+"/upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	must(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`)
	entries, _ := migrationFS.ReadDir("migrations")
	for _, e := range entries {
		if e.Name() >= before {
			continue
		}
		raw, _ := migrationFS.ReadFile("migrations/" + e.Name())
		for _, s := range strings.Split(string(raw), "--> statement-breakpoint") {
			if strings.TrimSpace(s) != "" {
				must(s)
			}
		}
		must(`INSERT INTO schema_migrations VALUES(?,0)`, e.Name())
	}
	return db, must
}

func TestAccessEmailUpgradeKeepsPeopleAndFillsNothingIn(t *testing.T) {
	db, must := upgradeFrom(t, "0039")
	// An install in use: an owner, a director, a turned-off officer with a calendar email,
	// a profile with personal and school emails, and pending, expired and accepted invites.
	must(`INSERT INTO subteams VALUES('team-events','Events')`)
	must(`INSERT INTO users(id,discord_id,name,email,role,is_owner,active) VALUES('u-owner','100000000000000005','Sam Rivera','sam@example.org','leadership',1,1)`)
	// Jordan joined through an invite: it existed before the account did.
	must(`INSERT INTO invites(id,name,discord_id,subteam_id,invited_by,method,token_hash,expires_at,created_at) VALUES
	 ('inv-accepted','Jordan Lee','100000000000000001','team-events','u-owner','link','h3',9999999999999,1)`)
	must(`INSERT INTO users(id,discord_id,name,email,role,is_owner,active) VALUES
	 ('u-dir','100000000000000001','Jordan Lee','jordan@example.org','director',0,1),
	 ('u-off','100000000000000002','Priya Shah','priya@example.org','officer',0,0)`)
	must(`UPDATE invites SET accepted_at=5,user_id='u-dir' WHERE id='inv-accepted'`)
	must(`INSERT INTO officer_profiles(user_id,subteam_id,phone,birthday,personal_email,school_email) VALUES('u-dir','team-events','+1 206 555 0100','01-15','jl@example.com','jl@uw.edu')`)
	must(`INSERT INTO invites(id,name,discord_id,subteam_id,invited_by,method,token_hash,expires_at,created_at) VALUES
	 ('inv-pending','Alex Chen','100000000000000777','team-events','u-dir','link','h1',9999999999999,1),
	 ('inv-expired','Old Invite','100000000000000778','team-events','u-dir','link','h2',2,1)`)

	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE access_email IS NULL`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("users without an access email = %d (%v), want all 3: nothing is copied from the calendar email", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM invites WHERE access_email IS NULL`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("invites without an access email = %d (%v), want all 3", n, err)
	}
	var email, name string
	if err := db.QueryRow(`SELECT email, name FROM users WHERE id='u-off' AND active=0`).Scan(&email, &name); err != nil || email != "priya@example.org" {
		t.Fatalf("existing user changed: %q %q %v", name, email, err)
	}
	for _, table := range []string{"access_sync_members", "access_sync_revocations", "access_sync_state"} {
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d rows, %v; want an empty table", table, n, err)
		}
	}
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != MigrationCount() {
		t.Fatalf("migration count %d: %v", n, err)
	}
	// Running again (every start does) is a no-op.
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestAccessEmailRulesInTheSchema(t *testing.T) {
	db, must := upgradeFrom(t, "0039")
	must(`INSERT INTO subteams VALUES('team-events','Events')`)
	must(`INSERT INTO users(id,discord_id,name,role,is_owner,active) VALUES
	 ('u-owner','100000000000000005','Sam Rivera','leadership',1,1),
	 ('u-dir','100000000000000001','Jordan Lee','director',0,1)`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	refused := func(label, query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err == nil {
			t.Errorf("%s: accepted, want refused", label)
		}
	}
	must(`UPDATE users SET access_email='sam@example.org' WHERE id='u-owner'`)
	refused("mixed case", `UPDATE users SET access_email='Jordan@Example.org' WHERE id='u-dir'`)
	refused("spaces", `UPDATE users SET access_email=' jordan@example.org' WHERE id='u-dir'`)
	refused("no @", `UPDATE users SET access_email='jordan' WHERE id='u-dir'`)
	refused("empty", `UPDATE users SET access_email='' WHERE id='u-dir'`)
	refused("two users, one email", `UPDATE users SET access_email='sam@example.org' WHERE id='u-dir'`)

	invite := func(id, discord, email string, accepted any) error {
		_, err := db.Exec(`INSERT INTO invites(id,name,discord_id,subteam_id,invited_by,method,token_hash,expires_at,accepted_at,access_email,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,1)`,
			id, "Name "+id, discord, "team-events", "u-dir", "link", "h-"+id, 9999999999999, accepted, email)
		return err
	}
	if err := invite("i1", "100000000000000777", "alex@example.org", nil); err != nil {
		t.Fatal(err)
	}
	if err := invite("i2", "100000000000000778", "alex@example.org", nil); err == nil {
		t.Error("two pending invites with one access email: accepted")
	}
	if err := invite("i3", "100000000000000779", "sam@example.org", nil); err == nil {
		t.Error("a pending invite with another account's access email: accepted")
	}
	if err := invite("i4", "100000000000000780", "Mixed@Example.org", nil); err == nil {
		t.Error("mixed-case invite email: accepted")
	}
	refused("user takes a pending invite's email", `UPDATE users SET access_email='alex@example.org' WHERE id='u-dir'`)
	// Accepting: the invitee's own new account takes the email while the invite is still pending.
	must(`INSERT INTO users(id,discord_id,name,role,active,access_email) VALUES('u-alex','100000000000000777','Alex Chen','officer',1,'alex@example.org')`)
	must(`UPDATE invites SET accepted_at=5,user_id='u-alex' WHERE id='i1'`)
	// An accepted invite keeps its email as history without blocking anyone.
	must(`UPDATE users SET access_email='alex.chen@example.org' WHERE id='u-alex'`)
	must(`UPDATE users SET access_email='alex@example.org' WHERE id='u-dir'`)
	if err := invite("i5", "100000000000000781", "alex.chen@example.org", 5); err != nil {
		t.Fatalf("accepted invite sharing an email: %v", err)
	}
}

// TestAccessEmailRollbackToPreviousImage replays what the image before this migration does
// against an upgraded database: its migration runner skips 0039 (it only runs files it
// ships), and its user and invite statements, which never name access_email, still work.
func TestAccessEmailRollbackToPreviousImage(t *testing.T) {
	db, must := upgradeFrom(t, "0039")
	must(`INSERT INTO subteams VALUES('team-events','Events')`)
	must(`INSERT INTO users(id,discord_id,name,role,is_owner,active) VALUES('u-owner','100000000000000005','Sam Rivera','leadership',1,1)`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	must(`UPDATE users SET access_email='sam@example.org' WHERE id='u-owner'`)
	// The previous image's statements, verbatim from store/users.go and store/invites.go.
	must(`INSERT INTO invites(id,name,discord_id,subteam_id,invited_by,method,token_hash,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"inv-old", "Alex Chen", "100000000000000777", "team-events", "u-owner", "link", "h1", int64(9999999999999), int64(1))
	must(`UPDATE invites SET name=?,subteam_id=?,invited_by=?,method=?,token_hash=?,expires_at=?,dm_sent_at=NULL,dm_error=NULL,created_at=? WHERE id=? AND accepted_at IS NULL`,
		"Alex Chen", "team-events", "u-owner", "dm", "h2", int64(9999999999999), int64(2), "inv-old")
	must(`INSERT INTO users (id, discord_id, name, email, avatar_url, role, is_owner, active, title, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"u-alex", "100000000000000777", "Alex Chen", nil, nil, "officer", false, true, nil, int64(3), int64(3))
	must(`UPDATE invites SET accepted_at=?,user_id=? WHERE id=? AND accepted_at IS NULL`, int64(3), "u-alex", "inv-old")
	must(`UPDATE users SET name = ?, active = ?, updated_at = ? WHERE id = ?`, "Alex C.", false, int64(4), "u-alex")
	var email sql.NullString
	if err := db.QueryRow(`SELECT access_email FROM users WHERE id='u-owner'`).Scan(&email); err != nil || email.String != "sam@example.org" {
		t.Fatalf("the previous image's writes disturbed an access email: %v %v", email, err)
	}
}
