-- V0.3 events: drafts in the dashboard, a manual Luma handoff, a cover image, and link
-- fields for the Notion page (stub) and the design request (design_requests lands in Pane F).
-- Rows that exist already came from Luma imports, so they start as published.
ALTER TABLE events ADD COLUMN status TEXT NOT NULL DEFAULT 'published' CHECK(status IN ('draft','published','completed','cancelled'));
--> statement-breakpoint
ALTER TABLE events ADD COLUMN description TEXT CHECK(description IS NULL OR length(description) <= 5000);
--> statement-breakpoint
ALTER TABLE events ADD COLUMN capacity INTEGER CHECK(capacity IS NULL OR (typeof(capacity) = 'integer' AND capacity BETWEEN 1 AND 100000));
--> statement-breakpoint
ALTER TABLE events ADD COLUMN host_subteam_id TEXT REFERENCES subteams(id);
--> statement-breakpoint
-- The public event page the officer pasted back after publishing in Luma (https lu.ma / luma.com).
ALTER TABLE events ADD COLUMN luma_url TEXT CHECK(luma_url IS NULL OR luma_url GLOB 'https://*');
--> statement-breakpoint
CREATE UNIQUE INDEX events_luma_url_unique ON events(luma_url) WHERE luma_url IS NOT NULL;
--> statement-breakpoint
-- TODO(notion): today this holds the stub's placeholder link; see docs/notion-integration.md.
ALTER TABLE events ADD COLUMN notion_url TEXT;
--> statement-breakpoint
-- The design request made from this event. No foreign key yet: design_requests is built in
-- the design-requests Pane, which should add the reference when its table lands.
ALTER TABLE events ADD COLUMN design_request_id TEXT;
--> statement-breakpoint
-- One cover per event, kept in the database: covers are small and must work without Drive.
CREATE TABLE event_covers (
 event_id TEXT PRIMARY KEY NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 mime_type TEXT NOT NULL CHECK(mime_type IN ('image/png','image/jpeg','image/webp')),
 bytes BLOB NOT NULL CHECK(length(bytes) BETWEEN 1 AND 5242880),
 filename TEXT NOT NULL,
 updated_at INTEGER NOT NULL
);
