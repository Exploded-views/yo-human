package bus

import (
	"context"
	"time"
)

// The long-poll's limits: a wait is capped, and a page holds at most this many updates.
const (
	MaxWait    = 25 * time.Second
	MaxUpdates = 100
	MaxScan    = 1000 // events scanned per call, skipped ones included
)

// Update is one event as a node sees it. Revision is the item's current revision, except for an
// answer or approval, where it is the revision of the record the event wrote (an event names no
// revision of its own). Record and Message are data a sender wrote.
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

// Answers is a node's long-poll on its own swarm's events after since: at most MaxUpdates, not
// the ones the node caused itself, in seq order. Next is the last seq read, so a poller that
// passes it back loses nothing and reads nothing twice, even when it skipped events of its own.
// With wait above zero (capped at MaxWait) and nothing new, it blocks until a commit for the
// swarm or the wait ends, then returns what the store holds, possibly nothing. A cancelled ctx
// returns at once with its error. The wake is Bus.committed; the data always comes from the store.
func (b *Bus) Answers(ctx context.Context, p Principal, since int64, wait time.Duration) (Updates, error) {
	if e := needNode(p, "read your swarm's answers"); e != nil {
		return Updates{}, e
	}
	wait = min(max(wait, 0), MaxWait)
	cursor := since
	var fired <-chan time.Time
	stop := func() {}
	defer func() { stop() }()
	for {
		ups, next, capped, wake, err := b.readSubscribed(ctx, p, cursor)
		if err != nil {
			return Updates{Events: []Update{}, Next: cursor}, err
		}
		if returnNow(ups, wait, capped) {
			return Updates{Events: ups, Next: next}, nil
		}
		cursor = next
		if fired == nil {
			fired, stop = b.timer(wait)
		}
		select {
		case <-wake:
		case <-fired:
			return Updates{Events: []Update{}, Next: cursor}, nil
		case <-ctx.Done():
			return Updates{Events: []Update{}, Next: cursor}, ctx.Err()
		}
	}
}

// returnNow: the read has news, the caller doesn't wait, or the scan cap ended the page early.
func returnNow(ups []Update, wait time.Duration, capped bool) bool {
	return len(ups) > 0 || wait == 0 || capped
}

// needNode refuses every credential but a node's, with a valid swarm.
func needNode(p Principal, to string) *Error {
	if p.Kind == PrincipalNode && ValidSwarm(p.Swarm) {
		return nil
	}
	return &Error{Code: CodeForbidden, What: "credential", Valid: "a node credential", Fix: "use a node credential to " + to}
}

// readSubscribed subscribes to the swarm's wake and then reads, so a commit that lands after the
// read still wakes the caller. A caller that has gone away gets its context's error, any other
// failure a bus error.
func (b *Bus) readSubscribed(ctx context.Context, p Principal, cursor int64) ([]Update, int64, bool, <-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, nil, err
	}
	wake := b.wakes.subscribe(p.Swarm)
	ups, next, capped, err := b.readUpdates(ctx, p, cursor)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, 0, false, nil, cerr
		}
		return nil, 0, false, nil, mapStoreErr(err)
	}
	return ups, next, capped, wake, nil
}

// readUpdates reads the swarm's events after cursor until it has MaxUpdates updates, runs out, or
// has scanned MaxScan events (skipped ones count, so a call's work is bounded however many events
// the node caused itself). It returns the updates, the seq of the last event it looked at, and
// whether the scan cap ended it, in which case the page may be short.
func (b *Bus) readUpdates(ctx context.Context, p Principal, cursor int64) ([]Update, int64, bool, error) {
	ups := []Update{}
	next := cursor
	scanned := 0
	err := b.s.Tx(ctx, func(x Tx) error {
		r := &eventReader{x: x, p: p, items: map[string]*Item{}, msgs: map[string][]Message{}}
		for len(ups) < MaxUpdates && scanned < MaxScan {
			limit := min(MaxUpdates, MaxScan-scanned)
			evs, err := x.EventsSince(p.Swarm, next, limit)
			if err != nil {
				return err
			}
			for _, e := range evs {
				if len(ups) == MaxUpdates {
					break
				}
				next = e.Seq
				scanned++
				u, ok, err := r.update(e)
				if err != nil {
					return err
				}
				if ok {
					ups = append(ups, u)
				}
			}
			if len(evs) < limit {
				break
			}
		}
		return nil
	})
	return ups, next, scanned >= MaxScan, err
}
