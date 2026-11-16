package bus

import (
	"context"
	"time"
)

// Update is one event as a node sees it.
type Update struct {
	Seq            int64
	Type           EventType
	ItemID, Handle string
	Revision       int
	Record         *Record
	Message        *Message
}

// Updates is a page of events and the cursor to continue from.
type Updates struct {
	Events []Update
	Next   int64
}

// Answers is not implemented yet.
func (b *Bus) Answers(ctx context.Context, p Principal, since int64, wait time.Duration) (Updates, error) {
	b.committed(p.Swarm) // stub: keeps the wake in use until the implementation
	_ = b.wakes.subscribe(p.Swarm)
	return Updates{}, errTODO
}
