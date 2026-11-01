-- Integrity rules Drizzle can't express. Tested in tests/db/integrity-triggers.test.ts.
--
-- Journal lifecycle: an entry is created as `draft`, lines are added, then one UPDATE sets
-- status = 'posted' (and posted_at). Posting checks the entry is balanced and its date is
-- not in a closed/locked fiscal period. After posting, the entry and its lines are frozen;
-- corrections are new reversing entries (journal_entries.reverses_entry_id).
--
-- REPLACE / UPSERT: with recursive_triggers off (as on D1), INSERT OR REPLACE and
-- UPDATE OR REPLACE delete a conflicting row WITHOUT firing its DELETE triggers. So each
-- protected table also has guards that refuse any insert or key change landing on a
-- protected row's primary key or unique key. (ON CONFLICT DO UPDATE runs the UPDATE
-- triggers and is blocked by those.)

-- Budgets: one row per fiscal year + account + fund, where NULL fund means "all funds".
CREATE UNIQUE INDEX `budgets_fy_account_fund_uq`
  ON `budgets` (`fiscal_year`, `account_id`, coalesce(`fund_id`, ''));
--> statement-breakpoint

CREATE TRIGGER `journal_entries_insert_draft`
BEFORE INSERT ON `journal_entries`
WHEN NEW.status <> 'draft'
BEGIN
  SELECT RAISE(ABORT, 'journal entry must be created as draft, then posted');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_entries_post_balanced`
BEFORE UPDATE OF `status` ON `journal_entries`
WHEN OLD.status = 'draft' AND NEW.status = 'posted'
BEGIN
  SELECT RAISE(ABORT, 'journal entry needs at least two lines to post')
  WHERE (SELECT count(*) FROM journal_lines WHERE entry_id = NEW.id) < 2;
  SELECT RAISE(ABORT, 'journal entry is unbalanced: debits must equal credits')
  WHERE (SELECT coalesce(sum(debit_cents), 0) - coalesce(sum(credit_cents), 0)
         FROM journal_lines WHERE entry_id = NEW.id) <> 0;
  SELECT RAISE(ABORT, 'fiscal period is closed for this entry date')
  WHERE EXISTS (
    SELECT 1 FROM fiscal_periods
    WHERE status IN ('closed', 'locked')
      AND NEW.entry_date BETWEEN start_date AND end_date
  );
END;
--> statement-breakpoint

CREATE TRIGGER `journal_entries_posted_no_update`
BEFORE UPDATE ON `journal_entries`
WHEN OLD.status = 'posted'
BEGIN
  SELECT RAISE(ABORT, 'posted journal entry is immutable; post a reversing entry instead');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_entries_posted_no_delete`
BEFORE DELETE ON `journal_entries`
WHEN OLD.status = 'posted'
BEGIN
  SELECT RAISE(ABORT, 'posted journal entry is immutable; post a reversing entry instead');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_lines_posted_no_insert`
BEFORE INSERT ON `journal_lines`
WHEN (SELECT status FROM journal_entries WHERE id = NEW.entry_id) = 'posted'
BEGIN
  SELECT RAISE(ABORT, 'posted journal entry is immutable; cannot add lines');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_lines_posted_no_update`
BEFORE UPDATE ON `journal_lines`
WHEN (SELECT status FROM journal_entries WHERE id = OLD.entry_id) = 'posted'
  OR (SELECT status FROM journal_entries WHERE id = NEW.entry_id) = 'posted'
BEGIN
  SELECT RAISE(ABORT, 'posted journal line is immutable');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_lines_posted_no_delete`
BEFORE DELETE ON `journal_lines`
WHEN (SELECT status FROM journal_entries WHERE id = OLD.entry_id) = 'posted'
BEGIN
  SELECT RAISE(ABORT, 'posted journal line is immutable');
END;
--> statement-breakpoint

CREATE TRIGGER `audit_log_no_update`
BEFORE UPDATE ON `audit_log`
BEGIN
  SELECT RAISE(ABORT, 'audit_log is append-only');
END;
--> statement-breakpoint

CREATE TRIGGER `audit_log_no_delete`
BEFORE DELETE ON `audit_log`
BEGIN
  SELECT RAISE(ABORT, 'audit_log is append-only');
END;
--> statement-breakpoint

-- ── REPLACE guards (see header) ──

CREATE TRIGGER `journal_entries_no_replace_posted`
BEFORE INSERT ON `journal_entries`
WHEN EXISTS (SELECT 1 FROM journal_entries WHERE id = NEW.id AND status = 'posted')
BEGIN
  SELECT RAISE(ABORT, 'posted journal entry is immutable; cannot be replaced');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_entries_no_rekey_onto_posted`
BEFORE UPDATE OF `id` ON `journal_entries`
WHEN NEW.id <> OLD.id AND EXISTS (SELECT 1 FROM journal_entries WHERE id = NEW.id AND status = 'posted')
BEGIN
  SELECT RAISE(ABORT, 'posted journal entry is immutable; cannot be replaced');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_lines_no_replace_posted`
BEFORE INSERT ON `journal_lines`
WHEN EXISTS (
  SELECT 1 FROM journal_lines l JOIN journal_entries e ON e.id = l.entry_id
  WHERE e.status = 'posted' AND (l.id = NEW.id OR (l.entry_id = NEW.entry_id AND l.line_no = NEW.line_no))
)
BEGIN
  SELECT RAISE(ABORT, 'posted journal line is immutable; cannot be replaced');
END;
--> statement-breakpoint

CREATE TRIGGER `journal_lines_no_rekey_onto_posted`
BEFORE UPDATE OF `id`, `entry_id`, `line_no` ON `journal_lines`
WHEN EXISTS (
  SELECT 1 FROM journal_lines l JOIN journal_entries e ON e.id = l.entry_id
  WHERE e.status = 'posted' AND l.id <> OLD.id
    AND (l.id = NEW.id OR (l.entry_id = NEW.entry_id AND l.line_no = NEW.line_no))
)
BEGIN
  SELECT RAISE(ABORT, 'posted journal line is immutable; cannot be replaced');
END;
--> statement-breakpoint

CREATE TRIGGER `audit_log_no_replace`
BEFORE INSERT ON `audit_log`
WHEN EXISTS (SELECT 1 FROM audit_log WHERE id = NEW.id)
BEGIN
  SELECT RAISE(ABORT, 'audit_log is append-only; id already exists');
END;
--> statement-breakpoint

CREATE TRIGGER `fiscal_periods_no_replace_closed`
BEFORE INSERT ON `fiscal_periods`
WHEN EXISTS (
  SELECT 1 FROM fiscal_periods
  WHERE status IN ('closed', 'locked')
    AND (id = NEW.id OR (start_date = NEW.start_date AND end_date = NEW.end_date))
)
BEGIN
  SELECT RAISE(ABORT, 'cannot replace a closed fiscal period');
END;
--> statement-breakpoint

CREATE TRIGGER `fiscal_periods_no_rekey_onto_closed`
BEFORE UPDATE OF `id`, `start_date`, `end_date` ON `fiscal_periods`
WHEN EXISTS (
  SELECT 1 FROM fiscal_periods
  WHERE status IN ('closed', 'locked') AND id <> OLD.id
    AND (id = NEW.id OR (start_date = NEW.start_date AND end_date = NEW.end_date))
)
BEGIN
  SELECT RAISE(ABORT, 'cannot replace a closed fiscal period');
END;
