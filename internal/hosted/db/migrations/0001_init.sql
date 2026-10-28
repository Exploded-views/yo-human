-- The triggers below are part of the schema's guarantees. decisions is append-only (UPDATE and
-- DELETE are refused). A later migration that rebuilds items or decisions (create new, copy,
-- drop, rename) must drop and recreate their triggers in the same file, or the guarantees go.

-- Hosted mode, schema 1: the tables Task 4 uses. Later tasks add theirs (credentials, OAuth,
-- passkeys, nudges). Times are UTC text of fixed width (see timeLayout in db.go), so they sort
-- as strings.

CREATE TABLE items (
	id              TEXT PRIMARY KEY,
	swarm           TEXT NOT NULL CHECK (swarm <> ''),
	handle          TEXT NOT NULL,
	kind            TEXT NOT NULL,
	severity        TEXT NOT NULL,
	to_target       TEXT NOT NULL,
	from_name       TEXT NOT NULL,
	from_kind       TEXT NOT NULL,
	title           TEXT NOT NULL,
	summary         TEXT NOT NULL,
	detail          TEXT NOT NULL,
	required_action TEXT NOT NULL,
	repo            TEXT NOT NULL,
	ref             TEXT NOT NULL,
	base_ref        TEXT NOT NULL,
	target          TEXT NOT NULL,
	pinned_commit   TEXT NOT NULL,
	pinned_tree     TEXT NOT NULL,
	action          TEXT NOT NULL,
	link            TEXT NOT NULL,
	risk_flags      TEXT NOT NULL DEFAULT '[]',
	untrusted       INTEGER NOT NULL CHECK (untrusted IN (0, 1)),
	revision        INTEGER NOT NULL CHECK (revision >= 1),
	state           TEXT NOT NULL CHECK (state IN ('open', 'decided')),
	content_hash    TEXT NOT NULL,
	last_seq        INTEGER NOT NULL DEFAULT 0,
	acked_seq       INTEGER NOT NULL DEFAULT 0,
	created         TEXT NOT NULL,
	updated         TEXT NOT NULL,
	UNIQUE (swarm, handle)
);
CREATE INDEX items_open_for_human ON items (state, to_target, created);

-- Answers and approvals share this table. The trigger below is the database's own check that a
-- record is for the item's kind and current revision. The CHECK is the database's refusal of an agent
-- approval; UNIQUE(item_id, revision, actor_kind) is its backstop for "first wins" within each
-- actor kind. assertion_id is the passkey challenge an approval consumed. It is nullable and
-- UNIQUE: SQLite's UNIQUE treats every NULL as distinct, so rows without one never collide.
-- Task 10c writes it and refuses an approval that has none.
CREATE TABLE decisions (
	item_id        TEXT NOT NULL REFERENCES items (id),
	revision       INTEGER NOT NULL CHECK (revision >= 1),
	kind           TEXT NOT NULL CHECK (kind IN ('answer', 'approval')),
	decision       TEXT NOT NULL DEFAULT '',
	actor_kind     TEXT NOT NULL CHECK (actor_kind IN ('human', 'agent') AND (kind = 'answer' OR actor_kind = 'human')),
	text           TEXT NOT NULL,
	source         TEXT NOT NULL,
	by_name        TEXT NOT NULL,
	source_message TEXT NOT NULL,
	human_ts       TEXT NOT NULL,
	recorded       TEXT NOT NULL,
	assertion_id   TEXT UNIQUE,
	CHECK ((kind = 'approval' AND decision IN ('approve', 'reject')) OR (kind = 'answer' AND decision = '')),
	UNIQUE (item_id, revision, actor_kind)
);

-- The owner's words as a node relayed them. Always an agent's; never decides an item.
CREATE TABLE relays (
	id        TEXT PRIMARY KEY,
	item_id   TEXT NOT NULL REFERENCES items (id),
	revision  INTEGER NOT NULL,
	text      TEXT NOT NULL,
	source    TEXT NOT NULL,
	by_name   TEXT NOT NULL,
	human_ts  TEXT NOT NULL,
	recorded  TEXT NOT NULL
);
CREATE INDEX relays_by_item ON relays (item_id);

