-- Hosted mode, schema 4: the outbox, the owner-push ledger, the nudge's memory and the delivery
-- flag (Task 11). Times are UTC text of fixed width (see timeLayout in db.go); '' is no time.

-- A message the owner must be told about, written in the same transaction as the post or
-- escalation it carries and sent at least once. item_id is '' for a row that names no item.
CREATE TABLE outbox (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	kind        TEXT NOT NULL CHECK (kind IN ('duet_post', 'escalation', 'page', 'test')),
	swarm       TEXT NOT NULL,
	item_id     TEXT NOT NULL,
	revision    INTEGER NOT NULL,
	level       TEXT NOT NULL,
	title       TEXT NOT NULL,
	body        TEXT NOT NULL,
	state       TEXT NOT NULL CHECK (state IN ('pending', 'sent', 'failed', 'superseded')),
	attempts    INTEGER NOT NULL DEFAULT 0,
	next_at     TEXT NOT NULL,
	created     TEXT NOT NULL,
	sent_at     TEXT NOT NULL DEFAULT '',
	failed_at   TEXT NOT NULL DEFAULT '',
	last_status INTEGER NOT NULL DEFAULT 0,
	retried     INTEGER NOT NULL DEFAULT 0 CHECK (retried IN (0, 1))
);
CREATE INDEX outbox_due ON outbox (state, next_at);
CREATE INDEX outbox_pages_sent ON outbox (kind, swarm, state, sent_at);

-- The owner-push cap's ledger: item_id reached the owner's phone at `at`. source_id is the
-- outbox id, or the nudge's sequence number.
CREATE TABLE owner_pushes (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	source    TEXT NOT NULL CHECK (source IN ('outbox', 'nudge')),
	source_id INTEGER NOT NULL,
	item_id   TEXT NOT NULL,
	revision  INTEGER NOT NULL,
	at        TEXT NOT NULL,
	UNIQUE (source, source_id, item_id)
);
CREATE INDEX owner_pushes_by_item ON owner_pushes (item_id, at);

-- The collapsed nudge's memory: one row. count names the next nudge.
CREATE TABLE nudge_state (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	last_seq    INTEGER NOT NULL,
	last_sent   TEXT NOT NULL,
	count       INTEGER NOT NULL,
	last_status INTEGER NOT NULL,
	failed_at   TEXT NOT NULL
);
INSERT INTO nudge_state (id, last_seq, last_sent, count, last_status, failed_at) VALUES (1, 0, '', 0, 0, '');

-- Items a nudge may not count until due_at, because they were pushed less than 60 minutes ago.
CREATE TABLE nudge_deferred (
	item_id TEXT PRIMARY KEY,
	due_at  TEXT NOT NULL
);

-- Whether the channel itself is refused (401 or 403): one row, sticky across a restart.
CREATE TABLE delivery_flag (
	id     INTEGER PRIMARY KEY CHECK (id = 1),
	broken INTEGER NOT NULL CHECK (broken IN (0, 1)),
	since  TEXT NOT NULL,
	status INTEGER NOT NULL
);
INSERT INTO delivery_flag (id, broken, since, status) VALUES (1, 0, '', 0);
