-- Purchase "needed by" deadlines are date-only text (YYYY-MM-DD). The column has
-- existed since 0000_init; this index backs the calendar range query and the
-- request list's deadline sort. Only dated rows are indexed.
CREATE INDEX purchase_requests_needed_by_idx ON purchase_requests(needed_by, id) WHERE needed_by IS NOT NULL;
