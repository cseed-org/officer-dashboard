-- Calendar delivery outbox (V0.14.1, D-23). Every event change and every recipient
-- backfill writes its job here in the same transaction as the change itself; one
-- background worker drains it in order and deletes each job once it has fully
-- succeeded, and the server drains what is left at startup. A failed job stays, with
-- backoff (next_attempt_at), until it succeeds. A cancellation carries the deleted
-- event's snapshot and the deliveries still to withdraw, because those rows cascade with
-- the event.
CREATE TABLE calendar_outbox (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 kind TEXT NOT NULL CHECK(kind IN ('sync','cancel','backfill')),
 event_id TEXT,
 user_id TEXT,
 snapshot TEXT CHECK(snapshot IS NULL OR json_valid(snapshot)),
 priors TEXT CHECK(priors IS NULL OR json_valid(priors)),
 attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT,
 next_attempt_at INTEGER NOT NULL DEFAULT 0,
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
-- beside the last success. attempt_sequence is written before each send and cleared when
-- its outcome is recorded: a row that keeps it after a crash may have the invite, so a
-- deletion cancels it too; 'pending' is such a row with no earlier success.
CREATE TABLE __new_calendar_deliveries (
 event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 destination TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('ics','google')),
 recipient TEXT,
 google_event_id TEXT,
 sequence INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('sent','failed','pending')),
 attempt_sequence INTEGER,
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)),
 error TEXT,
 content_hash TEXT,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(event_id, user_id, destination),
 CHECK(status <> 'pending' OR attempt_sequence IS NOT NULL)
);
--> statement-breakpoint
-- Each officer's single row becomes the row of the destination it names. A Google row
-- with an event id is on that calendar even when its last update failed, so it counts as
-- sent (with no fingerprint, so the next delivery updates it).
INSERT INTO __new_calendar_deliveries(event_id,user_id,destination,mode,recipient,google_event_id,sequence,status,cancelled,error,content_hash,updated_at)
SELECT d.event_id, d.user_id,
 CASE d.mode WHEN 'ics' THEN 'ics:' || lower(coalesce(d.recipient, ''))
  ELSE 'google:' || coalesce((SELECT lower(t.account_email) FROM google_calendar_tokens t WHERE t.user_id = d.user_id), '') END,
 d.mode, d.recipient, d.google_event_id, d.sequence,
 CASE WHEN d.mode = 'google' AND d.google_event_id IS NOT NULL THEN 'sent' ELSE d.status END,
 0, d.error,
 CASE WHEN d.mode = 'google' AND d.google_event_id IS NOT NULL AND d.status <> 'sent' THEN NULL ELSE d.content_hash END,
 d.updated_at
FROM calendar_deliveries d;
--> statement-breakpoint
-- Before V0.14.1, switching Google -> .ics kept the Google event id on the officer's row
-- so that switching back would update that event. Keep that event as its own Google
-- destination (the connected account's), so switching back updates it instead of
-- creating a second copy, and deleting or cancelling the event withdraws it there too.
-- It has no fingerprint, so the next Google delivery brings it up to date.
INSERT INTO __new_calendar_deliveries(event_id,user_id,destination,mode,recipient,google_event_id,sequence,status,cancelled,error,content_hash,updated_at)
SELECT d.event_id, d.user_id, 'google:' || lower(coalesce(t.account_email, '')),
 'google', NULL, d.google_event_id, d.sequence, 'sent', 0, NULL, NULL, d.updated_at
FROM calendar_deliveries d JOIN google_calendar_tokens t ON t.user_id = d.user_id
WHERE d.mode = 'ics' AND d.google_event_id IS NOT NULL;
--> statement-breakpoint
DROP TABLE calendar_deliveries;
--> statement-breakpoint
ALTER TABLE __new_calendar_deliveries RENAME TO calendar_deliveries;
