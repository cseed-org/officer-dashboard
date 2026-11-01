-- A short-lived checked row lets a D1 batch assert preconditions atomically before writes.
CREATE TABLE mutation_guards (id TEXT PRIMARY KEY, ok INTEGER NOT NULL CHECK(ok = 1));
--> statement-breakpoint
ALTER TABLE journal_entries ADD COLUMN settlement_key TEXT;
--> statement-breakpoint
CREATE UNIQUE INDEX purchase_settlement_once ON journal_entries(settlement_key) WHERE settlement_key IS NOT NULL;
--> statement-breakpoint
CREATE UNIQUE INDEX request_one_approval ON approvals(request_id) WHERE decision = 'approved';
--> statement-breakpoint
CREATE TRIGGER users_last_director BEFORE UPDATE OF active, role ON users
WHEN OLD.active = 1 AND OLD.role = 'director' AND (NEW.active = 0 OR NEW.role <> 'director')
 AND NOT EXISTS (SELECT 1 FROM users WHERE id <> OLD.id AND active = 1 AND role = 'director')
BEGIN SELECT RAISE(ABORT, 'cannot remove last active director'); END;
--> statement-breakpoint
CREATE TRIGGER period_no_reopen BEFORE UPDATE ON fiscal_periods
WHEN (OLD.status = 'locked') OR (OLD.status = 'closed' AND (NEW.status <> 'locked' OR NEW.start_date <> OLD.start_date OR NEW.end_date <> OLD.end_date))
BEGIN SELECT RAISE(ABORT, 'closed period cannot reopen or change dates'); END;
--> statement-breakpoint
CREATE TRIGGER period_no_delete_closed BEFORE DELETE ON fiscal_periods
WHEN OLD.status <> 'open'
BEGIN SELECT RAISE(ABORT, 'closed period cannot be deleted'); END;
--> statement-breakpoint
ALTER TABLE fulfillment_jobs ADD COLUMN payload_json TEXT;
--> statement-breakpoint
CREATE UNIQUE INDEX journal_reversal_once ON journal_entries(reverses_entry_id) WHERE reverses_entry_id IS NOT NULL;

--> statement-breakpoint
-- Development-only FileStore backing; production uploads use Google Drive.
CREATE TABLE local_receipt_files (id TEXT PRIMARY KEY, mime_type TEXT NOT NULL, body BLOB NOT NULL);
