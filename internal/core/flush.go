package core

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/store"
)

// Flush sends whatever is due in the queue, oldest first, within the deadline. Its result lists
// every send it attempted, and its status says how they ended: failed if any was dead-lettered,
// else queued (with next) if any is left queued to retry, else ok. An entry parked because this
// config cannot send it was not attempted and does not count.
func (p *Pipeline) Flush(ctx context.Context) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.d.Config.Deadline)
	defer cancel()
	p = p.within(ctx)
	now := p.d.Now()
	if err := p.recoverStale(); err != nil {
		return Result{}, err
	}
	b := &batch{p: p, now: now}
	res := Result{Status: StatusOK, Channels: b.backlog(ctx, nil)}
	res.Channels, res.Warnings = append(res.Channels, b.more...), b.warns
	var soonest *ChannelResult // the queued entry due first: what the next flush is for
	dead := false
	for i, r := range res.Channels {
		switch {
		case r.Status == ChannelDead:
			dead = true
		case r.Status == ChannelQueued && !r.parked && (soonest == nil || r.retryAt.Before(soonest.retryAt)):
			soonest = &res.Channels[i]
		}
	}
	switch {
	case dead:
		res.Status = StatusFailed
	case soonest != nil:
		res.Status, res.Next = StatusQueued, p.nextFor(*soonest, now)
	}
	err := b.err()
	p.withNext(&res, err, "")
	return res, err
}

// backlog sends the due entries not in skip, oldest first, and returns the outcome of each one it
// started. It leaves alone a first attempt younger than twice the deadline, whose own caller is
// still sending it, and starts nothing with under a quarter of the deadline left. A listing failure
// is recorded on the batch.
func (b *batch) backlog(ctx context.Context, skip map[string]bool) []ChannelResult {
	due, err := b.p.d.Store.Due(b.now)
	if err != nil {
		b.fail(err)
		return nil
	}
	var todo []store.Pending
	for _, d := range due {
		if !skip[d.Key] && !b.fresh(d) {
			todo = append(todo, d)
		}
	}
	var out []ChannelResult
	for _, r := range b.drain(ctx, todo, b.p.workers, b.p.d.Config.Deadline/4) {
		if r.Status != "" {
			out = append(out, r)
		}
	}
	return out
}

// fresh reports a first attempt enqueued less than twice the deadline ago, by the pipeline's own
// stamp: the message's Created comes from the caller and decides nothing here.
func (b *batch) fresh(d store.Pending) bool {
	if d.Attempts > 0 {
		return false
	}
	var pl payload
	if json.Unmarshal(d.Data, &pl) != nil { // unreadable: let delivery dead-letter it
		return false
	}
	if pl.Kind == kindCancel || pl.Kind == kindAdmit { // no caller is sending it: a flush runs it
		return false
	}
	return b.now.Sub(pl.Enqueued) < 2*b.p.d.Config.Deadline
}

// drain delivers items in order, at most limit at a time, and stops starting new ones once ctx is
// done or less than minLeft of it remains. Its result lines up with items; one never started has an
// empty Status and stays queued.
func (b *batch) drain(ctx context.Context, items []store.Pending, limit int, minLeft time.Duration) []ChannelResult {
	out := make([]ChannelResult, len(items))
	sem := make(chan struct{}, max(limit, 1))
	var wg sync.WaitGroup
	for i, it := range items {
		if !acquire(ctx, sem, minLeft) {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r, err := b.deliver(ctx, it)
			out[i] = r
			if err != nil {
				b.fail(err)
			}
		}()
	}
	wg.Wait()
	return out
}

// acquire takes a worker slot, and gives it back and reports false if ctx ended, or has less than
// minLeft to run, once the slot came free, so no send starts, and no attempt is spent, too late.
func acquire(ctx context.Context, sem chan struct{}, minLeft time.Duration) bool {
	sem <- struct{}{}
	if dl, ok := ctx.Deadline(); ctx.Err() != nil || (ok && time.Until(dl) < minLeft) {
		<-sem
		return false
	}
	return true
}

// nextFor is the next step for a queued entry that stays queued: its own due time and the attempts
// its level still allows, not the level's constants.
func (p *Pipeline) nextFor(r ChannelResult, now time.Time) *Next {
	n := p.next(r.level)
	n.AfterSeconds = max(0, int((r.retryAt.Sub(now)+time.Second-1)/time.Second))
	n.AttemptsLeft = sendsLeft(p.retryFor(r.level), r.attempts)
	return n
}

// sendsLeft is how many more sends a level's retry allows after made sends: NextAttempt refuses
// only once the attempts reach Times, so the first send and Times retries make Times+1 in all.
// Notify (one send made) and flush count down from the same figure.
func sendsLeft(r config.Retry, made int) int { return max(0, r.Times+1-made) }
