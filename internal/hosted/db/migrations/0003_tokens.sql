-- Hosted mode, schema 3: node tokens, desk starters and desk human tokens (Task 8). The OAuth
-- access and refresh stores arrive with Task 10.
--
-- A token is never stored, only its SHA-256 in hex; shown is the token's first 8 characters,
-- for `token list`. One name space covers nodes and desk starters, among live tokens, so
-- `token revoke --name` is never ambiguous; a revoked token's name is free again. revoked is ''
-- while the token is live, else the UTC time it was revoked (see timeLayout in db.go).

CREATE TABLE tokens (
	name      TEXT NOT NULL CHECK (name <> ''),
	kind      TEXT NOT NULL CHECK (kind IN ('node', 'desk-starter')),
	swarm     TEXT NOT NULL,
	shown     TEXT NOT NULL,
	hash      TEXT NOT NULL UNIQUE,
	created   TEXT NOT NULL,
	last_used TEXT NOT NULL DEFAULT '',
	revoked   TEXT NOT NULL DEFAULT '',
	-- A node speaks for one swarm; a desk starter has none.
	CHECK ((kind = 'node' AND swarm <> '') OR (kind = 'desk-starter' AND swarm = ''))
);

CREATE UNIQUE INDEX tokens_live_name ON tokens (name) WHERE revoked = '';

-- Desk human tokens (yhh_) live YH_DESK_TTL from created; Task 10 mints them.
CREATE TABLE desk_tokens (
	hash    TEXT PRIMARY KEY,
	created TEXT NOT NULL,
	revoked TEXT NOT NULL DEFAULT ''
);
