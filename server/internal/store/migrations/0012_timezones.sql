CREATE TABLE club_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),
 club_timezone TEXT NOT NULL DEFAULT 'America/Los_Angeles',
 location TEXT NOT NULL DEFAULT 'University of Washington, Seattle'
);
--> statement-breakpoint
INSERT INTO club_settings(id) VALUES(1);
--> statement-breakpoint
ALTER TABLE user_prefs ADD COLUMN timezone_mode TEXT NOT NULL DEFAULT 'club' CHECK(timezone_mode IN ('club','auto','manual'));
--> statement-breakpoint
ALTER TABLE user_prefs ADD COLUMN timezone TEXT NOT NULL DEFAULT '';
