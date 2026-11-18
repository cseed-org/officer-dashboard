CREATE INDEX events_instant_id_idx ON events(instant_ms(starts_at),id);

--> statement-breakpoint
CREATE INDEX design_stage_updated_idx ON design_requests(stage,updated_at DESC,id);
--> statement-breakpoint
CREATE INDEX design_event_idx ON design_requests(event_id);
