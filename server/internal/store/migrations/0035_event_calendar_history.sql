-- Keep the last fixed invitation window so tentative transitions can withdraw
-- existing per-officer invitations, including retries after a restart.
CREATE TABLE event_calendar_history (
 event_id TEXT PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot))
);
INSERT INTO event_calendar_history SELECT id,json_object('id',id,'name',name,'startsAt',starts_at,'endsAt',ends_at,'location',location,'url',url) FROM events WHERE starts_at IS NOT NULL;
CREATE TRIGGER event_calendar_history_insert AFTER INSERT ON events WHEN NEW.starts_at IS NOT NULL BEGIN
 INSERT INTO event_calendar_history VALUES(NEW.id,json_object('id',NEW.id,'name',NEW.name,'startsAt',NEW.starts_at,'endsAt',NEW.ends_at,'location',NEW.location,'url',NEW.url));
END;
CREATE TRIGGER event_calendar_history_update AFTER UPDATE ON events WHEN NEW.starts_at IS NOT NULL BEGIN
 INSERT INTO event_calendar_history VALUES(NEW.id,json_object('id',NEW.id,'name',NEW.name,'startsAt',NEW.starts_at,'endsAt',NEW.ends_at,'location',NEW.location,'url',NEW.url))
 ON CONFLICT(event_id) DO UPDATE SET snapshot=excluded.snapshot;
END;
