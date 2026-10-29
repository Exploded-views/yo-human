package bus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

// Post stores d for p: a new item, a revision of the item with the same (swarm, handle), or
// nothing when the content is unchanged. From, FromKind and Swarm come from the credential
// (Duet names the swarm in d.To), never from d.
func (b *Bus) Post(ctx context.Context, p Principal, d Draft) (PostResult, error) {
	if e := mayWrite(p); e != nil {
		return PostResult{}, e
	}
	swarm := postSwarm(p, d)
	if err := b.checkDraft(p, swarm, d); err != nil {
		return PostResult{}, err
	}
	hash := contentHash(d)
	d.RiskFlags = slices.Clone(d.RiskFlags)
	if len(d.RiskFlags) == 0 {
		d.RiskFlags = nil
	}
	var res PostResult
	err := b.s.Tx(ctx, func(x Tx) error {
		var err error
		res, err = b.upsert(x, p, swarm, d, hash)
		return err
	})
	if err != nil {
		return PostResult{}, mapStoreErr(err)
	}
	return res, nil
}

// checkDraft runs every check on a post before anything is stored: Task 3's validation, the
// rules on what may be posted (secrets and money are never on the bus, and an approval must be
// one the policy puts behind a passkey), then the swarm's content rules.
func (b *Bus) checkDraft(p Principal, swarm string, d Draft) error {
	if d.Action == ActionSecrets || d.Action == ActionMoney {
		return &Error{Code: CodeExcludedAction, What: "action", Valid: "an action that is not secrets or money",
			Fix: "do not proceed; secrets and money never go through the bus"}
	}
	if err := d.Validate(b.l); err != nil {
		return asBusError(err)
	}
	if d.Kind == KindApproval {
		if err := b.l.Approvable(d.Action, d.PolicyTarget()); err != nil {
			return asBusError(err)
		}
	}
	return b.checkSwarmContent(p, swarm, d)
}

// asBusError is err as a bus error; Validate and Approvable only return those, so anything
// else is internal.
func asBusError(err error) *Error {
	if e, ok := AsError(err); ok {
		return e
	}
	e, _ := AsError(mapStoreErr(err))
	return e
}

// postSwarm: a node's item goes to its credential's swarm; Duet's goes to the swarm it names,
// or to swarm duet when it is addressed to the human.
func postSwarm(p Principal, d Draft) string {
	switch {
	case p.Kind == PrincipalNode:
		return p.Swarm
	case d.To == "human":
		return "duet"
	}
	return d.To
}

// contentHash is the SHA-256, in hex, of the draft's encoding/json form. encoding/json writes
// struct fields in declaration order, so the bytes are deterministic; the field order of Draft
// is therefore part of the hash and must not change. Nil risk flags encode as [], so nil and
// empty are the same content.
func contentHash(d Draft) string {
	if d.RiskFlags == nil {
		d.RiskFlags = []string{}
	}
	raw, _ := json.Marshal(d) // cannot fail: the fields are strings, a string slice and a bool
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (b *Bus) upsert(x Tx, p Principal, swarm string, d Draft, hash string) (PostResult, error) {
	if d.Handle != "" {
		cur, found, err := x.ItemByHandle(swarm, d.Handle)
		if err != nil {
			return PostResult{}, err
		}
		if found {
			return b.revise(x, p, cur, d, hash)
		}
	}
	return b.create(x, p, swarm, d, hash)
}

func (b *Bus) create(x Tx, p Principal, swarm string, d Draft, hash string) (PostResult, error) {
	now := b.now().UTC()
	it := Item{Draft: d, ID: NewID("itm"), Swarm: swarm, From: p.Name, FromKind: p.Kind, Revision: 1,
		State: StateOpen, ContentHash: hash, Created: now, Updated: now}
	if it.Handle == "" {
		it.Handle = it.ID // an item without a handle is never matched by a repost
	}
	seq, err := x.AppendEvent(Event{Swarm: swarm, ItemID: it.ID, Type: EventItemPosted, Created: now})
	if err != nil {
		return PostResult{}, err
	}
	it.LastSeq = seq
	if err := x.InsertItem(it); err != nil {
		return PostResult{}, err
	}
	return PostResult{Item: it, Outcome: OutcomeCreated}, nil
}

// revise handles a repost of an existing (swarm, handle). Only the item's author may repost it:
// a changed repost rewrites the item but keeps its From, so another author's rewrite would be
// attributed to the first.
func (b *Bus) revise(x Tx, p Principal, cur Item, d Draft, hash string) (PostResult, error) {
	if cur.From != p.Name || cur.FromKind != p.Kind {
		return PostResult{}, &Error{Code: CodeForbidden, What: "handle", Valid: "a handle you posted, or a new one",
			Fix: "post under a new handle; only an item's author can revise it"}
	}
	if cur.ContentHash == hash {
		return PostResult{Item: cur, Outcome: OutcomeUnchanged}, nil
	}
	now := b.now().UTC()
	last, err := x.AppendEvent(Event{Swarm: cur.Swarm, ItemID: cur.ID, Type: EventItemRevised, Created: now})
	if err != nil {
		return PostResult{}, err
	}
	dec, hadDecision, err := x.Decision(cur.ID, cur.Revision)
	if err != nil {
		return PostResult{}, err
	}
	if hadDecision && dec.Kind == RecordApproval {
		if last, err = x.AppendEvent(Event{Swarm: cur.Swarm, ItemID: cur.ID, Type: EventApprovalVoided, Created: now}); err != nil {
			return PostResult{}, err
		}
	}
	next := cur
	next.Draft, next.Revision, next.State = d, cur.Revision+1, StateOpen
	next.ContentHash, next.LastSeq, next.Updated = hash, last, now
	if err := x.UpdateItem(next); err != nil {
		return PostResult{}, err
	}
	return PostResult{Item: next, Outcome: OutcomeRevised}, nil
}
