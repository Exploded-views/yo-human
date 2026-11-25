package bus

import (
	"errors"
	"time"
)

// Bus is hosted mode's domain object: it owns the rules and reaches state only through Store.
type Bus struct {
	s      Store
	now    func() time.Time
	l      Limits
	walled map[string]struct{} // the walled swarms, set once by New and never written again
	wakes  *wakes              // the long-poll broadcast, see committed
	timer  timerFunc           // the long-poll's timeout; a test replaces it
	onSave func()              // Limits.OnCommit
}

// New returns a Bus over s, reading time from now and checking drafts against l. The walled
// swarms come from l.WalledSwarms and are copied here: no later call can change them.
func New(s Store, now func() time.Time, l Limits) *Bus {
	walled := make(map[string]struct{}, len(l.WalledSwarms))
	for _, w := range l.WalledSwarms {
		walled[w] = struct{}{}
	}
	l.WalledSwarms = nil // the set above is the only copy
	return &Bus{s: s, now: now, l: l, walled: walled, wakes: newWakes(), timer: realTimer, onSave: l.OnCommit}
}

// The typed errors a Store returns when a write doesn't fit what is stored. internal/hosted/db
// returns these same values, so the bus can match them without importing db.
var (
	ErrNotFound      = errors.New("bus: not found")
	ErrStaleRevision = errors.New("bus: revision is not the item's current revision")
	ErrKindMismatch  = errors.New("bus: kind does not match the item's kind")
	// ErrConflict is a write the database refused because a row already holds that key (a second
	// decision for one item, revision and actor kind). A Store wraps it around its own error; the
	// bus re-reads and settles the race by rule 6.
	ErrConflict = errors.New("bus: a record for this key already exists")
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
