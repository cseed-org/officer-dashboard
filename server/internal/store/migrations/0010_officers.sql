-- V0.3 officers: invite lifecycle, onboarding, calendar delivery and Google OAuth tokens.
ALTER TABLE invites ADD COLUMN expires_at INTEGER;
--> statement-breakpoint
ALTER TABLE invites ADD COLUMN dm_error TEXT;
--> statement-breakpoint
-- Set when the invitee first signs in and their account is created.
ALTER TABLE invites ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
--> statement-breakpoint
-- Set when the invitee finishes the onboarding flow. Only accepted invites have one.
ALTER TABLE invites ADD COLUMN onboarded_at INTEGER CHECK(onboarded_at IS NULL OR accepted_at IS NOT NULL);
--> statement-breakpoint
-- The onboarding step to resume at (1-6), saved by "Finish later" and each step.
ALTER TABLE invites ADD COLUMN onboarding_step INTEGER NOT NULL DEFAULT 1 CHECK(onboarding_step BETWEEN 1 AND 6);
--> statement-breakpoint
-- Invites from before expiry existed get the standard 7 days from when they were sent.
UPDATE invites SET expires_at = created_at + 604800000 WHERE accepted_at IS NULL AND expires_at IS NULL;
--> statement-breakpoint
CREATE INDEX invites_user ON invites(user_id) WHERE user_id IS NOT NULL;
--> statement-breakpoint
-- Discord IDs that already belong to an account cannot hold a pending invite.
CREATE TRIGGER invites_no_existing_user BEFORE INSERT ON invites
WHEN EXISTS (SELECT 1 FROM users WHERE discord_id = NEW.discord_id)
BEGIN SELECT RAISE(ABORT, 'discord account already has a dashboard account'); END;
--> statement-breakpoint
-- One row per connected Google account. Tokens are AES-GCM sealed by the server.
CREATE TABLE google_calendar_tokens (
 user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 account_email TEXT,
 sealed_refresh_token TEXT NOT NULL,
 sealed_access_token TEXT,
 access_expires_at INTEGER,
 connected_at INTEGER NOT NULL
);
--> statement-breakpoint
-- What was sent to whom, so an event update re-sends with a higher SEQUENCE (.ics) or
-- patches the same Google event instead of creating a duplicate.
CREATE TABLE calendar_deliveries (
 event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('ics','google')),
 recipient TEXT,
 google_event_id TEXT,
 sequence INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('sent','failed')),
 error TEXT,
 -- What the recipient last received (mode, address and event content), to skip resends.
 content_hash TEXT,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(event_id, user_id)
);
--> statement-breakpoint
-- Birthdays are month and day only (no year is collected). Existing YYYY-MM-DD values
-- keep their month and day.
CREATE TABLE __new_officer_profiles (
 user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 subteam_id TEXT NOT NULL REFERENCES subteams(id),
 phone TEXT NOT NULL CHECK(length(trim(phone)) > 0),
 birthday TEXT NOT NULL CHECK(birthday GLOB '[0-1][0-9]-[0-3][0-9]'),
 preferred_name TEXT, pronouns TEXT, personal_email TEXT, school_email TEXT,
 grad_date TEXT, title TEXT, start_term TEXT, linkedin TEXT,
 calendar_delivery_mode TEXT NOT NULL DEFAULT 'ics' CHECK(calendar_delivery_mode IN ('ics','google'))
);
--> statement-breakpoint
INSERT INTO __new_officer_profiles SELECT user_id,subteam_id,phone,
 CASE WHEN length(birthday) = 10 THEN substr(birthday,6,5) ELSE birthday END,
 preferred_name,pronouns,personal_email,school_email,grad_date,title,start_term,linkedin,calendar_delivery_mode
 FROM officer_profiles;
--> statement-breakpoint
DROP TABLE officer_profiles;
--> statement-breakpoint
ALTER TABLE __new_officer_profiles RENAME TO officer_profiles;
