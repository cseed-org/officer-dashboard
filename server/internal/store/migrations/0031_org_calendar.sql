CREATE TABLE org_calendars (
 key TEXT PRIMARY KEY CHECK(key IN ('internal','external','programs','events')),
 name TEXT NOT NULL, google_calendar_id TEXT, hue TEXT NOT NULL, locked INTEGER NOT NULL CHECK(locked IN (0,1))
);
INSERT INTO org_calendars VALUES
 ('internal','Internal Meetings',NULL,'blue',0),('external','External Meetings',NULL,'teal',0),
 ('programs','Weekly Programs',NULL,'violet',1),('events','Events',NULL,'coral',1);
CREATE TABLE org_google_account (
 id INTEGER PRIMARY KEY CHECK(id=1), subject TEXT NOT NULL, email TEXT NOT NULL,
 sealed_refresh TEXT NOT NULL, sealed_access TEXT NOT NULL, expires_at INTEGER NOT NULL, connected_by TEXT REFERENCES users(id)
);
CREATE TABLE org_cal_events (
 id TEXT PRIMARY KEY, calendar_key TEXT NOT NULL REFERENCES org_calendars(key),
 title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', location TEXT NOT NULL DEFAULT '',
 starts_at TEXT, ends_at TEXT, all_day INTEGER NOT NULL DEFAULT 0,
 tentative_from TEXT, tentative_to TEXT, rrule TEXT,
 reminders TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(reminders)), send_invites INTEGER NOT NULL DEFAULT 0,
 source_kind TEXT NOT NULL DEFAULT 'manual' CHECK(source_kind IN ('manual','event','program')),
 source_id TEXT, google_event_id TEXT, google_etag TEXT,
 created_by TEXT REFERENCES users(id), updated_by TEXT REFERENCES users(id),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER,
 revision INTEGER NOT NULL DEFAULT 1,
 CHECK((starts_at IS NOT NULL AND ends_at IS NOT NULL) OR (tentative_from IS NOT NULL AND tentative_to >= tentative_from)),
 UNIQUE(source_kind,source_id), UNIQUE(calendar_key,google_event_id)
);
CREATE INDEX org_cal_events_range_idx ON org_cal_events(calendar_key,starts_at) WHERE deleted_at IS NULL;
CREATE TABLE org_cal_invitees (
 event_id TEXT NOT NULL REFERENCES org_cal_events(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('subteam','user','email')), ref TEXT NOT NULL,
 PRIMARY KEY(event_id,kind,ref)
);
CREATE TABLE org_cal_outbox (
 event_id TEXT PRIMARY KEY REFERENCES org_cal_events(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at INTEGER NOT NULL DEFAULT 0, last_error TEXT
);
CREATE TABLE org_cal_sync_state (
 calendar_key TEXT PRIMARY KEY REFERENCES org_calendars(key), sync_token TEXT
);
-- A roster change invalidates the ACL cache in the same transaction.
CREATE TABLE org_cal_acl_state (id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL DEFAULT 0);
INSERT INTO org_cal_acl_state VALUES(1,0);
CREATE TRIGGER org_cal_acl_user_insert AFTER INSERT ON users BEGIN UPDATE org_cal_acl_state SET generation=generation+1; END;
CREATE TRIGGER org_cal_acl_user_update AFTER UPDATE OF email,active ON users BEGIN UPDATE org_cal_acl_state SET generation=generation+1; END;
CREATE TRIGGER org_cal_acl_user_delete AFTER DELETE ON users BEGIN UPDATE org_cal_acl_state SET generation=generation+1; END;
