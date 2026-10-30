package core

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// Resolve closes the incident for key. It first cancels everything still pending for it: queued and
// held sends are removed, and sends a channel scheduled are cancelled through the channel. Then each
// channel the incident reached hears of it: one that can resolve is told to, the others get a
// passive note, whatever the quiet hours say. Notes go through the queue like any send, so a failed
// one is retried. An unknown or already resolved key is a noop. The result lists what was cancelled
// and what was sent.
func (p *Pipeline) Resolve(ctx context.Context, key, note string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.d.Config.Deadline)
	defer cancel()
	p = p.within(ctx)
	now := p.d.Now()
	if err := p.recoverStale(); err != nil {
		return Result{}, err
	}
	id := p.d.NewID()
	b := &batch{p: p, now: now}
	inc, gone, open, err := b.closeIncident(key)
	if err != nil {
		return Result{ID: id}, err
	}
	if !open {
		res := Result{Status: StatusNoop, ID: id}
		if inc.Open { // expired: over already, so a noop, but its held pages must still not fire
			b.cancelScheduled(ctx, inc)
		} else if len(gone) > 0 { // a message still waiting for its incident check, which this resolve stops
			res = Result{Status: StatusResolved, ID: id, Channels: cancelled(gone)}
		}
		b.backlog(ctx, nil)
		err = busyAsWarnings(&res, b.err())
		p.withNext(&res, err, inc.Level) // an expired incident's cancel may be queued
		return res, err
	}
	res := Result{Status: StatusResolved, ID: id}
	res.Channels = append(res.Channels, cancelled(gone)...)
	cancels := b.cancelScheduled(ctx, inc)
	entries, notes := b.enqueueNotes(id, note, inc)
	closes := b.fanOut(ctx, entries, notes)
	res.Channels = append(append(res.Channels, cancels...), closes...)
	res.Next = b.closeOutcome(inc.Key, cancels, closes)
	skip := map[string]bool{}
	for _, e := range entries {
		skip[e.pending.Key] = true
	}
	b.backlog(ctx, skip)
	err = busyAsWarnings(&res, b.err())
	p.withNext(&res, err, inc.Level)
	return res, err
}

// closeIncident marks the incident resolved and removes its queued sends in one hold of the
// incident lock, so no other process can reopen the key, or claim one of its entries without
// finding it closed, between the two. It returns a copy of the incident as it stood, and false for a
// key with no open incident. A queue that cannot be cleaned is recorded on the batch; the incident
// is closed regardless, and an entry left behind is dropped when it is claimed.
//
// A key with no open incident is left with a closed record stamped now, so a message for it still
// waiting for its incident check, even one a flush has already claimed, is dropped rather than
// paged (admit). An incident silent for longer than incident_ttl has expired: it is closed the same
// way and its queued sends removed, and the returned copy, still Open, carries its scheduled sends
// for the caller to cancel; it is a noop to the caller.
func (b *batch) closeIncident(key string) (inc store.Incident, gone []store.Pending, open bool, err error) {
	err = b.p.d.Store.WithIncidents(func(incs map[string]*store.Incident) error {
		b.p.pruneClosed(incs, b.now)
		cur := incs[key]
		switch {
		case cur == nil:
			project, bare := splitIncidentKey(key)
			cur = &store.Incident{Key: bare, Project: project}
			incs[key] = cur
		case cur.Open:
			inc = *cur // an expired one goes back still Open, for its scheduled sends
			open = b.now.Sub(cur.LastSent) <= b.p.d.Config.IncidentTTL
		}
		cur.Open, cur.Updated = false, b.now
		if open && b.p.afterClose != nil {
			b.p.afterClose()
		}
		b.withdraw(cur, key)
		var cerr error
		if gone, cerr = b.p.d.Store.CancelIncident(key); cerr != nil {
			b.fail(cerr)
		}
		return nil
	})
	return inc, gone, open, err
}

// withdraw lists on the closed record every waiting message of the key, queued or in flight, so
// each is dropped when its check runs, even one a flush has claimed already. If they cannot be
// listed, the time rule of resolvedBefore still holds, and the failure is reported.
func (b *batch) withdraw(cur *store.Incident, key string) {
	waiting, err := b.p.d.Store.WaitingOf(key)
	if err != nil {
		b.fail(err)
	}
	for _, k := range waiting {
		if !slices.Contains(cur.Withdrawn, k) {
			cur.Withdrawn = append(cur.Withdrawn, k)
		}
	}
}

// cancelled reports removed queue entries.
func cancelled(gone []store.Pending) []ChannelResult {
	out := make([]ChannelResult, 0, len(gone))
	for _, pd := range gone {
		out = append(out, ChannelResult{Channel: instanceOf(pd.Key), Status: ChannelCancelled})
	}
	return out
}

