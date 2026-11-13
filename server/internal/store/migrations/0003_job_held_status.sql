-- M1: add the agent-terminal `held` job status. SQLite cannot alter a CHECK, so the table is
-- rebuilt (nothing references fulfillment_jobs). CHECKs use unqualified columns so they survive the rename.
PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_fulfillment_jobs` (
	`id` text PRIMARY KEY NOT NULL,
	`request_id` text NOT NULL,
	`status` text DEFAULT 'queued' NOT NULL,
	`claimed_by_token_id` text,
	`claimed_at` integer,
	`attempts` integer DEFAULT 0 NOT NULL,
	`last_error` text,
	`result_json` text,
	`payload_json` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`request_id`) REFERENCES `purchase_requests`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`claimed_by_token_id`) REFERENCES `service_tokens`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "fulfillment_jobs_status_check" CHECK("status" IN ('queued', 'claimed', 'held', 'done', 'failed', 'cancelled')),
	CONSTRAINT "fulfillment_jobs_attempts_nonneg" CHECK("attempts" >= 0)
);
--> statement-breakpoint
INSERT INTO `__new_fulfillment_jobs`("id", "request_id", "status", "claimed_by_token_id", "claimed_at", "attempts", "last_error", "result_json", "payload_json", "created_at", "updated_at") SELECT "id", "request_id", "status", "claimed_by_token_id", "claimed_at", "attempts", "last_error", "result_json", "payload_json", "created_at", "updated_at" FROM `fulfillment_jobs`;--> statement-breakpoint
DROP TABLE `fulfillment_jobs`;--> statement-breakpoint
ALTER TABLE `__new_fulfillment_jobs` RENAME TO `fulfillment_jobs`;--> statement-breakpoint
PRAGMA foreign_keys=ON;--> statement-breakpoint
CREATE INDEX `fulfillment_jobs_status_idx` ON `fulfillment_jobs` (`status`);