CREATE TABLE messages (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL,
	from_name TEXT NOT NULL,
	from_kind TEXT NOT NULL,
	body      TEXT NOT NULL,
	severity  TEXT NOT NULL,
	created   TEXT NOT NULL
);
CREATE INDEX messages_by_thread ON messages (thread_id, created);

-- The append-only event log. swarm is '' for events that belong to no swarm (desk_*).
CREATE TABLE events (
	seq       INTEGER PRIMARY KEY AUTOINCREMENT,
	swarm     TEXT NOT NULL,
	item_id   TEXT NOT NULL,
	type      TEXT NOT NULL,
	created   TEXT NOT NULL,
	starter   TEXT NOT NULL,
	phrase    TEXT NOT NULL,
	user_code TEXT NOT NULL,
	expires   TEXT NOT NULL,
	-- Only the desk_* events belong to no swarm, and they belong to nothing else.
	CHECK ((substr(type, 1, 5) = 'desk_') = (swarm = ''))
);
CREATE INDEX events_by_swarm ON events (swarm, seq);

CREATE TABLE statuses (
	swarm   TEXT PRIMARY KEY CHECK (swarm <> ''),
	doc     TEXT NOT NULL,
	updated TEXT NOT NULL
);

CREATE TRIGGER decisions_guard BEFORE INSERT ON decisions
BEGIN
	SELECT RAISE(ABORT, 'decision for a missing item')
	WHERE NOT EXISTS (SELECT 1 FROM items WHERE id = NEW.item_id);
	SELECT RAISE(ABORT, 'decision kind does not match the item kind')
	WHERE (NEW.kind = 'approval') <> (SELECT kind = 'approval' FROM items WHERE id = NEW.item_id);
	SELECT RAISE(ABORT, 'decision revision is not the item''s current revision')
	WHERE NEW.revision <> (SELECT revision FROM items WHERE id = NEW.item_id);
END;

-- Decisions are append-only: UPDATE (above) and DELETE are refused.
CREATE TRIGGER decisions_append_only BEFORE UPDATE ON decisions
BEGIN
	SELECT RAISE(ABORT, 'decisions are append-only');
END;

-- An item's revision never goes backwards, and its kind changes only with a new revision, so a
-- decision recorded for one kind or revision can never come to decide another.
CREATE TRIGGER items_guard BEFORE UPDATE OF revision, kind ON items
BEGIN
	SELECT RAISE(ABORT, 'item revision cannot go backwards') WHERE NEW.revision < OLD.revision;
	SELECT RAISE(ABORT, 'item kind cannot change without a new revision')
	WHERE NEW.kind <> OLD.kind AND NEW.revision = OLD.revision;
END;

CREATE TRIGGER decisions_no_delete BEFORE DELETE ON decisions
BEGIN
	SELECT RAISE(ABORT, 'decisions are append-only');
END;

-- State is derived: decided only while a decision of the item's kind exists at its current
-- revision, open otherwise. InsertDecision and UpdateItem both satisfy this.
CREATE TRIGGER items_state_guard BEFORE UPDATE OF state ON items
BEGIN
	SELECT RAISE(ABORT, 'item cannot be decided without a decision of its kind at its revision')
	WHERE NEW.state = 'decided' AND NOT EXISTS (SELECT 1 FROM decisions
		WHERE item_id = NEW.id AND revision = NEW.revision AND (kind = 'approval') = (NEW.kind = 'approval'));
	SELECT RAISE(ABORT, 'item cannot be open while a decision of its kind exists at its revision')
	WHERE NEW.state = 'open' AND EXISTS (SELECT 1 FROM decisions
		WHERE item_id = NEW.id AND revision = NEW.revision AND (kind = 'approval') = (NEW.kind = 'approval'));
END;
