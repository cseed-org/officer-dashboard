CREATE TABLE events_next (
 id TEXT PRIMARY KEY NOT NULL, luma_id TEXT, name TEXT NOT NULL, starts_at TEXT, ends_at TEXT,
 location TEXT, url TEXT, host_user_id TEXT REFERENCES users(id),
 created_at INTEGER NOT NULL DEFAULT (unixepoch('subsec')*1000), updated_at INTEGER NOT NULL DEFAULT (unixepoch('subsec')*1000),
 status TEXT NOT NULL DEFAULT 'published' CHECK(status IN ('draft','published','completed','cancelled')),
 description TEXT CHECK(description IS NULL OR length(description)<=5000),
 capacity INTEGER CHECK(capacity IS NULL OR (typeof(capacity)='integer' AND capacity BETWEEN 1 AND 100000)),
 host_subteam_id TEXT REFERENCES subteams(id), luma_url TEXT CHECK(luma_url IS NULL OR luma_url GLOB 'https://*'), notion_url TEXT,
 design_need TEXT NOT NULL DEFAULT 'unknown' CHECK(design_need IN ('unknown','needed','not_needed')), starts_at_ms INTEGER,
 tentative_from TEXT, tentative_to TEXT,
 CHECK(starts_at IS NOT NULL OR (tentative_from IS NOT NULL AND tentative_to>=tentative_from))
);
INSERT INTO events_next(id,luma_id,name,starts_at,ends_at,location,url,host_user_id,created_at,updated_at,status,description,capacity,host_subteam_id,luma_url,notion_url,design_need,starts_at_ms)
 SELECT id,luma_id,name,starts_at,ends_at,location,url,host_user_id,created_at,updated_at,status,description,capacity,host_subteam_id,luma_url,notion_url,design_need,starts_at_ms FROM events;
DROP TABLE events;
ALTER TABLE events_next RENAME TO events;
CREATE UNIQUE INDEX events_luma_id_unique ON events(luma_id);
CREATE INDEX events_starts_idx ON events(starts_at);
CREATE UNIQUE INDEX events_luma_url_unique ON events(lower(luma_url)) WHERE luma_url IS NOT NULL;
CREATE INDEX events_calendar_start_idx ON events(julianday(starts_at));
CREATE INDEX events_starts_ms_id_idx ON events(starts_at_ms,id);
