package nudge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

// counted is an item a nudge speaks of, at the revision it had.
type counted struct {
	id  string
	rev int
}

// plan is what one nudge will send and what undoes it: the state it replaced and the deferred
// rows it cleared.
type plan struct {
	prev    bus.NudgeState
	seq     int64
	items   []counted
	cleared []bus.Deferred
}

// nudge sends the collapsed nudge when the window is open and an item is countable. It commits
// the cursor and the window before the send, so a crash loses one nudge at most and never sends
// two: a transient failure or a broken channel puts them back, a refusal leaves them.
func (w *Worker) nudge(ctx context.Context, now time.Time) error {
	var p *plan
	err := w.s.Tx(ctx, func(x bus.Tx) (err error) { p, err = w.planNudge(x, now); return })
	if err != nil || p == nil {
		return err
	}
	waiting, err := w.b.Waiting(ctx)
	if err != nil {
		return errors.Join(err, w.restore(ctx, p))
	}
	res := w.ch.Send(ctx, notify.Message{Title: "yo-human", Body: fmt.Sprintf("%d new, %d waiting for Claude", len(p.items), waiting),
		Group: "yo-human", Level: w.cfg.Level})
	return w.nudged(ctx, now, p, res)
}

// planNudge is the whole decision, in one transaction. It returns nil when nothing goes out.
func (w *Worker) planNudge(x bus.Tx, now time.Time) (*plan, error) {
	ns, err := x.NudgeState()
	if err != nil || (!ns.LastSent.IsZero() && now.Sub(ns.LastSent) < w.cfg.Window) {
		return nil, err
	}
	ids, head, err := w.eligibleItems(x, ns.LastSeq)
	if err != nil {
		return nil, err
	}
	deferred, err := x.Deferred()
	if err != nil {
		return nil, err
	}
	p := &plan{prev: ns}
	if err := w.sort(x, now, ids, deferred, p); err != nil {
		return nil, err
	}
	if len(p.items) == 0 {
		if head > ns.LastSeq {
			ns.LastSeq = head
			return nil, x.PutNudgeState(ns)
		}
		return nil, nil //nolint:nilnil // nothing to send is not an error
	}
	ns.LastSeq, ns.LastSent, ns.Count = head, now, ns.Count+1
	p.seq = ns.Count
	return p, x.PutNudgeState(ns)
}

