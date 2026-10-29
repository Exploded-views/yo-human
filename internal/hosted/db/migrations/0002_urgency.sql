-- Interrupt lanes: every item and every message carries the poster's urgency, one of three
-- values. The DEFAULT exists only because SQLite's ADD COLUMN needs one for NOT NULL, and
-- backfills rows older than this migration; the code names the column on every insert, and the
-- bus refuses a missing urgency before anything is stored, so the default is never used for a
-- new row. The CHECK is the database's own refusal of any other value.

ALTER TABLE items ADD COLUMN urgency TEXT NOT NULL DEFAULT 'digest'
	CHECK (urgency IN ('page', 'interrupt-when-good', 'digest'));

ALTER TABLE messages ADD COLUMN urgency TEXT NOT NULL DEFAULT 'digest'
	CHECK (urgency IN ('page', 'interrupt-when-good', 'digest'));
