CREATE TABLE `accounts` (
	`id` text PRIMARY KEY NOT NULL,
	`code` text NOT NULL,
	`name` text NOT NULL,
	`type` text NOT NULL,
	`normal_balance` text NOT NULL,
	`restriction` text,
	`form990_part` text,
	`form990_line` text,
	`form990_label` text,
	`parent_id` text,
	`budgetable` integer DEFAULT false NOT NULL,
	`active` integer DEFAULT true NOT NULL,
	`description` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`parent_id`) REFERENCES `accounts`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "accounts_type_check" CHECK("accounts"."type" IN ('asset', 'liability', 'net_asset', 'revenue', 'expense')),
	CONSTRAINT "accounts_normal_balance_check" CHECK("accounts"."normal_balance" IN ('debit', 'credit')),
	CONSTRAINT "accounts_restriction_check" CHECK("accounts"."restriction" IS NULL OR "accounts"."restriction" IN ('without_donor', 'with_donor')),
	CONSTRAINT "accounts_form990_part_check" CHECK("accounts"."form990_part" IS NULL OR "accounts"."form990_part" IN ('VIII', 'IX', 'X')),
	CONSTRAINT "accounts_form990_line_pair" CHECK(("accounts"."form990_part" IS NULL) = ("accounts"."form990_line" IS NULL))
);
--> statement-breakpoint
CREATE UNIQUE INDEX `accounts_code_unique` ON `accounts` (`code`);--> statement-breakpoint
CREATE TABLE `approvals` (
	`id` text PRIMARY KEY NOT NULL,
	`request_id` text NOT NULL,
	`director_id` text NOT NULL,
	`decision` text NOT NULL,
	`comment` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`request_id`) REFERENCES `purchase_requests`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`director_id`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "approvals_decision_check" CHECK("approvals"."decision" IN ('approved', 'denied'))
);
--> statement-breakpoint
CREATE INDEX `approvals_request_idx` ON `approvals` (`request_id`);--> statement-breakpoint
CREATE TABLE `attachments` (
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
	CONSTRAINT "attachments_kind_check" CHECK("attachments"."kind" IN ('receipt', 'invoice', 'quote', 'other')),
	CONSTRAINT "attachments_storage_check" CHECK("attachments"."storage" IN ('drive', 'local')),
	CONSTRAINT "attachments_size_nonneg" CHECK("attachments"."size_bytes" >= 0),
	CONSTRAINT "attachments_owner" CHECK("attachments"."request_id" IS NOT NULL OR "attachments"."journal_entry_id" IS NOT NULL)
);
--> statement-breakpoint
CREATE INDEX `attachments_request_idx` ON `attachments` (`request_id`);--> statement-breakpoint
CREATE TABLE `audit_log` (
	`id` text PRIMARY KEY NOT NULL,
	`actor_user_id` text,
	`actor_token_id` text,
	`action` text NOT NULL,
	`entity_type` text NOT NULL,
	`entity_id` text,
	`before_json` text,
	`after_json` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`actor_user_id`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`actor_token_id`) REFERENCES `service_tokens`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE INDEX `audit_entity_idx` ON `audit_log` (`entity_type`,`entity_id`);--> statement-breakpoint
