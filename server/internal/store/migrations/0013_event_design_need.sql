-- V0.4 Design page: does an event need design work? 'unknown' (never decided) counts as
-- needed for upcoming events; 'not_needed' is set by director+ or the event's host.
-- The board's indicator derives from this plus the event's linked design_requests.
ALTER TABLE events ADD COLUMN design_need TEXT NOT NULL DEFAULT 'unknown' CHECK(design_need IN ('unknown','needed','not_needed'));
