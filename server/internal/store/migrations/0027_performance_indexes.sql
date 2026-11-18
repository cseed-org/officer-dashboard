-- Growing history tables: ordered cursors, finance before-images and FK/cleanup lookups.
CREATE INDEX contributions_journal_entry_idx ON contributions(journal_entry_id);
--> statement-breakpoint
CREATE INDEX audit_type_created_idx ON audit_log(entity_type, created_at, id);
--> statement-breakpoint
CREATE INDEX audit_created_id_idx ON audit_log(created_at, id);
--> statement-breakpoint
DROP INDEX audit_created_idx;
--> statement-breakpoint
CREATE INDEX purchase_requests_created_id_idx ON purchase_requests(created_at, id);
--> statement-breakpoint
CREATE INDEX fulfillment_jobs_request_idx ON fulfillment_jobs(request_id);
--> statement-breakpoint
CREATE INDEX oauth_grants_client ON oauth_grants(client_id);
--> statement-breakpoint
CREATE INDEX oauth_codes_client ON oauth_codes(client_id);
--> statement-breakpoint
CREATE INDEX buildspace_activity_purchase_idx ON buildspace_launchpad_activity(purchase_id);
