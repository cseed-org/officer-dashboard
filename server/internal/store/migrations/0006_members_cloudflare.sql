CREATE TABLE members_mirror (
 person_id TEXT PRIMARY KEY,
 first_name TEXT NOT NULL,
 last_name TEXT NOT NULL,
 uw_email TEXT,
 major TEXT,
 grad_year TEXT,
 campus TEXT,
 join_date TEXT NOT NULL,
 has_been_officer INTEGER NOT NULL CHECK(has_been_officer IN (0,1)),
 record_json TEXT NOT NULL CHECK(json_valid(record_json))
);
CREATE INDEX members_joined ON members_mirror(join_date DESC,person_id);
CREATE INDEX members_filters ON members_mirror(campus,grad_year,has_been_officer);
CREATE TABLE members_submissions_mirror (
 submission_id TEXT PRIMARY KEY,
 person_id TEXT NOT NULL REFERENCES members_mirror(person_id) ON DELETE CASCADE,
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 received_at TEXT NOT NULL
);
CREATE TABLE cloudflare_snapshots (
 kind TEXT NOT NULL,
 day TEXT NOT NULL,
 data_json TEXT NOT NULL CHECK(json_valid(data_json)),
 synced_at INTEGER NOT NULL,
 PRIMARY KEY(kind,day)
);
CREATE TABLE cloudflare_sync_state (
 kind TEXT PRIMARY KEY,
 last_success INTEGER,
 last_error TEXT
);
