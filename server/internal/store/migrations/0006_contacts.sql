CREATE TABLE contacts (
 id TEXT PRIMARY KEY NOT NULL,
 name TEXT NOT NULL CHECK(length(trim(name)) > 0),
 org TEXT NOT NULL DEFAULT '', role TEXT NOT NULL DEFAULT '',
 type TEXT NOT NULL CHECK(type IN ('speaker','mentor','both')),
 topics TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(topics) AND json_type(topics) = 'array'),
 email TEXT NOT NULL DEFAULT '', linkedin TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
--> statement-breakpoint
CREATE TABLE contact_events (
 contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
 event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 PRIMARY KEY(contact_id,event_id)
);
--> statement-breakpoint
CREATE TABLE contact_outreach (
 id TEXT PRIMARY KEY NOT NULL,
 contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
 officer_id TEXT NOT NULL REFERENCES users(id),
 date TEXT NOT NULL, channel TEXT NOT NULL CHECK(channel IN ('email','linkedin','discord','phone','in_person')),
 outcome TEXT NOT NULL CHECK(outcome IN ('awaiting_reply','interested','confirmed','declined','follow_up')),
 note TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL
);
--> statement-breakpoint
CREATE INDEX contact_outreach_latest ON contact_outreach(contact_id,date DESC,created_at DESC,id DESC);