CREATE INDEX `audit_created_idx` ON `audit_log` (`created_at`);--> statement-breakpoint
CREATE TABLE `budgets` (
	`id` text PRIMARY KEY NOT NULL,
	`fiscal_year` integer NOT NULL,
	`account_id` text NOT NULL,
	`fund_id` text,
	`amount_cents` integer NOT NULL,
	`notes` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`account_id`) REFERENCES `accounts`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`fund_id`) REFERENCES `funds`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "budgets_amount_nonneg" CHECK("budgets"."amount_cents" >= 0)
);
--> statement-breakpoint
CREATE INDEX `budgets_fy_idx` ON `budgets` (`fiscal_year`);--> statement-breakpoint
CREATE TABLE `contributions` (
	`id` text PRIMARY KEY NOT NULL,
	`donor_id` text NOT NULL,
	`fund_id` text NOT NULL,
	`received_date` text NOT NULL,
	`kind` text NOT NULL,
	`amount_cents` integer NOT NULL,
	`in_kind_description` text,
	`quid_pro_quo_cents` integer DEFAULT 0 NOT NULL,
	`goods_services_description` text,
	`acknowledgment_required` integer DEFAULT false NOT NULL,
	`acknowledged_at` integer,
	`journal_entry_id` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`donor_id`) REFERENCES `donors`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`fund_id`) REFERENCES `funds`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`journal_entry_id`) REFERENCES `journal_entries`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "contributions_kind_check" CHECK("contributions"."kind" IN ('cash', 'check', 'online', 'in_kind', 'stock', 'other')),
	CONSTRAINT "contributions_amount_pos" CHECK("contributions"."amount_cents" > 0),
	CONSTRAINT "contributions_qpq_range" CHECK("contributions"."quid_pro_quo_cents" >= 0 AND "contributions"."quid_pro_quo_cents" <= "contributions"."amount_cents"),
	CONSTRAINT "contributions_in_kind_desc" CHECK("contributions"."kind" <> 'in_kind' OR "contributions"."in_kind_description" IS NOT NULL)
);
--> statement-breakpoint
CREATE INDEX `contributions_donor_idx` ON `contributions` (`donor_id`);--> statement-breakpoint
CREATE TABLE `donors` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`type` text NOT NULL,
	`email` text,
	`address` text,
	`notes` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	CONSTRAINT "donors_type_check" CHECK("donors"."type" IN ('individual', 'organization', 'foundation', 'government', 'corporate'))
);
--> statement-breakpoint
CREATE TABLE `event_guests` (
	`id` text PRIMARY KEY NOT NULL,
	`event_id` text NOT NULL,
	`email` text NOT NULL,
	`name` text,
	`luma_guest_id` text,
	`approval_status` text,
	`registered_at` integer,
	`checked_in_at` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`event_id`) REFERENCES `events`(`id`) ON UPDATE no action ON DELETE cascade,
	CONSTRAINT "event_guests_email_lower" CHECK("event_guests"."email" = lower("event_guests"."email"))
);
--> statement-breakpoint
CREATE UNIQUE INDEX `event_guests_event_email_uq` ON `event_guests` (`event_id`,`email`);--> statement-breakpoint
CREATE TABLE `events` (
	`id` text PRIMARY KEY NOT NULL,
	`luma_id` text,
	`name` text NOT NULL,
	`starts_at` text NOT NULL,
	`ends_at` text,
	`location` text,
	`url` text,
	`host_user_id` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`host_user_id`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `events_luma_id_unique` ON `events` (`luma_id`);--> statement-breakpoint
CREATE INDEX `events_starts_idx` ON `events` (`starts_at`);--> statement-breakpoint
CREATE TABLE `fiscal_periods` (
	`id` text PRIMARY KEY NOT NULL,
	`fiscal_year` integer NOT NULL,
	`name` text NOT NULL,
	`start_date` text NOT NULL,
	`end_date` text NOT NULL,
	`status` text DEFAULT 'open' NOT NULL,
	`closed_at` integer,
	`closed_by` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`closed_by`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "fiscal_periods_status_check" CHECK("fiscal_periods"."status" IN ('open', 'closed', 'locked')),
	CONSTRAINT "fiscal_periods_dates" CHECK("fiscal_periods"."end_date" >= "fiscal_periods"."start_date")
);
--> statement-breakpoint
CREATE UNIQUE INDEX `fiscal_periods_range_uq` ON `fiscal_periods` (`start_date`,`end_date`);--> statement-breakpoint
CREATE TABLE `fulfillment_jobs` (
	`id` text PRIMARY KEY NOT NULL,
	`request_id` text NOT NULL,
	`status` text DEFAULT 'queued' NOT NULL,
	`claimed_by_token_id` text,
	`claimed_at` integer,
	`attempts` integer DEFAULT 0 NOT NULL,
	`last_error` text,
	`result_json` text,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`request_id`) REFERENCES `purchase_requests`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`claimed_by_token_id`) REFERENCES `service_tokens`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "fulfillment_jobs_status_check" CHECK("fulfillment_jobs"."status" IN ('queued', 'claimed', 'done', 'failed', 'cancelled')),
	CONSTRAINT "fulfillment_jobs_attempts_nonneg" CHECK("fulfillment_jobs"."attempts" >= 0)
);
--> statement-breakpoint
CREATE INDEX `fulfillment_jobs_status_idx` ON `fulfillment_jobs` (`status`);--> statement-breakpoint
CREATE TABLE `funds` (
	`id` text PRIMARY KEY NOT NULL,
	`code` text NOT NULL,
	`name` text NOT NULL,
	`kind` text NOT NULL,
	`restriction` text NOT NULL,
	`purpose` text,
	`donor_id` text,
	`start_date` text,
	`end_date` text,
	`amount_awarded_cents` integer,
	`released_at` integer,
	`active` integer DEFAULT true NOT NULL,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`donor_id`) REFERENCES `donors`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "funds_kind_check" CHECK("funds"."kind" IN ('general', 'grant', 'sponsor', 'other')),
	CONSTRAINT "funds_restriction_check" CHECK("funds"."restriction" IN ('without_donor', 'with_donor')),
	CONSTRAINT "funds_amount_nonneg" CHECK("funds"."amount_awarded_cents" IS NULL OR "funds"."amount_awarded_cents" >= 0),
	CONSTRAINT "funds_dates" CHECK("funds"."end_date" IS NULL OR "funds"."start_date" IS NULL OR "funds"."end_date" >= "funds"."start_date")
);
--> statement-breakpoint
CREATE UNIQUE INDEX `funds_code_unique` ON `funds` (`code`);--> statement-breakpoint
CREATE TABLE `journal_entries` (
	`id` text PRIMARY KEY NOT NULL,
	`entry_date` text NOT NULL,
	`memo` text NOT NULL,
	`status` text DEFAULT 'draft' NOT NULL,
	`source` text DEFAULT 'manual' NOT NULL,
	`purchase_request_id` text,
	`contribution_id` text,
	`reverses_entry_id` text,
	`created_by` text,
	`posted_by` text,
	`posted_at` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`purchase_request_id`) REFERENCES `purchase_requests`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`reverses_entry_id`) REFERENCES `journal_entries`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`created_by`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`posted_by`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "journal_entries_status_check" CHECK("journal_entries"."status" IN ('draft', 'posted')),
	CONSTRAINT "journal_entries_source_check" CHECK("journal_entries"."source" IN ('manual', 'purchase_request', 'contribution', 'reversal', 'release', 'import', 'opening_balance')),
	CONSTRAINT "journal_entries_posted_at" CHECK(("journal_entries"."status" = 'posted') = ("journal_entries"."posted_at" IS NOT NULL))
);
--> statement-breakpoint
CREATE INDEX `journal_entries_date_idx` ON `journal_entries` (`entry_date`);--> statement-breakpoint
CREATE INDEX `journal_entries_request_idx` ON `journal_entries` (`purchase_request_id`);--> statement-breakpoint
CREATE TABLE `journal_lines` (
	`id` text PRIMARY KEY NOT NULL,
	`entry_id` text NOT NULL,
	`line_no` integer NOT NULL,
	`account_id` text NOT NULL,
	`fund_id` text NOT NULL,
	`functional_class` text,
	`debit_cents` integer DEFAULT 0 NOT NULL,
	`credit_cents` integer DEFAULT 0 NOT NULL,
	`memo` text,
	FOREIGN KEY (`entry_id`) REFERENCES `journal_entries`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`account_id`) REFERENCES `accounts`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`fund_id`) REFERENCES `funds`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "journal_lines_debit_nonneg" CHECK("journal_lines"."debit_cents" >= 0),
	CONSTRAINT "journal_lines_credit_nonneg" CHECK("journal_lines"."credit_cents" >= 0),
	CONSTRAINT "journal_lines_one_side" CHECK(("journal_lines"."debit_cents" = 0) <> ("journal_lines"."credit_cents" = 0)),
	CONSTRAINT "journal_lines_functional_class_check" CHECK("journal_lines"."functional_class" IS NULL OR "journal_lines"."functional_class" IN ('program', 'mgmt_general', 'fundraising'))
);
--> statement-breakpoint
CREATE UNIQUE INDEX `journal_lines_entry_line_uq` ON `journal_lines` (`entry_id`,`line_no`);--> statement-breakpoint
CREATE INDEX `journal_lines_account_idx` ON `journal_lines` (`account_id`);--> statement-breakpoint
CREATE INDEX `journal_lines_fund_idx` ON `journal_lines` (`fund_id`);--> statement-breakpoint
CREATE TABLE `purchase_requests` (
	`id` text PRIMARY KEY NOT NULL,
	`number` text NOT NULL,
	`requester_id` text NOT NULL,
	`type` text NOT NULL,
	`title` text NOT NULL,
	`vendor` text NOT NULL,
	`item_url` text,
	`quantity` integer DEFAULT 1 NOT NULL,
	`unit_cost_cents` integer NOT NULL,
	`tax_ship_cents` integer DEFAULT 0 NOT NULL,
	`total_cents` integer NOT NULL,
	`expense_account_id` text NOT NULL,
	`fund_id` text NOT NULL,
	`event_id` text,
	`functional_class` text DEFAULT 'program' NOT NULL,
	`justification` text NOT NULL,
	`needed_by` text,
	`status` text DEFAULT 'draft' NOT NULL,
	`submitted_at` integer,
	`decided_at` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`requester_id`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`expense_account_id`) REFERENCES `accounts`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`fund_id`) REFERENCES `funds`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`event_id`) REFERENCES `events`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "purchase_requests_type_check" CHECK("purchase_requests"."type" IN ('reimbursement', 'club_purchase')),
	CONSTRAINT "purchase_requests_status_check" CHECK("purchase_requests"."status" IN ('draft', 'submitted', 'approved', 'denied', 'fulfilling', 'fulfilled', 'reimbursed', 'closed', 'cancelled')),
	CONSTRAINT "purchase_requests_fc_check" CHECK("purchase_requests"."functional_class" IN ('program', 'mgmt_general', 'fundraising')),
	CONSTRAINT "purchase_requests_qty_pos" CHECK("purchase_requests"."quantity" > 0),
	CONSTRAINT "purchase_requests_unit_nonneg" CHECK("purchase_requests"."unit_cost_cents" >= 0),
	CONSTRAINT "purchase_requests_taxship_nonneg" CHECK("purchase_requests"."tax_ship_cents" >= 0),
	CONSTRAINT "purchase_requests_total" CHECK("purchase_requests"."total_cents" = "purchase_requests"."quantity" * "purchase_requests"."unit_cost_cents" + "purchase_requests"."tax_ship_cents")
);
--> statement-breakpoint
CREATE UNIQUE INDEX `purchase_requests_number_unique` ON `purchase_requests` (`number`);--> statement-breakpoint
CREATE INDEX `purchase_requests_requester_idx` ON `purchase_requests` (`requester_id`);--> statement-breakpoint
CREATE INDEX `purchase_requests_status_idx` ON `purchase_requests` (`status`);--> statement-breakpoint
CREATE TABLE `reflections` (
	`id` text PRIMARY KEY NOT NULL,
	`event_id` text NOT NULL,
	`email` text NOT NULL,
	`submitted_at` integer NOT NULL,
	`rating` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`event_id`) REFERENCES `events`(`id`) ON UPDATE no action ON DELETE cascade,
	CONSTRAINT "reflections_email_lower" CHECK("reflections"."email" = lower("reflections"."email")),
	CONSTRAINT "reflections_rating_range" CHECK("reflections"."rating" IS NULL OR "reflections"."rating" BETWEEN 1 AND 5)
);
--> statement-breakpoint
CREATE UNIQUE INDEX `reflections_event_email_uq` ON `reflections` (`event_id`,`email`);--> statement-breakpoint
CREATE TABLE `service_tokens` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`token_hash` text NOT NULL,
	`prefix` text NOT NULL,
	`scopes` text NOT NULL,
	`created_by` text NOT NULL,
	`last_used_at` integer,
	`expires_at` integer,
	`revoked_at` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	FOREIGN KEY (`created_by`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE no action,
	CONSTRAINT "service_tokens_scopes_json" CHECK(json_valid("service_tokens"."scopes"))
);
--> statement-breakpoint
CREATE UNIQUE INDEX `service_tokens_token_hash_unique` ON `service_tokens` (`token_hash`);--> statement-breakpoint
CREATE TABLE `sessions` (
	`id` text PRIMARY KEY NOT NULL,
	`user_id` text NOT NULL,
	`expires_at` integer NOT NULL,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`user_agent` text,
	FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `sessions_user_idx` ON `sessions` (`user_id`);--> statement-breakpoint
CREATE TABLE `users` (
	`id` text PRIMARY KEY NOT NULL,
	`discord_id` text NOT NULL,
	`name` text NOT NULL,
	`email` text,
	`avatar_url` text,
	`role` text DEFAULT 'officer' NOT NULL,
	`active` integer DEFAULT true NOT NULL,
	`title` text,
	`last_login_at` integer,
	`created_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	`updated_at` integer DEFAULT (unixepoch('subsec') * 1000) NOT NULL,
	CONSTRAINT "users_role_check" CHECK("users"."role" IN ('director', 'officer'))
);
--> statement-breakpoint
CREATE UNIQUE INDEX `users_discord_id_unique` ON `users` (`discord_id`);