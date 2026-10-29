package bus

import (
	"errors"
	"time"
)

// Bus is hosted mode's domain object: it owns the rules and reaches state only through Store.
type Bus struct {
	s   Store
	now func() time.Time
	l   Limits
}

// New returns a Bus over s, reading time from now and checking drafts against l.
func New(s Store, now func() time.Time, l Limits) *Bus {
	return &Bus{s: s, now: now, l: l}
}

// The typed errors a Store returns when a write doesn't fit what is stored. internal/hosted/db
// returns these same values, so the bus can match them without importing db.
var (
	ErrNotFound      = errors.New("bus: not found")
	ErrStaleRevision = errors.New("bus: revision is not the item's current revision")
	ErrKindMismatch  = errors.New("bus: kind does not match the item's kind")
)

// Outcome says what a Post did.
type Outcome string

// The outcomes.
const (
	OutcomeCreated   Outcome = "created"
	OutcomeRevised   Outcome = "revised"
	OutcomeUnchanged Outcome = "unchanged"
)

// PostResult is the item as stored and what the post did to it.
type PostResult struct {
	Item    Item
	Outcome Outcome
}
