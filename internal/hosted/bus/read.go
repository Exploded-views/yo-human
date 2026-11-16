package bus

import (
	"context"
	"errors"
	"time"
)

// Summary is one line of the inbox. Every text field is untrusted data. Kind and Urgency are
// added to the plan's fields so an item in the Interrupt group still says what it is.
type Summary struct {
	ID, Handle, From, Swarm string
	Severity                Severity
	Title, Summary          string
	Revision                int
	Created                 time.Time
	Link                    string
	Untrusted               bool
	Kind                    Kind
	Urgency                 Urgency
}

// Inbox is what Duet reads.
type Inbox struct {
	Groups    map[Kind][]Summary
	Interrupt []Summary
	Threads   []Summary
	Next      int64
}

// ItemView is an item with its thread and the decision for its current revision.
type ItemView struct {
	Item     Item
	Messages []Message
	Decision *Record
}

var errTODO = errors.New("not implemented")

// Inbox is not implemented yet.
func (b *Bus) Inbox(ctx context.Context, p Principal, since int64) (Inbox, error) {
	return Inbox{}, errTODO
}

// Item is not implemented yet.
func (b *Bus) Item(ctx context.Context, p Principal, id string) (ItemView, error) {
	return ItemView{}, errTODO
}

// Ack is not implemented yet.
func (b *Bus) Ack(ctx context.Context, p Principal, id string) error { return errTODO }
