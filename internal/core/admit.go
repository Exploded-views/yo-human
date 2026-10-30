package core

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/store"
)

// kindAdmit marks a queue entry holding a keyed message whose incident could not be checked: another
// process held the incident lock past the notify's deadline.
const kindAdmit = "admit"

// admitInstance stands for the channel part of a kindAdmit entry's key. No channel can have that
// name (channel names start with a letter or digit).
const admitInstance = "_admit"

// deferAdmission keeps a keyed message whose incident could not be checked. Nothing is sent, because
// that could page twice; the message waits in the queue, its sends pinned to where each instance
// points now, for a flush to check the incident and send what the check allows. The call is queued
// (exit 75), and its next step is that flush.
func (p *Pipeline) deferAdmission(m channel.Message, plan []PlannedSend) (Result, error) {
	res := Result{Status: StatusQueued, ID: m.ID, Next: p.next(m.Level)}
	pinned := make([]PlannedSend, len(plan))
	for i, s := range plan {
		s.Pinned, s.Fingerprint = true, p.fingerprint(s.Instance)
		pinned[i] = s
		res.Channels = append(res.Channels, ChannelResult{Channel: s.Instance, Status: ChannelQueued,
			Error: "waiting for its incident check: another yo-human process holds the incident lock"})
	}
	data, _ := json.Marshal(payload{Kind: kindAdmit, Message: m, Enqueued: p.d.Now(), Config: p.d.ConfigPath, Plan: pinned}) // strings, numbers and settings always encode
	if err := p.d.Store.Enqueue(store.Pending{Key: admitKey(m), IncidentKey: incidentID(m), Data: data}); err != nil {
		return Result{ID: m.ID}, err
	}
	return res, nil
}

// admitKey is the queue key of m's waiting entry.
func admitKey(m channel.Message) string { return m.ID + "." + admitInstance }

// admitDeferred checks the incident of a message a notify had to leave in the queue, and sends what
// the check allows, as the notify would have. Its sends join the batch's results. While the lock is
// still held it waits for the next flush, no attempt spent.
func (b *batch) admitDeferred(ctx context.Context, c store.Claimed, pl payload) (ChannelResult, error) {
	if b.p.beforeAdmit != nil {
		b.p.beforeAdmit()
	}
	adm, err := b.p.admit(pl.Message, pl.Plan, b.now, pl.Enqueued)
	if store.IsBusy(err) {
		res := ChannelResult{Channel: admitInstance, Status: ChannelQueued, Error: "still waiting for the incident lock",
			level: pl.Message.Level, retryAt: b.now}
		return res, errors.Join(err, b.p.d.Store.Release(c, b.now))
	}
	if err == nil {
		err = b.p.d.Store.Complete(c) // an entry left behind is admitted again later, and suppressed then
	}
	if err != nil {
		return ChannelResult{}, err
	}
	if adm.dropped {
		return ChannelResult{Channel: admitInstance, Status: ChannelCancelled, level: pl.Message.Level,
			Error: "dropped: incident " + pl.Message.DedupKey + " was resolved after this message was queued"}, nil
	}
	results := append(b.fanOut(ctx, adm.entries, adm.plan), skippedResults(adm.skipped)...)
	warns := b.cancelAside(ctx, adm.cancel)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.more = append(b.more, results...)
	b.warns = append(b.warns, warns...)
	return ChannelResult{}, nil // nothing of its own to report: its sends are
}

// EntryInstances names the channel instances a queued entry needs built: its own, or, for a message
// waiting for its incident check, every instance its level sends to.
func EntryInstances(pd store.Pending) []string {
	var pl payload
	if json.Unmarshal(pd.Data, &pl) != nil || pl.Kind != kindAdmit {
		return []string{instanceOf(pd.Key)}
	}
	out := make([]string, 0, len(pl.Plan))
	for _, s := range pl.Plan {
		out = append(out, s.Instance)
	}
	return out
}

// skippedResults reports the planned instances a call could not build.
func skippedResults(names []string) []ChannelResult {
	out := make([]ChannelResult, 0, len(names))
	for _, name := range names {
		out = append(out, ChannelResult{Channel: name, Status: ChannelSkipped,
			Error: "not sent: the channel could not be built for this call", unbuilt: true})
	}
	return out
}
