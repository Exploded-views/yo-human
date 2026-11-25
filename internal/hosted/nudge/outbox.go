package nudge

import (
	"context"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

// verdict is what the cap and the supersede rules say about one pending row.
type verdict int

const (
	send       verdict = iota // nothing stands in the way
	held                      // the cap moved its next_at; it stays pending
	superseded                // the bus no longer wants it sent
)

// outbox takes the pending rows that are due, oldest first, at most outboxBatch, and sends each
// that the rules allow. It stops at the first transient error (the channel is likely down) or a
// broken one. In probe mode it sends one message and stops: attempted says whether it did.
func (w *Worker) outbox(ctx context.Context, now time.Time, probe bool) (attempted bool, err error) {
	var rows []bus.OutboxRow
	if err := w.s.Tx(ctx, func(x bus.Tx) (err error) { rows, err = x.PendingOutbox(now, outboxBatch); return }); err != nil {
		return false, err
	}
	done := map[int64]bool{}
	for _, r := range rows {
		if done[r.ID] {
			continue
		}
		group, err := w.judgeGroup(ctx, now, r, rows, done)
		if err != nil {
			return attempted, err
		}
		if len(group) == 0 {
			continue
		}
		attempted = true
		stop, err := w.sendGroup(ctx, now, group)
		if err != nil || stop || probe {
			return attempted, err
		}
	}
	return attempted, nil
}

// judgeGroup applies the cap and the supersede rule to r, immediately before its send and after
// every earlier row of the pass has committed. A row that may go is returned with the rows that
// coalesce with it: held Duet posts for one item, or pages from one swarm.
func (w *Worker) judgeGroup(ctx context.Context, now time.Time, r bus.OutboxRow, rows []bus.OutboxRow, done map[int64]bool) (group []bus.OutboxRow, err error) {
	err = w.s.Tx(ctx, func(x bus.Tx) error {
		group = nil
		for _, o := range rows {
			if done[o.ID] || (o.ID != r.ID && !coalesces(r, o)) {
				continue
			}
			done[o.ID] = true
			v, err := w.judge(x, now, &o)
			if err != nil {
				return err
			}
			if v == send {
				group = append(group, o)
			}
		}
		return nil
	})
	return group, err
}

// coalesces says whether two rows go out as one push when they come due together.
func coalesces(a, b bus.OutboxRow) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case bus.OutboxDuetPost:
		return a.ItemID == b.ItemID
	case bus.OutboxPage:
		return a.Swarm == b.Swarm
	}
	return false
}

// judge decides one row. A Duet post is superseded once its item has moved to a later revision
// or has a decision at its own; a Duet post or a page is held while its item reached the owner
// less than pushWindow ago, and a page also while its swarm paged less than pageSpacing ago. An
// escalation and a test push are neither. A held row is never dropped: its next_at moves.
func (w *Worker) judge(x bus.Tx, now time.Time, r *bus.OutboxRow) (verdict, error) {
	if r.Kind == bus.OutboxDuetPost {
		if gone, err := stale(x, *r); err != nil || gone {
			if err == nil {
				r.State = bus.OutboxSuperseded
				err = x.UpdateOutbox(*r)
			}
			return superseded, err
		}
	}
	if r.Kind != bus.OutboxDuetPost && r.Kind != bus.OutboxPage {
		return send, nil
	}
	until, err := w.holdUntil(x, now, *r)
	if err != nil || until.IsZero() {
		return send, err
	}
	r.NextAt = until
	return held, x.UpdateOutbox(*r)
}

// holdUntil is when a capped row may next go, or the zero time when it may go now.
func (w *Worker) holdUntil(x bus.Tx, now time.Time, r bus.OutboxRow) (time.Time, error) {
	var until time.Time
	if r.ItemID != "" {
		if h, u, err := heldUntil(x, now, r.ItemID); err != nil || h {
			if err != nil {
				return until, err
			}
			until = u
		}
	}
	if r.Kind != bus.OutboxPage {
		return until, nil
	}
	last, ok, err := x.LastPageSent(r.Swarm)
	if err != nil || !ok {
		return until, err
	}
	if u := last.Add(pageSpacing); now.Before(u) && u.After(until) {
		until = u
	}
	return until, nil
}

// stale says whether a Duet post is no longer worth sending: its item's revision moved on, or
// the owner's decision for its revision is in.
func stale(x bus.Tx, r bus.OutboxRow) (bool, error) {
	it, found, err := x.ItemByID(r.ItemID)
	if err != nil || !found || it.Revision != r.Revision {
		return err == nil, err
	}
	_, decided, err := x.Decision(it.ID, r.Revision)
	return decided, err
}

