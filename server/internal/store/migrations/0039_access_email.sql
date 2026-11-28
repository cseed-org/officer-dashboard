-- Officer access email (V0.16, PR 1 of the Cloudflare Access sync). The address Cloudflare
-- Access will send a person's sign-in PIN to, typed by a director when adding or inviting
-- them. Separate from users.email (the org calendar's sharing address) and from the
-- profile's personal and school emails, which officers edit themselves. Nothing fills it in:
-- every existing row stays NULL. Stored trimmed and lower-case, so equality is exact.
ALTER TABLE users ADD COLUMN access_email TEXT CHECK(access_email IS NULL OR
 (access_email = lower(trim(access_email)) AND length(access_email) BETWEEN 3 AND 254 AND instr(access_email, '@') > 1));
--> statement-breakpoint
CREATE UNIQUE INDEX users_access_email ON users(access_email) WHERE access_email IS NOT NULL;
--> statement-breakpoint
ALTER TABLE invites ADD COLUMN access_email TEXT CHECK(access_email IS NULL OR
 (access_email = lower(trim(access_email)) AND length(access_email) BETWEEN 3 AND 254 AND instr(access_email, '@') > 1));
--> statement-breakpoint
-- Pending (not accepted) invites, expired ones included until they are removed, hold their
-- email. An accepted invite keeps its email only as history.
CREATE UNIQUE INDEX invites_pending_access_email ON invites(access_email) WHERE accepted_at IS NULL AND access_email IS NOT NULL;
--> statement-breakpoint
-- A pending invite's email can't belong to another account, and the reverse. The invitee's
-- own account (same Discord ID) takes the email over while accepting.
CREATE TRIGGER invites_access_email_insert BEFORE INSERT ON invites
WHEN NEW.access_email IS NOT NULL AND NEW.accepted_at IS NULL
 AND EXISTS (SELECT 1 FROM users WHERE access_email = NEW.access_email AND discord_id <> NEW.discord_id)
BEGIN SELECT RAISE(ABORT, 'access email belongs to another account'); END;
--> statement-breakpoint
CREATE TRIGGER invites_access_email_update BEFORE UPDATE OF access_email ON invites
WHEN NEW.access_email IS NOT NULL AND NEW.accepted_at IS NULL
 AND EXISTS (SELECT 1 FROM users WHERE access_email = NEW.access_email AND discord_id <> NEW.discord_id)
BEGIN SELECT RAISE(ABORT, 'access email belongs to another account'); END;
--> statement-breakpoint
CREATE TRIGGER users_access_email_insert BEFORE INSERT ON users
WHEN NEW.access_email IS NOT NULL
 AND EXISTS (SELECT 1 FROM invites WHERE accepted_at IS NULL AND access_email = NEW.access_email AND discord_id <> NEW.discord_id)
BEGIN SELECT RAISE(ABORT, 'access email belongs to a pending invite'); END;
--> statement-breakpoint
CREATE TRIGGER users_access_email_update BEFORE UPDATE OF access_email ON users
WHEN NEW.access_email IS NOT NULL
 AND EXISTS (SELECT 1 FROM invites WHERE accepted_at IS NULL AND access_email = NEW.access_email AND discord_id <> NEW.discord_id)
BEGIN SELECT RAISE(ABORT, 'access email belongs to a pending invite'); END;
--> statement-breakpoint
-- Used by the Access sync (PR 2); empty and unread until then.
-- What the dashboard itself put in its Cloudflare Access group, one row per email.
CREATE TABLE access_sync_members (
 email TEXT PRIMARY KEY CHECK(email = lower(trim(email)) AND instr(email, '@') > 1),
 state TEXT NOT NULL CHECK(state IN ('adding','added','removing','missing')),
 added_at INTEGER,
 changed_at INTEGER NOT NULL,
 note TEXT
);
--> statement-breakpoint
-- Session revocations still to send, queued with the confirmed removal they follow.
CREATE TABLE access_sync_revocations (
 email TEXT PRIMARY KEY CHECK(email = lower(trim(email)) AND instr(email, '@') > 1),
 queued_at INTEGER NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT
);
--> statement-breakpoint
-- The sync's single state row: change counter, pause switch, last write and a held plan.
CREATE TABLE access_sync_state (
 id INTEGER PRIMARY KEY CHECK(id = 1),
 generation INTEGER NOT NULL DEFAULT 0,
 paused INTEGER NOT NULL DEFAULT 0 CHECK(paused IN (0,1)),
 group_id TEXT,
 group_name TEXT,
 group_updated_at TEXT,
 last_success_at INTEGER,
 last_error TEXT,
 failures INTEGER NOT NULL DEFAULT 0,
 held_plan TEXT CHECK(held_plan IS NULL OR json_valid(held_plan)),
 held_reason TEXT
);