// sort splits the eligible items and the due deferred ones into those a nudge counts and those
// the 60-minute cap holds. A held item is deferred until the hour after its last push; a counted
// item's deferred row is cleared.
func (w *Worker) sort(x bus.Tx, now time.Time, ids []string, deferred []bus.Deferred, p *plan) error {
	byID := map[string]bus.Deferred{}
	for _, d := range deferred {
		byID[d.ItemID] = d
		if !d.DueAt.After(now) {
			ids = append(ids, d.ItemID)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := countOrHold(x, now, id, byID, p); err != nil {
			return err
		}
	}
	return nil
}

// countOrHold either defers the item to the end of its hour or adds it to the plan.
func countOrHold(x bus.Tx, now time.Time, id string, deferred map[string]bus.Deferred, p *plan) error {
	isHeld, until, err := heldUntil(x, now, id)
	if err != nil {
		return err
	}
	if isHeld {
		return x.PutDeferred(bus.Deferred{ItemID: id, DueAt: until})
	}
	it, found, err := x.ItemByID(id)
	if err != nil || !found {
		return err
	}
	p.items = append(p.items, counted{id, it.Revision})
	d, ok := deferred[id]
	if !ok {
		return nil
	}
	p.cleared = append(p.cleared, d)
	return x.DeleteDeferred(id)
}

// heldUntil says whether the item reached the owner less than pushWindow ago, at any revision,
// and when the hour is over.
func heldUntil(x bus.Tx, now time.Time, itemID string) (bool, time.Time, error) {
	last, ok, err := x.LastPush(itemID)
	if err != nil || !ok {
		return false, time.Time{}, err
	}
	until := last.Add(pushWindow)
	return now.Before(until), until, nil
}

// eligibleItems reads the events after since and returns the items a nudge may count, in the
// order their first event came, and the last event it looked at. desk_* events and everything
// else are skipped.
func (w *Worker) eligibleItems(x bus.Tx, since int64) (ids []string, head int64, err error) {
	head = since
	seen := map[string]bool{}
	for {
		evs, err := x.EventsSince("", head, 100)
		if err != nil || len(evs) == 0 {
			return ids, head, err
		}
		for _, ev := range evs {
			head = ev.Seq
			ok, err := w.countable(x, ev)
			if err != nil {
				return nil, since, err
			}
			if ok && !seen[ev.ItemID] {
				seen[ev.ItemID] = true
				ids = append(ids, ev.ItemID)
			}
		}
	}
}

// countable says whether an event is news the nudge speaks of: an item for the owner that
// someone but Duet posted or revised, in the digest lane and not below the minimum severity, or
// a message from someone but Duet on a thread Duet started.
func (w *Worker) countable(x bus.Tx, ev bus.Event) (bool, error) {
	switch ev.Type {
	case bus.EventItemPosted, bus.EventItemRevised:
		it, found, err := x.ItemByID(ev.ItemID)
		return found && it.To == "human" && it.FromKind != bus.PrincipalDuet && w.news(it.Severity, it.Urgency), err
	case bus.EventMessageAdded:
		return w.countableMessage(x, ev)
	}
	return false, nil
}

func (w *Worker) countableMessage(x bus.Tx, ev bus.Event) (bool, error) {
	it, found, err := x.ItemByID(ev.ItemID)
	if err != nil || !found || it.FromKind != bus.PrincipalDuet || it.To == "human" {
		return false, err
	}
	msgs, err := x.Messages(it.ID)
	if err != nil {
		return false, err
	}
	for _, m := range msgs {
		if m.Created.Equal(ev.Created) { // an event finds its message by time
			return m.FromKind != bus.PrincipalDuet && w.news(m.Severity, m.Urgency), nil
		}
	}
	return false, nil
}

func (w *Worker) news(s bus.Severity, u bus.Urgency) bool {
	return u == bus.UrgencyDigest && severityRank[s] >= severityRank[w.cfg.MinSeverity]
}

// nudged applies what the channel answered.
func (w *Worker) nudged(ctx context.Context, now time.Time, p *plan, res error) error {
	switch {
	case res == nil:
		w.log.Info("nudge sent", "nudge", p.seq, "counted", len(p.items), "status", 200)
		return w.s.Tx(ctx, func(x bus.Tx) error {
			for _, c := range p.items {
				if err := x.InsertPush(bus.Push{Source: bus.PushNudge, SourceID: p.seq, ItemID: c.id, Revision: c.rev, At: now}); err != nil {
					return err
				}
			}
			return w.clearIn(x)
		})
	case notify.IsPermanent(res):
		w.log.Info("nudge refused", "nudge", p.seq, "status", notify.Status(res))
		return w.s.Tx(ctx, func(x bus.Tx) error {
			ns, err := x.NudgeState()
			if err != nil {
				return err
			}
			ns.LastStatus, ns.FailedAt = notify.Status(res), now
			return x.PutNudgeState(ns)
		})
	case notify.IsBroken(res):
		w.log.Warn("nudge channel broken", "nudge", p.seq, "status", notify.Status(res))
		return errors.Join(w.restore(ctx, p), w.raise(ctx, now, notify.Status(res)))
	}
	w.log.Info("nudge will retry", "nudge", p.seq, "status", notify.Status(res))
	return w.restore(ctx, p)
}

// restore puts back the nudge state and the deferred rows the plan changed, so the next tick
// looks at the same events again.
func (w *Worker) restore(ctx context.Context, p *plan) error {
	return w.s.Tx(ctx, func(x bus.Tx) error {
		if err := x.PutNudgeState(p.prev); err != nil {
			return err
		}
		for _, d := range p.cleared {
			if err := x.PutDeferred(d); err != nil {
				return err
			}
		}
		return nil
	})
}
