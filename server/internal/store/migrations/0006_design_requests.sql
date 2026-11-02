-- V0.3 Design Requests: a board everyone sees (New → Accepted → In progress → In review → Delivered).
-- Design-team membership is officer_profiles.subteam_id pointing at the subteam named "Design".
INSERT INTO subteams(id, name) SELECT 'team-design', 'Design' WHERE NOT EXISTS (SELECT 1 FROM subteams WHERE name = 'Design' COLLATE NOCASE);
--> statement-breakpoint
CREATE TABLE design_requests (
 id TEXT PRIMARY KEY NOT NULL,
 title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
 type TEXT NOT NULL CHECK(type IN ('poster','social','slides','merch','video','other')),
 formats TEXT NOT NULL DEFAULT '' CHECK(length(formats) <= 500),
 due_date TEXT CHECK(due_date IS NULL OR due_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
 urgency TEXT NOT NULL DEFAULT 'normal' CHECK(urgency IN ('normal','high')),
 brief TEXT NOT NULL CHECK(length(trim(brief)) BETWEEN 1 AND 4000),
 refs TEXT NOT NULL DEFAULT '' CHECK(length(refs) <= 4000),
 event_id TEXT REFERENCES events(id) ON DELETE SET NULL,
 program TEXT CHECK(program IS NULL OR length(trim(program)) BETWEEN 1 AND 200),
 stage TEXT NOT NULL DEFAULT 'new' CHECK(stage IN ('new','accepted','in-progress','in-review','delivered')),
 requester_id TEXT NOT NULL REFERENCES users(id),
 assignee_id TEXT REFERENCES users(id),
 approved_by TEXT REFERENCES users(id),
 approved_at INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 -- Work past New belongs to a designer; Delivered only follows the requester's approval.
 CHECK(stage = 'new' OR assignee_id IS NOT NULL),
 CHECK(stage <> 'delivered' OR (approved_at IS NOT NULL AND approved_by = requester_id))
);
--> statement-breakpoint
CREATE INDEX design_requests_stage_idx ON design_requests(stage, updated_at);
--> statement-breakpoint
CREATE INDEX design_requests_event_idx ON design_requests(event_id);
--> statement-breakpoint
CREATE TABLE design_request_comments (
 id TEXT PRIMARY KEY NOT NULL,
 request_id TEXT NOT NULL REFERENCES design_requests(id) ON DELETE CASCADE,
 author_id TEXT NOT NULL REFERENCES users(id),
 body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 4000),
 created_at INTEGER NOT NULL
);
--> statement-breakpoint
CREATE INDEX design_request_comments_request_idx ON design_request_comments(request_id, created_at);
--> statement-breakpoint
CREATE TABLE design_request_files (
 id TEXT PRIMARY KEY NOT NULL,
 request_id TEXT NOT NULL REFERENCES design_requests(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('reference','review','delivered')),
 filename TEXT NOT NULL CHECK(length(filename) BETWEEN 1 AND 255),
 mime_type TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
 storage TEXT NOT NULL CHECK(storage IN ('drive','local')),
 storage_key TEXT NOT NULL,
 web_view_link TEXT,
 uploaded_by TEXT NOT NULL REFERENCES users(id),
 created_at INTEGER NOT NULL
);
--> statement-breakpoint
CREATE INDEX design_request_files_request_idx ON design_request_files(request_id, created_at);
