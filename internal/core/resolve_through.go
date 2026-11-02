package core

import (
	"context"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// ResolveThrough is the resolve of the page shim for a key with no open incident. The old paging
// script always told PagerDuty to resolve, so the shim must too, whether the key was opened before
// yo-human held it or its incident has expired.
//
// It closes the project's record of key as Resolve does (queued sends removed, waiting messages
// withdrawn, scheduled sends of an expired incident cancelled). Then, if the record still lists
// where the key was delivered, each of those destinations hears of it exactly as in Resolve: pinned
// to the fingerprint that delivered, with the key as the record holds it. Otherwise a resolve for
// the key goes to each of instances (the instances of level that can resolve) as ProviderKey makes it
// for sendProject: bare for none, which is what the old script sent and PagerDuty holds, and inside
// the project's namespace otherwise. project is the record's project, sendProject the one whose
// namespace the key is sent in. Notes go through the queue like any send.
//
// It is never a noop. A close queued for another try is StatusQueued and a close refused for good
// is StatusFailed, with the same problems Resolve reports.
func (p *Pipeline) ResolveThrough(ctx context.Context, project, sendProject, key, level string, instances []string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.d.Config.Deadline)
	defer cancel()
	p = p.within(ctx)
	now := p.d.Now()
	if err := p.recoverStale(); err != nil {
		return Result{}, err
	}
	id := p.d.NewID()
	incID := IncidentKey(project, key)
	b := &batch{p: p, now: now}
	inc, gone, _, err := b.closeIncident(incID)
	if err != nil {
		return Result{ID: id}, err
	}
	res := Result{Status: StatusResolved, ID: id, Channels: cancelled(gone)}
	var cancels []ChannelResult
	if inc.Open { // expired: over already, but its held pages must still not fire
		cancels = b.cancelScheduled(ctx, inc)
	}
	entries, notes := b.enqueueNotes(id, "", b.prior)
	if len(notes) == 0 {
		entries, notes = b.enqueueBare(id, sendProject, key, level, instances)
	}
	closes := b.fanOut(ctx, entries, notes)
	res.Channels = append(append(res.Channels, cancels...), closes...)
	res.Next = b.closeOutcome(incID, cancels, closes)
	res.Status = b.throughStatus(closes)
	skip := map[string]bool{}
	for _, e := range entries {
		skip[e.pending.Key] = true
	}
	b.backlog(ctx, skip)
	err = busyAsWarnings(&res, b.err())
	p.withNext(&res, err, level)
	return res, err
}

// enqueueBare queues a resolve for the key, in project's namespace, on each instance, at the instance's quietest
// interruption, and returns what reached the queue.
func (b *batch) enqueueBare(id, project, key, level string, instances []string) ([]entry, []PlannedSend) {
	plan := make([]PlannedSend, 0, len(instances))
	for _, name := range instances {
		plan = append(plan, PlannedSend{Instance: name, Interruption: b.p.quietest(name)})
	}
	m := channel.Message{ID: id, Level: level, Title: "Resolved: " + key, DedupKey: key, Project: project, Created: b.now, Resolved: true}
	entries, err := b.p.enqueue(m, b.prior.Generation, plan)
	if err != nil {
		b.fail(err)
	}
	return entries, plan[:len(entries)]
}

// throughStatus is the status of a pass-through: the first close on a channel that can resolve that
// is not done decides it, in the order closeOutcome reports its problems, so the status and the
// exit code agree.
func (b *batch) throughStatus(closes []ChannelResult) Status {
	for _, c := range closes {
		if !b.p.d.Caps[c.Channel].Resolve {
			continue
		}
		switch c.Status {
		case ChannelQueued:
			return StatusQueued
		case ChannelDead:
			return StatusFailed
		}
	}
	return StatusResolved
}
