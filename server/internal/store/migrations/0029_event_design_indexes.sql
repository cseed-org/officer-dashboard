-- Store the parsed instant as ordinary data: persisted app-function expressions
-- prevent plain SQLite backup/repair clients from working. The backfill runs once
-- through the app's migration connection; no function is retained in the schema.
ALTER TABLE events ADD COLUMN starts_at_ms INTEGER;
--> statement-breakpoint
UPDATE events SET starts_at_ms=instant_ms(starts_at);
--> statement-breakpoint
CREATE INDEX events_starts_ms_id_idx ON events(starts_at_ms,id);
--> statement-breakpoint
CREATE INDEX design_stage_updated_idx ON design_requests(stage,updated_at DESC,id);
--> statement-breakpoint
CREATE INDEX design_event_idx ON design_requests(event_id);
