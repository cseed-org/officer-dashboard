CREATE TABLE snapshots (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('hourly','daily','d1-weekly')),
 created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 sha256 TEXT NOT NULL, size INTEGER NOT NULL CHECK(size BETWEEN 1 AND 24700000),
 chunks INTEGER NOT NULL CHECK(chunks BETWEEN 1 AND 13), committed INTEGER NOT NULL DEFAULT 0,
 allocation INTEGER NOT NULL, write_cost INTEGER NOT NULL
);
CREATE TABLE chunks (
 snapshot_id TEXT NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
 n INTEGER NOT NULL, blob BLOB NOT NULL CHECK(length(blob) BETWEEN 1 AND 1900000),
 PRIMARY KEY(snapshot_id,n)
);
CREATE TABLE budgets (day TEXT PRIMARY KEY, writes INTEGER NOT NULL DEFAULT 0, uploads INTEGER NOT NULL DEFAULT 0);
-- Reserve a conservative lifetime write allowance, including cleanup, atomically.
-- Half the account allowance belongs to membership and other workloads.
CREATE TRIGGER reserve BEFORE INSERT ON snapshots BEGIN
 SELECT CASE WHEN (SELECT count(*) FROM snapshots) >= 100
  OR COALESCE((SELECT sum(allocation) FROM snapshots),0) + NEW.allocation > 450000000
  OR COALESCE((SELECT writes FROM budgets WHERE day=date('now')),0) + NEW.write_cost > 40000
  OR COALESCE((SELECT uploads FROM budgets WHERE day=date('now')),0) >= 200
  THEN RAISE(ABORT,'backup budget exhausted') END;
 INSERT INTO budgets(day,writes,uploads) VALUES(date('now'),NEW.write_cost,1)
 ON CONFLICT(day) DO UPDATE SET writes=writes+NEW.write_cost,uploads=uploads+1;
END;
