CREATE TABLE program_tracks (id TEXT PRIMARY KEY, key TEXT NOT NULL UNIQUE, name TEXT NOT NULL, hue TEXT NOT NULL);
INSERT INTO program_tracks VALUES ('buildspace','buildspace','buildspace','violet'),('buildher','buildher','buildHER','coral');
CREATE TABLE programs (
 id TEXT PRIMARY KEY, track_id TEXT NOT NULL REFERENCES program_tracks(id), name TEXT NOT NULL,
 weekday INTEGER NOT NULL CHECK(weekday BETWEEN 0 AND 6), start_time TEXT NOT NULL,
 duration_min INTEGER NOT NULL CHECK(duration_min BETWEEN 1 AND 1440),
 location TEXT NOT NULL DEFAULT '', starts_on TEXT NOT NULL, ends_on TEXT NOT NULL CHECK(ends_on>=starts_on),
 description TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE program_exceptions (
 program_id TEXT NOT NULL REFERENCES programs(id) ON DELETE CASCADE, on_date TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('skip','move')), new_starts_at TEXT, new_ends_at TEXT, new_location TEXT,
 CHECK(kind='skip' OR (new_starts_at IS NOT NULL AND new_ends_at IS NOT NULL)), PRIMARY KEY(program_id,on_date)
);
