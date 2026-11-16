package bus

import (
	"context"
	"time"
)

// RelayApproval stores the owner's words on an approval as Duet passes them on, and decides
// nothing: the relay is an agent's, it appends words_relayed, and the item stays open at its
// revision, so the owner's own passkey approval still succeeds. Only Duet calls it (a node relays
// with RelayWords, the owner decides on the approval page). It runs the rules a decision on an
// approval does, so Duet learns at once that it cannot be decided: the item must be an approval
// the policy still puts behind a passkey (not_approvable, standing or excluded_action), at the
// revision the owner saw (stale_revision) that has no decision yet (already_decided, with the winner), with a decision of approve or reject whose text, in a
// walled swarm, is exactly go sl:<n> or hold sl:<n> to match. The decision is checked, not stored:
// the relay keeps the owner's words, and the words are what an agent reads.
func (b *Bus) RelayApproval(ctx context.Context, p Principal, in ApprovalInput) (Relay, error) {
	if p.Kind != PrincipalDuet {
		return Relay{}, forbidden("Duet's credential", "only Duet relays the owner's words on an approval; he resolves it himself on the approval page")
	}
	rq := decideReq{itemID: in.ItemID, rev: in.Revision, kind: RecordApproval, decision: in.Decision, text: in.Text}
	var r Relay
	var swarm string
	err := b.s.Tx(ctx, func(x Tx) error {
		it, err := b.decidable(x, p, rq)
		if err != nil {
			return err
		}
		swarm = it.Swarm
		if cur, found, err := x.Decision(it.ID, it.Revision); err != nil || found {
			if err != nil {
				return err
			}
			return alreadyDecided(cur) // the revision is closed: Duet must not hand out a link to it
		}
		if err := b.checkApprovalText(it.Swarm, rq); err != nil {
			return err
		}
		r, err = b.storeRelay(x, it, p, in.Text, in.HumanTS)
		return err
	})
	if err != nil {
		return Relay{}, mapStoreErr(err)
	}
	b.committed(swarm)
	return r, nil
}

// storeRelay inserts the relay and appends its words_relayed event in the caller's transaction.
func (b *Bus) storeRelay(x Tx, it Item, p Principal, text string, humanTS time.Time) (Relay, error) {
	now := b.now().UTC()
	r := Relay{ID: NewID("rly"), ItemID: it.ID, Revision: it.Revision, Text: text, Source: relaySource(p), By: p.Name,
		HumanTS: humanTS.UTC(), Recorded: now}
	if err := x.InsertRelay(r); err != nil {
		return Relay{}, err
	}
	return r, b.appendEvents(x, it, now, []EventType{EventWordsRelayed})
}
