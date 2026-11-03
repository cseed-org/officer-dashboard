ALTER TABLE user_prefs ADD COLUMN dashboard_tutorial_seen INTEGER NOT NULL DEFAULT 0;
ALTER TABLE club_settings ADD COLUMN website_url TEXT NOT NULL DEFAULT '';
ALTER TABLE club_settings ADD COLUMN notion_url TEXT NOT NULL DEFAULT '';
ALTER TABLE club_settings ADD COLUMN drive_url TEXT NOT NULL DEFAULT '';
