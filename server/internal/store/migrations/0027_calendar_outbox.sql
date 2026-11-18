-- Calendar delivery outbox (V0.14.1, D-23). Every event change and every recipient
-- backfill writes its job here in the same transaction as the change itself; one
-- background worker drains it in order and deletes each job when done, and the server
-- drains what is left at startup. A cancellation carries the deleted event's snapshot
-- and the deliveries still to withdraw, because those rows cascade with the event.
CREATE TABLE calendar_outbox (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 kind TEXT NOT NULL CHECK(kind IN ('sync','cancel','backfill')),
 event_id TEXT,
 user_id TEXT,
 snapshot TEXT CHECK(snapshot IS NULL OR json_valid(snapshot)),
 priors TEXT CHECK(priors IS NULL OR json_valid(priors)),
 attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT,
 created_at INTEGER NOT NULL,
 CHECK((kind = 'backfill') = (user_id IS NOT NULL AND event_id IS NULL)),
 CHECK((kind = 'cancel') = (snapshot IS NOT NULL))
);
--> statement-breakpoint
-- One delivery row per destination (an .ics address, or one Google account) instead of
-- per officer, so a failed attempt never overwrites the last successful one, a newly
-- connected Google account gets its own events, and cancellations reach every calendar
-- that still has the invite. status 'sent' means the destination has it; cancelled = 1
-- means it has since been withdrawn there. error is the last attempt's failure, kept
-- beside the last success.
CREATE TABLE __new_calendar_deliveries (
 event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 destination TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('ics','google')),
 recipient TEXT,
 google_event_id TEXT,
 sequence INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('sent','failed')),
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 error TEXT,
 content_hash TEXT,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(event_id, user_id, destination)
);
--> statement-breakpoint
INSERT INTO __new_calendar_deliveries(event_id,user_id,destination,mode,recipient,google_event_id,sequence,status,cancelled,error,content_hash,updated_at)
SELECT d.event_id, d.user_id,
 CASE d.mode WHEN 'ics' THEN 'ics:' || lower(coalesce(d.recipient, ''))
  ELSE 'google:' || coalesce((SELECT lower(t.account_email) FROM google_calendar_tokens t WHERE t.user_id = d.user_id), '') END,
 d.mode, d.recipient, d.google_event_id, d.sequence, d.status, 0, d.error, d.content_hash, d.updated_at
FROM calendar_deliveries d;
--> statement-breakpoint
DROP TABLE calendar_deliveries;
--> statement-breakpoint
ALTER TABLE __new_calendar_deliveries RENAME TO calendar_deliveries;
