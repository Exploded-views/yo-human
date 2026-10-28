-- Hosted mode, schema 1. Times are UTC text of fixed width (see timeLayout in db.go), so they
-- sort as strings. Token and secret columns hold hashes only, never the value.

CREATE TABLE items (
	id              TEXT PRIMARY KEY,
	swarm           TEXT NOT NULL,
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

-- Answers and approvals share this table. The CHECK is the database's refusal of an agent
-- approval; UNIQUE(item_id, revision, actor_kind) is its backstop for "first wins" within each
-- actor kind. assertion_id is the passkey challenge an approval consumed (NULL for answers).
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
	expires   TEXT NOT NULL
);
CREATE INDEX events_by_swarm ON events (swarm, seq);

CREATE TABLE statuses (
	swarm   TEXT PRIMARY KEY,
	doc     TEXT NOT NULL,
	updated TEXT NOT NULL
);

-- Credentials and flow state. The tasks that use these tables (7, 9, 10) own their columns'
-- meaning; each holds a hash of the credential, never the credential.
CREATE TABLE tokens (
	id      TEXT PRIMARY KEY,
	swarm   TEXT NOT NULL,
	name    TEXT NOT NULL,
	hash    TEXT NOT NULL UNIQUE,
	created TEXT NOT NULL,
	revoked TEXT NOT NULL DEFAULT ''
);

CREATE TABLE desk_starters (
	id      TEXT PRIMARY KEY,
	hash    TEXT NOT NULL UNIQUE,
	created TEXT NOT NULL,
	expires TEXT NOT NULL,
	revoked TEXT NOT NULL DEFAULT ''
);

CREATE TABLE desk_tokens (
	id         TEXT PRIMARY KEY,
	starter_id TEXT NOT NULL REFERENCES desk_starters (id),
	hash       TEXT NOT NULL UNIQUE,
	created    TEXT NOT NULL,
	expires    TEXT NOT NULL,
	revoked    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE desk_flows (
	id         TEXT PRIMARY KEY,
	starter_id TEXT NOT NULL REFERENCES desk_starters (id),
	device_hash TEXT NOT NULL UNIQUE,
	user_code  TEXT NOT NULL UNIQUE,
	phrase     TEXT NOT NULL,
	state      TEXT NOT NULL,
	created    TEXT NOT NULL,
	expires    TEXT NOT NULL
);

CREATE TABLE oauth_clients (
	client_id     TEXT PRIMARY KEY,
	name          TEXT NOT NULL,
	redirect_uris TEXT NOT NULL,
	created       TEXT NOT NULL
);

CREATE TABLE oauth_codes (
	code_hash      TEXT PRIMARY KEY,
	client_id      TEXT NOT NULL REFERENCES oauth_clients (client_id),
	redirect_uri   TEXT NOT NULL,
	code_challenge TEXT NOT NULL,
	created        TEXT NOT NULL,
	expires        TEXT NOT NULL,
	used           TEXT NOT NULL DEFAULT ''
);

CREATE TABLE oauth_tokens (
	hash      TEXT PRIMARY KEY,
	kind      TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
	client_id TEXT NOT NULL REFERENCES oauth_clients (client_id),
	created   TEXT NOT NULL,
	expires   TEXT NOT NULL,
	revoked   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE nudge_state (
	key     TEXT PRIMARY KEY,
	value   TEXT NOT NULL,
	updated TEXT NOT NULL
);
