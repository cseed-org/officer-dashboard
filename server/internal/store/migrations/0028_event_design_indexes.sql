-- Keep instant_* functions in event queries only: persisted expressions would
-- prevent plain SQLite clients (including the backup integrity check) from working.
CREATE INDEX design_stage_updated_idx ON design_requests(stage,updated_at DESC,id);
--> statement-breakpoint
CREATE INDEX design_event_idx ON design_requests(event_id);
