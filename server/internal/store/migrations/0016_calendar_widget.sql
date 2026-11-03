CREATE TABLE calendar_widget_settings (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 settings TEXT NOT NULL,
 updated_at INTEGER NOT NULL
);
--> statement-breakpoint
CREATE INDEX events_calendar_start_idx ON events(julianday(starts_at));
--> statement-breakpoint
CREATE INDEX design_calendar_due_idx ON design_requests(due_date);
