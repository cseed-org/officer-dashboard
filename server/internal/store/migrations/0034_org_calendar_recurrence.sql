ALTER TABLE org_cal_events ADD COLUMN recurrence TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(recurrence));
ALTER TABLE org_cal_events ADD COLUMN timezone TEXT NOT NULL DEFAULT 'America/Los_Angeles';
CREATE TABLE org_cal_overrides (
 calendar_key TEXT NOT NULL REFERENCES org_calendars(key), google_event_id TEXT NOT NULL,
 master_google_id TEXT NOT NULL, original_start TEXT NOT NULL, payload TEXT NOT NULL CHECK(json_valid(payload)),
 PRIMARY KEY(calendar_key,google_event_id)
);
