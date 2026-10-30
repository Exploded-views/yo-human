package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// kindCancel marks a queue entry that cancels a send the provider holds for later delivery.
const kindCancel = "cancel"

// queueCancel queues the cancellation of a scheduled send whose channel cannot be reached now. The
// entry holds the instance, the provider's id and the destination the send went through; a flush
// runs it once an instance with that destination can be built. It belongs to no incident (a
// resolve must not remove it) and carries no dedup key, so it is never dropped as stale: it is
// valid for the generation that closed, and for every later one.
func (p *Pipeline) queueCancel(h heldSend) error {
	pd, _ := p.cancelEntry(h)
	return p.d.Store.Enqueue(pd)
}

// cancelEntry is the queue entry, and its payload, that cancels h.
func (p *Pipeline) cancelEntry(h heldSend) (store.Pending, payload) {
	pl := payload{Kind: kindCancel, Message: channel.Message{ID: p.d.NewID(), Level: h.Level}, Generation: h.Generation,
		Enqueued: p.d.Now(), Instance: h.Instance, Fingerprint: h.Fingerprint, Config: p.d.ConfigPath, ProviderID: h.ProviderID}
	data, _ := json.Marshal(pl) // strings and numbers always encode
	return store.Pending{Key: p.d.NewID() + "." + h.Instance, Data: data}, pl
}

// cancelNow cancels h through ch, a channel that points where the send went. A cancel that fails is
// not left to the error alone: it is queued and settled like a queued cancellation, so one that may
// pass is retried and one that cannot is dead-lettered, where status shows it. The failure is
// returned as a problem saying the human may still be paged, joined with any store error.
func (b *batch) cancelNow(ctx context.Context, ch channel.Channel, h heldSend) (ChannelResult, error) {
	started := time.Now() // real time: how long the cancel ran, against the deadline
	cerr := ch.Cancel(ctx, h.Receipt)
	if cerr == nil {
		return ChannelResult{Channel: h.Instance, Status: ChannelCancelled}, nil
	}
	res, err := b.keepFailedCancel(ctx, h, cerr, time.Since(started))
	return res, errors.Join(cancelProblem(res, cerr), err)
}

// keepFailedCancel puts a cancel that failed in the queue and settles it there.
func (b *batch) keepFailedCancel(ctx context.Context, h heldSend, cerr error, took time.Duration) (ChannelResult, error) {
	res := ChannelResult{Channel: h.Instance, Status: ChannelDead, Error: cerr.Error()}
	pd, pl := b.p.cancelEntry(h)
	if err := b.p.d.Store.Enqueue(pd); err != nil {
		return res, err
	}
	c, ok, err := b.p.d.Store.Claim(pd.Key)
	if err != nil || !ok { // still queued, or claimed by a flush that runs it
		res.Status = ChannelQueued
		return res, err
	}
	return b.settle(ctx, sendOutcome{c: c, pl: pl, err: cerr, took: took})
}

// cancelProblem is the problem for a scheduled send that could not be cancelled: the human may still
// be paged for an incident that is resolved. A cancel queued for another try is Queued (exit 75: the
// flush in the result's next step retries it); one in the dead letter is Unavailable (exit 69).
func cancelProblem(res ChannelResult, cerr error) error {
	kind, fix := problem.Queued, "the cancel is queued: run the next step (`yo-human flush`), which retries it; until it succeeds the human may still be paged"
	if res.Status == ChannelDead {
		kind, fix = problem.Unavailable, "the human may still be paged: the cancel is in the dead letter (see `yo-human status`); delete the scheduled message at the provider if it is still there"
	}
	return &problem.Problem{Kind: kind, What: "the scheduled send on " + res.Channel + " could not be cancelled: " + cerr.Error(),
		Valid: "a cancel the provider accepts", Fix: fix}
}

// cancelQueued runs a queued cancellation through its channel and settles the entry like a send:
// removed when cancelled, retried when the failure may pass, dead-lettered when it cannot.
func (b *batch) cancelQueued(ctx context.Context, c store.Claimed, pl payload, ch channel.Channel) (ChannelResult, error) {
	started := time.Now() // real time: how long the cancel ran, against the deadline
	err := ch.Cancel(ctx, channel.Receipt{Instance: pl.Instance, ProviderID: pl.ProviderID, Scheduled: true})
	if err != nil {
		return b.settle(ctx, sendOutcome{c: c, pl: pl, err: err, took: time.Since(started)})
	}
	res := ChannelResult{Channel: pl.Instance, Status: ChannelCancelled, attempts: c.Attempts + 1}
	return b.finish(c, pl, res, b.p.d.Store.Complete(c))
}
