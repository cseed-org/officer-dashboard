-- V0.9 onboarding: everyone below the current onboarding version goes through the intro and
-- intake once (existing accounts start at 0). The resume step now lives on the user, since
-- accounts without an invite go through it too. team is the officer's team/committee.
ALTER TABLE users ADD COLUMN onboarding_version INTEGER NOT NULL DEFAULT 0;
--> statement-breakpoint
ALTER TABLE users ADD COLUMN onboarding_step INTEGER NOT NULL DEFAULT 1;
--> statement-breakpoint
ALTER TABLE officer_profiles ADD COLUMN team TEXT;
