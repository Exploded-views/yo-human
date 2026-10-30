package core

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// logEntry is one line of log.jsonl. It holds channel error text, which implementations keep free
// of secrets, and nothing from the settings or the secrets.
type logEntry struct {
	At         string `json:"at"`
	ID         string `json:"id"`
	Channel    string `json:"channel"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Attempts   int    `json:"attempts"`
	ProviderID string `json:"provider_id,omitempty"`
}

// deliver claims one queue entry, sends it and settles it: removed when sent, back in the queue
// with a later time when a retry can help, in dead/ when none can. A store error is returned with
// the outcome so far; the entry then stays where the store left it for recovery.
func (b *batch) deliver(ctx context.Context, pd store.Pending) (ChannelResult, error) {
	c, ok, err := b.p.d.Store.Claim(pd.Key)
	if err != nil {
		return ChannelResult{Channel: instanceOf(pd.Key)}, err
	}
	if !ok { // another process claimed it, or a resolve removed it
		return b.lostClaim(pd)
	}
	var pl payload
	if err := json.Unmarshal(c.Data, &pl); err != nil {
		return b.bury(c, payload{Instance: instanceOf(c.Key)}, "unreadable queue entry: "+err.Error(), false)
	}
	if pl.Kind == kindAdmit {
		return b.admitDeferred(ctx, c, pl)
	}
	return b.send(ctx, c, pl)
}

// send settles a claimed send or cancellation: dropped if its incident moved on, parked if this
// config cannot send it, otherwise run through its channel.
func (b *batch) send(ctx context.Context, c store.Claimed, pl payload) (ChannelResult, error) {
	// A cancellation carries no dedup key, so it is never stale: it outlives the incident that queued it.
	if cause, err := b.p.stale(pl); err != nil || cause != "" {
		return b.drop(c, pl, cause, err)
	}
	ch, built := b.p.d.Channels[pl.Instance]
	if why := b.stranded(pl, built); why != "" {
		return b.park(c, pl, why)
	}
	if pl.Kind == kindCancel {
		return b.cancelQueued(ctx, c, pl, ch)
	}
	spec := channel.SendSpec{Instance: pl.Instance, Interruption: pl.Interruption, Options: pl.Options}
	if b.p.d.Caps[pl.Instance].Schedule && c.DeliverAt.After(b.now) {
		spec.DeliverAt = c.DeliverAt
	}
	started := time.Now() // real time: how long the send ran, against the deadline
	rcpt, serr := b.dispatch(ctx, ch, pl, spec)
	return b.settle(ctx, sendOutcome{c: c, pl: pl, rcpt: rcpt, err: serr, took: time.Since(started)})
}

// stranded says why an entry cannot be sent by this run, or "" when it can. The queue belongs to the
// user and the config to the invocation, so an entry may have been queued under a config that is
// not the one in force: its channel is not named here, or points somewhere else (another type,
// settings or secret names; an entry with no fingerprint is not trusted), or was left unbuilt by the
// command (a missing secret on another channel must not block it).
func (b *batch) stranded(pl payload, built bool) string {
	inst, configured := b.p.d.Config.Channels[pl.Instance]
	under := ""
	if pl.Config != "" {
		under = " (it was queued under --config " + problem.ShellQuote(pl.Config) + ")"
	}
	switch {
	case configured && pl.Fingerprint != fingerprintOf(inst):
		return "channel " + pl.Instance + " now points somewhere else than when this message was queued" + under +
			"; it is not sent to a different destination"
	case !configured:
		return "channel " + pl.Instance + " is not configured in the config in force" + under
	case !built:
		return "channel " + pl.Instance + " was not built by this command; the entry stays queued"
	}
	return ""
}

// park puts a claimed entry back in the queue untouched, with the reason it was not sent. Nothing is
// logged: a cron flush under another config would otherwise write a line a minute for it.
func (b *batch) park(c store.Claimed, pl payload, why string) (ChannelResult, error) {
	res := ChannelResult{Channel: pl.Instance, Status: ChannelQueued, Error: why, attempts: c.Attempts, parked: true}
	return res, b.p.d.Store.Park(c, b.now, why)
}

// lostClaim reports an entry another process claimed as skipped, still on its way. One whose
// incident was resolved, or reopened since, was removed by that resolve, and is cancelled.
func (b *batch) lostClaim(pd store.Pending) (ChannelResult, error) {
	res := ChannelResult{Channel: instanceOf(pd.Key), Status: ChannelSkipped}
	var pl payload
	_ = json.Unmarshal(pd.Data, &pl) // an unreadable entry has no incident: it is skipped, and dead-lettered by whoever holds it
	cause, err := b.p.stale(pl)
	if cause != "" {
		res.Status = ChannelCancelled
	}
	return res, err
}

// dispatch makes the call for a claimed entry: a resolve note to a channel that can close an
// incident closes it, anything else is sent.
func (b *batch) dispatch(ctx context.Context, ch channel.Channel, pl payload, spec channel.SendSpec) (channel.Receipt, error) {
	m := pl.Message
	m.DedupKey = ProviderKey(m.Project, m.DedupKey) // the provider sees the project's key, never the bare one
	if m.Resolved && b.p.d.Caps[pl.Instance].Resolve {
		return channel.Receipt{}, ch.Resolve(ctx, m.DedupKey, m)
	}
	return ch.Send(ctx, m, spec)
}

// sendOutcome is what one send left behind, for settle.
type sendOutcome struct {
	c    store.Claimed
	pl   payload
	rcpt channel.Receipt
	err  error
	took time.Duration
}

func (b *batch) settle(ctx context.Context, o sendOutcome) (ChannelResult, error) {
	switch {
	case o.err == nil:
		status := ChannelSent
		if o.rcpt.Scheduled {
			status = ChannelHeld // the provider delivers it later; the queue is done with it
		}
		res := ChannelResult{Channel: o.pl.Instance, Status: status, ProviderID: o.rcpt.ProviderID,
			Scheduled: o.rcpt.Scheduled, attempts: o.c.Attempts + 1}
		complete := func() error { return b.p.d.Store.Complete(o.c) }
		err := error(nil)
		if o.pl.Message.DedupKey != "" && !o.pl.Message.Resolved {
			err = b.landed(ctx, o.pl, res, complete)
		} else {
			err = complete()
		}
		return b.finish(o.c, o.pl, res, err)
	case channel.KindOf(o.err) == channel.TransientError:
		return b.settleTransient(ctx, o)
	}
	// Permanent and config errors are never retried.
	return b.bury(o.c, o.pl, o.err.Error(), channel.KindOf(o.err) == channel.ConfigError)
}

func (b *batch) settleTransient(ctx context.Context, o sendOutcome) (ChannelResult, error) {
	res := ChannelResult{Channel: o.pl.Instance, Error: o.err.Error(), attempts: o.c.Attempts + 1}
	// Our own deadline cut the send short before the channel had a fair run: not its failure, so
	// no attempt is spent. One that ran half the deadline or more counts, or a provider that
	// always hangs would never reach the dead letter.
	if ctx.Err() != nil && o.took < b.p.d.Config.Deadline/2 {
		res.Status, res.attempts, res.retryAt = b.waiting(o.c.Pending), o.c.Attempts, b.now
		return b.requeue(o, res, func() error { return b.p.d.Store.Release(o.c, b.now) })
	}
	next, retry := NextAttempt(b.p.retryFor(o.pl.Message.Level), o.c.Attempts, b.now)
	if !retry {
		return b.bury(o.c, o.pl, "retries exhausted: "+o.err.Error(), false)
	}
	res.Status, res.retryAt = b.waiting(o.c.Pending), next
	return b.requeue(o, res, func() error { return b.p.d.Store.Retry(o.c, next) })
}

// requeue puts a claimed entry back in the queue with op (Retry or Release), unless its incident
// was resolved or superseded since: a resolve cannot reach an entry in flight, so it is dropped here
// instead. The check and op share one hold of the incident lock, so a resolve either closes the
// incident first, and the entry is dropped, or finds the requeued entry and cancels it.
func (b *batch) requeue(o sendOutcome, res ChannelResult, op func() error) (ChannelResult, error) {
	key := o.pl.Message.DedupKey
	if key == "" {
		return b.finish(o.c, o.pl, res, op())
	}
	cause := ""
	err := b.p.d.Store.WithIncidents(func(incs map[string]*store.Incident) error {
		if cause = outdated(o.pl, incs[incidentID(o.pl.Message)]); cause != "" {
			return b.p.d.Store.Complete(o.c)
		}
		return op()
	})
	if cause != "" {
		res.Status, res.Error = ChannelCancelled, "dropped: incident "+key+" was "+cause
	}
	return b.finish(o.c, o.pl, res, err)
}

// drop removes a claimed entry whose incident has moved on (cause says how), with a log line. A
// failed check (err) leaves the entry for recovery.
func (b *batch) drop(c store.Claimed, pl payload, cause string, err error) (ChannelResult, error) {
	res := ChannelResult{Channel: pl.Instance, attempts: c.Attempts}
	if err != nil {
		return res, err
	}
	res.Status, res.Error = ChannelCancelled, "dropped: incident "+pl.Message.DedupKey+" was "+cause
	return b.finish(c, pl, res, b.p.d.Store.Complete(c))
}

// landed records a successful send on its incident and, if the incident closed while the send was
// in flight, undoes what it can: it cancels a scheduled delivery, and answers a delivered message
// with a resolve note, sent now. complete removes the sent entry from the queue; it runs in the same
// hold of the incident lock as the record, so a repeat never sees a send that is neither pending
// nor recorded.
func (b *batch) landed(ctx context.Context, pl payload, res ChannelResult, complete func() error) error {
	l, err := b.p.landed(pl, res, complete)
	if err != nil {
		return err
	}
	if l.cancel != nil {
		// The send arrived through this channel a moment ago, so it is in the map.
		_, err = b.cancelNow(ctx, b.p.d.Channels[l.cancel.Instance], *l.cancel)
		return err
	}
	if l.note != nil {
		_, err = b.deliver(ctx, *l.note)
	}
	return err
}

// bury moves a claimed entry to dead/ with a reason shown by status.
func (b *batch) bury(c store.Claimed, pl payload, reason string, config bool) (ChannelResult, error) {
	res := ChannelResult{Channel: pl.Instance, Status: ChannelDead, Error: reason, attempts: c.Attempts + 1, config: config}
	err := b.p.d.Store.Dead(c, reason)
	res.buried = err == nil
	return b.finish(c, pl, res, err)
}

// finish logs the outcome. storeErr is the result of the store call that settled the entry; either
// failure is reported, and the outcome is returned regardless.
func (b *batch) finish(c store.Claimed, pl payload, res ChannelResult, storeErr error) (ChannelResult, error) {
	res.level = pl.Message.Level
	logErr := b.p.d.Store.AppendLog(logEntry{At: b.now.UTC().Format("2006-01-02T15:04:05Z"), ID: pl.Message.ID,
		Channel: res.Channel, Status: res.Status, Error: res.Error, Attempts: res.attempts, ProviderID: res.ProviderID})
	if storeErr != nil {
		return res, storeErr
	}
	return res, logErr
}

// instanceOf is the channel part of a "<message-id>.<instance>" key.
func instanceOf(key string) string {
	_, inst, _ := strings.Cut(key, ".")
	return inst
}
