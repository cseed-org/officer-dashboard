ALTER TABLE contacts ADD COLUMN has_spoken_for_us INTEGER NOT NULL DEFAULT 0 CHECK(has_spoken_for_us IN (0,1));
--> statement-breakpoint
UPDATE contacts SET has_spoken_for_us = EXISTS (
 SELECT 1 FROM contact_events ce JOIN events e ON e.id = ce.event_id
 WHERE ce.contact_id = contacts.id AND julianday(e.starts_at) < julianday('now')
);
