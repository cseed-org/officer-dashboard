-- Rebuild users with foreign keys disabled by the migration runner before BEGIN.
--> statement-breakpoint
CREATE TABLE `__new_users` (
	`id` text PRIMARY KEY NOT NULL,
	`discord_id` text NOT NULL,
	`name` text NOT NULL,
	`email` text,
	`avatar_url` text,
	`role` text DEFAULT 'officer' NOT NULL,
 `is_owner` integer DEFAULT 0 NOT NULL CHECK(is_owner IN (0,1)),
	`active` integer DEFAULT true NOT NULL,
	`title` text,
	`last_login_at` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	CHECK(is_owner = 0 OR (role = 'leadership' AND active = 1)),
	CONSTRAINT "users_role_check" CHECK("role" IN ('leadership', 'director', 'officer'))
);

--> statement-breakpoint
INSERT INTO __new_users(id,discord_id,name,email,avatar_url,role,active,title,last_login_at,created_at,updated_at) SELECT id,discord_id,name,email,avatar_url,role,active,title,last_login_at,created_at,updated_at FROM users;
--> statement-breakpoint
DROP TABLE users;
--> statement-breakpoint
ALTER TABLE __new_users RENAME TO users;
--> statement-breakpoint
CREATE UNIQUE INDEX users_discord_id_unique ON users(discord_id);
--> statement-breakpoint
-- Preserve the last administrator rule, counting leadership as director+.
CREATE TRIGGER users_last_director BEFORE UPDATE OF active, role ON users
WHEN OLD.active = 1 AND OLD.role IN ('director','leadership')
 AND (NEW.active = 0 OR NEW.role NOT IN ('director','leadership'))
 AND NOT EXISTS (SELECT 1 FROM users WHERE id <> OLD.id AND active = 1 AND role IN ('director','leadership'))
BEGIN SELECT RAISE(ABORT, 'cannot remove last active director'); END;
--> statement-breakpoint
CREATE TABLE subteams (
 id TEXT PRIMARY KEY NOT NULL,
 name TEXT NOT NULL UNIQUE CHECK(length(trim(name)) > 0)
);
--> statement-breakpoint
CREATE TABLE officer_profiles (
 user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 subteam_id TEXT NOT NULL REFERENCES subteams(id),
 phone TEXT NOT NULL CHECK(length(trim(phone)) > 0),
 birthday TEXT NOT NULL CHECK(birthday GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
 preferred_name TEXT, pronouns TEXT, personal_email TEXT, school_email TEXT,
 grad_date TEXT, title TEXT, start_term TEXT, linkedin TEXT,
 calendar_delivery_mode TEXT NOT NULL DEFAULT 'ics' CHECK(calendar_delivery_mode IN ('ics','google'))
);
--> statement-breakpoint
CREATE TABLE calendar_emails (
 id TEXT PRIMARY KEY NOT NULL,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 email TEXT NOT NULL COLLATE NOCASE CHECK(length(trim(email)) > 0 AND email = lower(trim(email)) COLLATE BINARY),
 active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
 UNIQUE(user_id,email)
);
--> statement-breakpoint
CREATE UNIQUE INDEX calendar_emails_one_active ON calendar_emails(user_id) WHERE active = 1;
--> statement-breakpoint
CREATE TRIGGER calendar_emails_limit_insert BEFORE INSERT ON calendar_emails
WHEN (SELECT count(*) FROM calendar_emails WHERE user_id = NEW.user_id) >= 5
BEGIN SELECT RAISE(ABORT, 'at most 5 calendar emails per user'); END;
--> statement-breakpoint
CREATE TRIGGER calendar_emails_limit_update BEFORE UPDATE OF user_id ON calendar_emails
WHEN NEW.user_id <> OLD.user_id AND (SELECT count(*) FROM calendar_emails WHERE user_id = NEW.user_id) >= 5
BEGIN SELECT RAISE(ABORT, 'at most 5 calendar emails per user'); END;
--> statement-breakpoint
CREATE TABLE invites (
 id TEXT PRIMARY KEY NOT NULL,
 name TEXT NOT NULL CHECK(length(trim(name)) > 0),
 discord_id TEXT NOT NULL,
 subteam_id TEXT NOT NULL REFERENCES subteams(id),
 invited_by TEXT NOT NULL REFERENCES users(id),
 method TEXT NOT NULL CHECK(method IN ('dm','link','both')),
 dm_sent_at INTEGER,
 token_hash TEXT NOT NULL UNIQUE,
 accepted_at INTEGER,
 created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000)
);
--> statement-breakpoint
CREATE UNIQUE INDEX invites_pending_discord ON invites(discord_id) WHERE accepted_at IS NULL;
--> statement-breakpoint
CREATE TABLE user_prefs (
 user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 widget_layout TEXT NOT NULL CHECK(json_valid(widget_layout) AND json_type(widget_layout) = 'array'),
 updated_at INTEGER NOT NULL
);

--> statement-breakpoint
CREATE TRIGGER users_last_leadership BEFORE UPDATE OF active, role ON users
WHEN OLD.active = 1 AND OLD.role = 'leadership'
 AND (NEW.active = 0 OR NEW.role <> 'leadership')
 AND NOT EXISTS (SELECT 1 FROM users WHERE id <> OLD.id AND active = 1 AND role = 'leadership')
BEGIN SELECT RAISE(ABORT, 'cannot remove last active leadership user'); END;
--> statement-breakpoint
CREATE TRIGGER users_last_leadership_delete BEFORE DELETE ON users
WHEN OLD.active = 1 AND OLD.role = 'leadership'
 AND NOT EXISTS (SELECT 1 FROM users WHERE id <> OLD.id AND active = 1 AND role = 'leadership')
BEGIN SELECT RAISE(ABORT, 'cannot remove last active leadership user'); END;

--> statement-breakpoint
CREATE TRIGGER users_last_owner BEFORE UPDATE OF is_owner, role, active ON users
WHEN OLD.is_owner = 1 AND (NEW.is_owner = 0 OR NEW.role <> 'leadership' OR NEW.active <> 1)
 AND NOT EXISTS (SELECT 1 FROM users WHERE id <> OLD.id AND is_owner = 1)
BEGIN SELECT RAISE(ABORT, 'cannot remove last owner'); END;
--> statement-breakpoint
CREATE TRIGGER users_last_owner_delete BEFORE DELETE ON users
WHEN OLD.is_owner = 1 AND NOT EXISTS (SELECT 1 FROM users WHERE id <> OLD.id AND is_owner = 1)
BEGIN SELECT RAISE(ABORT, 'cannot remove last owner'); END;
--> statement-breakpoint
CREATE TRIGGER users_owner_no_replace BEFORE INSERT ON users
WHEN EXISTS (SELECT 1 FROM users WHERE is_owner = 1 AND (id = NEW.id OR discord_id = NEW.discord_id))
BEGIN SELECT RAISE(ABORT, 'cannot replace an owner'); END;
--> statement-breakpoint
CREATE TRIGGER users_owner_no_rekey BEFORE UPDATE OF id, discord_id ON users
WHEN EXISTS (SELECT 1 FROM users WHERE is_owner = 1 AND id <> OLD.id AND (id = NEW.id OR discord_id = NEW.discord_id))
BEGIN SELECT RAISE(ABORT, 'cannot replace an owner'); END;
