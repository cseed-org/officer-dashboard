-- Future r2_* schema changes must preserve MergeR2BeforeRollback compatibility.
CREATE TABLE `attachments_new` (
	`id` text PRIMARY KEY NOT NULL,
	`request_id` text,
	`journal_entry_id` text,
	`kind` text NOT NULL,
	`filename` text NOT NULL,
	`mime_type` text NOT NULL,
	`size_bytes` integer NOT NULL,
	`storage` text NOT NULL,
	`storage_key` text NOT NULL,
	`web_view_link` text,
	`uploaded_by` text NOT NULL,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`request_id`) REFERENCES `purchase_requests`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`journal_entry_id`) REFERENCES `journal_entries`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`uploaded_by`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "attachments_kind_check" CHECK("attachments_new"."kind" IN ('receipt', 'invoice', 'quote', 'other')),
	CONSTRAINT "attachments_storage_check" CHECK("attachments_new"."storage" IN ('drive', 'local', 'r2')),
	CONSTRAINT "attachments_size_nonneg" CHECK("attachments_new"."size_bytes" >= 0),
	CONSTRAINT "attachments_size_int" CHECK(typeof("attachments_new"."size_bytes") = 'integer'),
	CONSTRAINT "attachments_owner" CHECK("attachments_new"."request_id" IS NOT NULL OR "attachments_new"."journal_entry_id" IS NOT NULL)
);

--> statement-breakpoint
INSERT INTO attachments_new SELECT * FROM attachments;
--> statement-breakpoint
DROP TABLE attachments;
--> statement-breakpoint
ALTER TABLE attachments_new RENAME TO attachments;
--> statement-breakpoint
CREATE INDEX attachments_request_idx ON attachments(request_id);
--> statement-breakpoint
CREATE TABLE design_request_files_new (
 id TEXT PRIMARY KEY NOT NULL,
 request_id TEXT NOT NULL REFERENCES design_requests(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('reference','review','delivered')),
 filename TEXT NOT NULL CHECK(length(filename) BETWEEN 1 AND 255),
 mime_type TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
 storage TEXT NOT NULL CHECK(storage IN ('drive','local','r2')),
 storage_key TEXT NOT NULL,
 web_view_link TEXT,
 uploaded_by TEXT NOT NULL REFERENCES users(id),
 created_at INTEGER NOT NULL
);

--> statement-breakpoint
INSERT INTO design_request_files_new SELECT * FROM design_request_files;
--> statement-breakpoint
DROP TABLE design_request_files;
--> statement-breakpoint
ALTER TABLE design_request_files_new RENAME TO design_request_files;
--> statement-breakpoint
CREATE INDEX design_request_files_request_idx ON design_request_files(request_id, created_at);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_budget (
 id INTEGER PRIMARY KEY CHECK(id=1), bucket TEXT NOT NULL,
 bytes INTEGER NOT NULL DEFAULT 0 CHECK(bytes>=0), reconciled_at INTEGER NOT NULL DEFAULT 0
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_month (
 month TEXT PRIMARY KEY, class_a INTEGER NOT NULL DEFAULT 0, class_b INTEGER NOT NULL DEFAULT 0,
 deletes INTEGER NOT NULL DEFAULT 0, alerted INTEGER NOT NULL DEFAULT 0, alert_attempt_at INTEGER NOT NULL DEFAULT 0, alert_claim_mask INTEGER NOT NULL DEFAULT 0
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_day(day TEXT PRIMARY KEY, class_a INTEGER NOT NULL DEFAULT 0, class_b INTEGER NOT NULL DEFAULT 0);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_restore_lock(id INTEGER PRIMARY KEY CHECK(id=1), until_utc TEXT NOT NULL);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_objects (key TEXT PRIMARY KEY, bytes INTEGER NOT NULL CHECK(bytes>0), reserved_at INTEGER NOT NULL DEFAULT 0);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_user_day (user_id TEXT NOT NULL, day TEXT NOT NULL, uploads INTEGER NOT NULL, PRIMARY KEY(user_id,day));
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS r2_migrations(source TEXT PRIMARY KEY, key TEXT NOT NULL UNIQUE, uploaded INTEGER NOT NULL DEFAULT 0);