// compose is the one push for a group. Its id is the lowest outbox id in the group, so a retry
// or a re-send after a crash replaces the notification on the phone instead of adding one.
func compose(g []bus.OutboxRow) notify.Message {
	low, newest := g[0], g[0]
	for _, r := range g {
		low, newest = lowerID(low, r), higherID(newest, r)
	}
	m := notify.Message{ID: fmt.Sprintf("yh-%d", low.ID), Title: newest.Title, Body: newest.Body, Group: "yo-human", Level: notify.Level(newest.Level)}
	switch {
	case len(g) == 1:
	case newest.Kind == bus.OutboxPage:
		m.Title, m.Body = "yo-human", fmt.Sprintf("%d pages for Claude", len(g))
	default:
		m.Body += fmt.Sprintf(" (+%d more)", len(g)-1)
	}
	return m
}

func lowerID(a, b bus.OutboxRow) bus.OutboxRow {
	if b.ID < a.ID {
		return b
	}
	return a
}

func higherID(a, b bus.OutboxRow) bus.OutboxRow {
	if b.ID > a.ID {
		return b
	}
	return a
}

// sendGroup sends the group's push and applies the answer. stop says the pass ends here.
func (w *Worker) sendGroup(ctx context.Context, now time.Time, g []bus.OutboxRow) (stop bool, err error) {
	res := w.ch.Send(ctx, compose(g))
	switch {
	case res == nil:
		return false, w.sent(ctx, now, g)
	case notify.IsBroken(res):
		w.logRows("outbox channel broken", g, notify.Status(res))
		return true, w.raise(ctx, now, notify.Status(res))
	case notify.IsPermanent(res):
		return false, w.settle(ctx, g, "outbox refused", notify.Status(res), func(r *bus.OutboxRow) {
			r.State, r.FailedAt = bus.OutboxFailed, now
		})
	}
	return true, w.settle(ctx, g, "outbox will retry", notify.Status(res), func(r *bus.OutboxRow) {
		r.NextAt = now.Add(retryDelay(r.Attempts, notify.RetryAfter(res)))
	})
}

// retryDelay is how long a row waits after its attempts-th failed attempt: the larger of the
// channel's Retry-After and the backoff (30 s doubling), at most backoffMax either way.
func retryDelay(attempts int, retryAfter time.Duration) time.Duration {
	back := backoffBase << min(max(attempts-1, 0), 10)
	return min(max(back, retryAfter), backoffMax)
}

// sent marks the group sent. For Duet posts only the lowest row is sent and the rest, whose text
// it carried, are superseded; every sent row that names an item gets its one ledger row in the
// same transaction. The 2xx clears the broken flag.
func (w *Worker) sent(ctx context.Context, now time.Time, g []bus.OutboxRow) error {
	low := g[0]
	for _, r := range g {
		low = lowerID(low, r)
	}
	err := w.s.Tx(ctx, func(x bus.Tx) error {
		for _, r := range g {
			if r.Kind == bus.OutboxDuetPost && r.ID != low.ID {
				r.State = bus.OutboxSuperseded
			} else {
				r.State, r.SentAt, r.Attempts = bus.OutboxSent, now, r.Attempts+1
				if err := ledger(x, r, now); err != nil {
					return err
				}
			}
			if err := x.UpdateOutbox(r); err != nil {
				return err
			}
		}
		return w.clearIn(x)
	})
	if err == nil {
		w.logRows("outbox sent", g, 200)
	}
	return err
}

func ledger(x bus.Tx, r bus.OutboxRow, now time.Time) error {
	if r.ItemID == "" {
		return nil
	}
	return x.InsertPush(bus.Push{Source: bus.PushOutbox, SourceID: r.ID, ItemID: r.ItemID, Revision: r.Revision, At: now})
}

// settle counts an attempt on every row of the group, applies change to each, and logs it.
func (w *Worker) settle(ctx context.Context, g []bus.OutboxRow, msg string, status int, change func(*bus.OutboxRow)) error {
	err := w.s.Tx(ctx, func(x bus.Tx) error {
		for _, r := range g {
			r.Attempts, r.LastStatus = r.Attempts+1, status
			change(&r)
			if err := x.UpdateOutbox(r); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		w.logRows(msg, g, status)
	}
	return err
}

// logRows logs each row's result with its ids, kind, status and attempts, and never a reason, a
// title, a body or a key.
func (w *Worker) logRows(msg string, g []bus.OutboxRow, status int) {
	for _, r := range g {
		w.log.Info(msg, "outbox_id", r.ID, "item_id", r.ItemID, "kind", string(r.Kind), "status", status, "attempts", r.Attempts+1)
	}
}