// cancelScheduled cancels, through its channel, each send a channel holds for later delivery. A
// channel that is not built, or no longer points where the send went, cannot cancel it now; the
// cancellation is queued for the day it can, and reported. One that fails to cancel is reported:
// the human may still be paged for a resolved incident.
func (b *batch) cancelScheduled(ctx context.Context, inc store.Incident) []ChannelResult {
	return b.cancelHeld(ctx, heldSends(&inc), b.fail)
}

// cancelAside cancels the held sends a notify replaced, after its own sends went out, so a slow
// cancel cannot cost the page its deadline. A failed cancel is queued or dead-lettered, where status
// shows it, and does not change the exit code of the page: its problem comes back as a warning. A
// store failure is still an error.
func (b *batch) cancelAside(ctx context.Context, held []heldSend) []error {
	var warns []error
	b.cancelHeld(ctx, held, func(err error) {
		for _, e := range splitErrs(err) {
			if pr, ok := problem.As(e); ok && pr.Kind != problem.Store {
				warns = append(warns, e)
			} else {
				b.fail(e)
			}
		}
	})
	return warns
}

// splitErrs lists the errors inside err, looking inside errors.Join.
func splitErrs(err error) []error {
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		return []error{err}
	}
	var out []error
	for _, e := range joined.Unwrap() {
		out = append(out, splitErrs(e)...)
	}
	return out
}

// cancelHeld cancels held sends through their channels, or queues their cancellation. Each failure
// goes to fail.
func (b *batch) cancelHeld(ctx context.Context, held []heldSend, fail func(error)) []ChannelResult {
	var out []ChannelResult
	for _, h := range held {
		ch, built := b.p.d.Channels[h.Instance]
		if built && h.Fingerprint != "" && h.Fingerprint == b.p.fingerprint(h.Instance) {
			res, err := b.cancelNow(ctx, ch, h)
			if err != nil {
				fail(err)
			}
			out = append(out, res)
			continue
		}
		res := ChannelResult{Channel: h.Instance, level: h.Level}
		res.Status, res.Error = ChannelQueued, "the cancellation of its scheduled send is queued until the channel can be built as it was"
		if err := b.p.queueCancel(h); err != nil {
			res.Status, res.Error = ChannelDead, "its scheduled send could not be queued for cancellation: "+err.Error()
			fail(err)
		} else {
			fail(problem.Configf("a scheduled send on %s could not be cancelled yet; it is queued for cancellation",
				"channel "+h.Instance+" built from the destination the send went through",
				"tell the human: channel "+h.Instance+" must be buildable again (they set its secret with `yo-human secret set <name>` in their own terminal, or restore it in the config); then run `yo-human flush`", h.Instance))
		}
		out = append(out, res)
	}
	return out
}

// resolveNote is the message that tells a channel its incident was resolved. It is valid for the
// generation it closes only.
func resolveNote(inc store.Incident, id, note string, now time.Time) channel.Message {
	title := inc.Latest
	if title == "" {
		title = inc.Key
	}
	return channel.Message{ID: id, Level: inc.Level, Title: "Resolved: " + title, Body: note,
		DedupKey: inc.Key, Project: inc.Project, Created: now, Resolved: true}
}

// enqueueNotes queues a resolve note for each destination that delivered something for the incident,
// each pinned to its own fingerprint: an instance repointed between two deliveries gets one note
// for each place a message went. A note round has one note per instance, so each has its own id. A
// queue failure is recorded on the batch, and only what reached the queue is returned.
func (b *batch) enqueueNotes(id, note string, inc store.Incident) (entries []entry, sent []PlannedSend) {
	for round := 0; ; round++ {
		var plan []PlannedSend
		for _, r := range inc.Reached {
			if round < len(r.Delivered) {
				plan = append(plan, PlannedSend{Instance: r.Instance, Interruption: b.p.quietest(r.Instance), Pinned: true, Fingerprint: r.Delivered[round]})
			}
		}
		if len(plan) == 0 {
			return entries, sent
		}
		noteID := id
		if round > 0 {
			noteID = b.p.d.NewID()
		}
		queued, err := b.p.enqueue(resolveNote(inc, noteID, note, b.now), inc.Generation, plan)
		entries, sent = append(entries, queued...), append(sent, plan[:len(queued)]...)
		if err != nil {
			b.fail(err)
			return entries, sent
		}
	}
}

var quietToLoud = []channel.Interruption{channel.Passive, channel.Active, channel.TimeSensitive, channel.Critical}

// quietest is the gentlest interruption the channel offers: passive, unless its capabilities list
// interruptions and leave passive out.
func (p *Pipeline) quietest(instance string) channel.Interruption {
	have := p.d.Caps[instance].Interruptions
	if have == nil {
		return channel.Passive
	}
	for _, i := range quietToLoud {
		if have[i] {
			return i
		}
	}
	return channel.Passive
}
