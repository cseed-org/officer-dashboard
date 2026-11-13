-- Weekly check-in deadlines are versioned. Each rule governs dates from effective_from
-- until the next rule; a change made mid-week starts after the current week (see
-- docs/buildspace.md). The seed rule reproduces the original Monday–Sunday week.
CREATE TABLE buildspace_week_rules (
 effective_from TEXT PRIMARY KEY,
 due_weekday INTEGER NOT NULL CHECK(due_weekday BETWEEN 0 AND 6),
 due_time TEXT CHECK(due_time IS NULL OR (length(due_time)=5 AND due_time GLOB '[0-2][0-9]:[0-5][0-9]' AND due_time<='23:59')),
 updated_at INTEGER NOT NULL,
 updated_by TEXT
);
--> statement-breakpoint
INSERT INTO buildspace_week_rules(effective_from,due_weekday,due_time,updated_at,updated_by) VALUES('0001-01-01',0,NULL,0,NULL);
