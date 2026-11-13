-- Permit director+ to move unassigned work and leadership to override delivery.
CREATE TABLE design_requests_next (
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
 -- Stage and assignment rights are enforced transactionally by the API.
 -- Approval metadata always records the actual requester; leadership overrides leave it empty.
 CHECK((approved_at IS NULL AND approved_by IS NULL) OR (approved_at IS NOT NULL AND approved_by IS NOT NULL AND approved_by = requester_id))
);

--> statement-breakpoint
INSERT INTO design_requests_next SELECT * FROM design_requests;
--> statement-breakpoint
DROP TABLE design_requests;
--> statement-breakpoint
ALTER TABLE design_requests_next RENAME TO design_requests;
--> statement-breakpoint
CREATE INDEX design_requests_stage_idx ON design_requests(stage, updated_at);
--> statement-breakpoint
CREATE INDEX design_requests_event_idx ON design_requests(event_id);
