-- A monotonically increasing version invalidates the email cache even for direct
-- imports, single-row corrections and replacements, including within a transaction.
CREATE TABLE members_generation (id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL);
--> statement-breakpoint
INSERT INTO members_generation VALUES(1,0);
--> statement-breakpoint
CREATE TRIGGER members_generation_insert AFTER INSERT ON members_mirror BEGIN UPDATE members_generation SET generation=generation+1 WHERE id=1; END;
--> statement-breakpoint
CREATE TRIGGER members_generation_update AFTER UPDATE ON members_mirror BEGIN UPDATE members_generation SET generation=generation+1 WHERE id=1; END;
--> statement-breakpoint
CREATE TRIGGER members_generation_delete AFTER DELETE ON members_mirror BEGIN UPDATE members_generation SET generation=generation+1 WHERE id=1; END;
