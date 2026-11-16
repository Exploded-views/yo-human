package bus

import "time"

// The credential rows a token store returns. They carry no token and no hash: a lookup is by
// hash, and nothing past it needs either. A zero Revoked means the credential is live.

// Node is a node token's row: the swarm it speaks for.
type Node struct {
	Name, Swarm                string
	Created, LastUsed, Revoked time.Time
}

// Starter is a desk starter's row (yhd_). It has no swarm.
type Starter struct {
	Name                       string
	Created, LastUsed, Revoked time.Time
}

// Desk is a desk human token's row (yhh_). It lives YH_DESK_TTL from Created.
type Desk struct {
	Created, Revoked time.Time
}

// Access is an OAuth access token's row (yha_, Duet). It is filled by Task 10.
type Access struct {
	Expires, Revoked time.Time
}

// TokenKind says which store a listed token lives in.
type TokenKind string

// The kinds `token list` shows.
const (
	TokenNode    TokenKind = "node"
	TokenStarter TokenKind = "desk-starter"
)

// TokenRow is a token as `token list` shows it: Shown is the token's first 8 characters.
type TokenRow struct {
	Kind                       TokenKind
	Name, Swarm, Shown         string
	Created, LastUsed, Revoked time.Time
}
