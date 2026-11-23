-- Last pulled Google RSVP per attendee email ({"email":"accepted"}), sent back on push
-- so a dashboard edit does not reset guests' responses.
ALTER TABLE org_cal_events ADD COLUMN attendee_status TEXT NOT NULL DEFAULT '{}';
