-- Request numbers come from a per-fiscal-year counter so a number is never handed out twice,
-- even after the newest draft is deleted (max(number)+1 would reuse it).
CREATE TABLE `request_counters` (
	`fiscal_year` integer PRIMARY KEY NOT NULL,
	`last` integer NOT NULL,
	CONSTRAINT "request_counters_last_nonneg" CHECK(`last` >= 0)
);
--> statement-breakpoint
INSERT INTO `request_counters` (`fiscal_year`, `last`)
SELECT CAST(substr(number, 1, 4) AS INTEGER), max(CAST(substr(number, 6) AS INTEGER))
FROM purchase_requests
WHERE number GLOB '[0-9][0-9][0-9][0-9]-[0-9]*' AND substr(number, 6) NOT GLOB '*[^0-9]*'
GROUP BY substr(number, 1, 4);
