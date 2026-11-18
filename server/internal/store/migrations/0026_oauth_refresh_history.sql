-- Retain every rotated refresh-token hash, not only the immediately previous one.
-- A replay of any older token must revoke its grant (RFC 9700 §4.14.2).
CREATE TABLE `oauth_refresh_history` (
	`refresh_hash` text PRIMARY KEY NOT NULL,
	`grant_id` text NOT NULL,
	`rotated_at` integer NOT NULL,
	FOREIGN KEY (`grant_id`) REFERENCES `oauth_grants`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `oauth_refresh_history_grant` ON `oauth_refresh_history` (`grant_id`);
--> statement-breakpoint
-- Preserve the one rotated token that existing installations already remember.
INSERT INTO `oauth_refresh_history` (`refresh_hash`, `grant_id`, `rotated_at`)
SELECT `prev_refresh_hash`, `id`, COALESCE(`rotated_at`, `created_at`) FROM `oauth_grants` WHERE `prev_refresh_hash` IS NOT NULL;
