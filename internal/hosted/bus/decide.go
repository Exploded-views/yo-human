package bus

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// AnswerInput is a question's or blocker's answer as a caller submits it. It has no actor kind,
// recorder or source of its own: those come from the credential. Source is accepted and ignored
// so a client that sends one can't say who it is.
type AnswerInput struct {
	ItemID        string
	Revision      int
	Text          string
	HumanTS       time.Time
	Source        string
	SourceMessage string
}

// ApprovalInput is an approval's decision as a caller submits it. Source is accepted and ignored.
type ApprovalInput struct {
	ItemID        string
	Revision      int
	Decision      Decision
	Text          string
	HumanTS       time.Time
	Source        string
	SourceMessage string
}

// RelayInput is the owner's words as a node or Duet passes them on. Source is accepted and ignored.
type RelayInput struct {
	ItemID   string
	Revision int
	Text     string
	HumanTS  time.Time
	Source   string
}

// decideReq is what RecordAnswer and RecordApproval share once the principal is allowed.
type decideReq struct {
	itemID, text, sourceMessage string
	rev                         int
	kind                        RecordKind
	decision                    Decision
	humanTS                     time.Time
}

// RecordAnswer records the answer to a question or blocker. Only Duet (an agent answer) and,
// under desk Option A, the desk (a human one) may call it; the actor kind comes from that
// credential alone. A node relays the owner's words instead (RelayWords).
func (b *Bus) RecordAnswer(ctx context.Context, p Principal, in AnswerInput) (Record, error) {
	if p.Kind != PrincipalDuet && p.Kind != PrincipalDesk {
		return Record{}, forbidden("a Duet or desk credential", "a node passes the owner's words on with relay; it never answers")
	}
	return b.decide(ctx, p, decideReq{itemID: in.ItemID, rev: in.Revision, kind: RecordAnswer, text: in.Text,
		sourceMessage: in.SourceMessage, humanTS: in.HumanTS})
}

// RecordApproval records the owner's decision on an approval. Only a passkey principal, which
// only the approval page creates from a verified assertion, may call it: no other credential ever
// resolves an approval.
func (b *Bus) RecordApproval(ctx context.Context, p Principal, in ApprovalInput) (Record, error) {
	if p.Kind != PrincipalPasskey || p.Name == "" {
		return Record{}, forbidden("a passkey credential, from the approval page",
			"approvals resolve only through the approval page with your passkey; Duet sends you the link")
	}
	return b.decide(ctx, p, decideReq{itemID: in.ItemID, rev: in.Revision, kind: RecordApproval, decision: in.Decision,
		text: in.Text, sourceMessage: in.SourceMessage, humanTS: in.HumanTS})
}

func forbidden(valid, fix string) *Error {
	return &Error{Code: CodeForbidden, What: "credential", Valid: valid, Fix: fix}
}

// decide runs the rules in one transaction. A unique-constraint conflict means a racing writer
// committed between the transaction's read and its insert; the transaction is rolled back and run
// once more, so rule 6 settles it from what that writer stored.
func (b *Bus) decide(ctx context.Context, p Principal, rq decideReq) (Record, error) {
	var rec Record
	var err error
	for range 2 {
		err = b.s.Tx(ctx, func(x Tx) error {
			var err error
			rec, err = b.decideTx(x, p, rq)
			return err
		})
		if !errors.Is(err, ErrConflict) {
			break
		}
	}
	if err != nil {
		return Record{}, mapStoreErr(err)
	}
	return rec, nil
}

func (b *Bus) decideTx(x Tx, p Principal, rq decideReq) (Record, error) {
	it, err := b.decidable(x, p, rq)
	if err != nil {
		return Record{}, err
	}
	if err := b.checkDecisionInput(it, rq); err != nil {
		return Record{}, err
	}
	rec := Record{ItemID: it.ID, Revision: it.Revision, Kind: rq.kind, Decision: rq.decision, ActorKind: p.ActorKind(),
		Text: rq.text, Source: recordSource(p), By: p.Name, SourceMessage: rq.sourceMessage,
		HumanTS: rq.humanTS.UTC(), Recorded: b.now().UTC()}
	return b.settle(x, it, rec)
}

// recordSource is the credential's own name for itself, never the caller's.
func recordSource(p Principal) string {
	switch p.Kind {
	case PrincipalPasskey:
		return "passkey:" + p.Name
	case PrincipalDesk:
		return "desk"
	}
	return "duet"
}

// decidable is rules 1 to 4: the item is visible to p, of the right kind, not excluded, and at
// the revision the caller saw.
func (b *Bus) decidable(x Tx, p Principal, rq decideReq) (Item, error) {
	it, found, err := x.ItemByID(rq.itemID)
	if err != nil {
		return Item{}, err
	}
	if !found || !b.visible(p, it) {
		return Item{}, notFoundErr()
	}
	if e := b.checkDecidableKind(it, rq.kind); e != nil {
		return Item{}, e
	}
	if rq.rev != it.Revision {
		return Item{}, staleErr(it.Revision)
	}
	return it, nil
}

func staleErr(current int) *Error {
	return &Error{Code: CodeStaleRevision, What: "revision", Valid: "the item's current revision, " + strconv.Itoa(current),
		Fix: "read the item and decide on its current revision"}
}

// checkDecidableKind is rules 2 and 3: approvals only on approvals, answers only on questions and
// blockers, and never an excluded action.
func (b *Bus) checkDecidableKind(it Item, kind RecordKind) error {
	if kind == RecordApproval {
		if it.Kind != KindApproval {
			return notApprovable("approval")
		}
		return b.l.Approvable(it.Action, it.PolicyTarget())
	}
	if it.Kind != KindQuestion && it.Kind != KindBlocker {
		return notApprovable("question or blocker")
	}
	if it.Action == ActionSecrets || it.Action == ActionMoney {
		return &Error{Code: CodeExcludedAction, What: "action", Valid: "an action that is not secrets or money",
			Fix: "do not proceed; secrets and money never go through the bus"}
	}
	return nil
}

func notApprovable(want string) *Error {
	return &Error{Code: CodeNotApprovable, What: "item", Valid: "an item of kind " + want, Fix: "check the item's kind before deciding it"}
}

// checkDecisionInput is rules 4a and 5: a walled answer is go or hold, an approval has a decision,
// and the text is non-empty after trimming, within the limit, and plain.
func (b *Bus) checkDecisionInput(it Item, rq decideReq) error {
	if rq.kind == RecordAnswer {
		return b.checkWords(it.Swarm, rq.text)
	}
	if rq.decision != DecisionApprove && rq.decision != DecisionReject {
		return invalid("decision", "approve or reject")
	}
	return checkText(rq.text)
}

// checkWords is the content rule for the owner's words entering a swarm: behind the wall they are
// exactly go sl:<n> or hold sl:<n>, anywhere they are plain text of a bounded size.
func (b *Bus) checkWords(swarm, text string) error {
	if b.isWalled(swarm) && !wallGoHoldRE.MatchString(text) {
		return wallInvalid("text", "exactly go sl:<n> or hold sl:<n>")
	}
	return checkText(text)
}

func checkText(text string) error {
	if strings.TrimSpace(text) == "" || len(text) > MaxAnswer || !utf8.ValidString(text) || strings.ContainsFunc(text, isRefusedRune) {
		return invalid("text", "1 to 4096 bytes of valid UTF-8 with no control, format or separator characters (newline, tab, ZWNJ and ZWJ are allowed)")
	}
	return nil
}

// settle is rules 6 and 7. Decision returns an item revision's effective record, its human one if
// there is one. The same actor kind: an identical retry gets its own record back, anything else
// loses to the first. A human after an agent supersedes it; an agent after a human is refused.
func (b *Bus) settle(x Tx, it Item, rec Record) (Record, error) {
	cur, found, err := x.Decision(it.ID, it.Revision)
	switch {
	case err != nil:
		return Record{}, err
	case !found:
		return b.storeRecord(x, it, rec, false)
	case cur.ActorKind == rec.ActorKind:
		return sameRecord(cur, rec)
	case rec.ActorKind == ActorHuman:
		return b.storeRecord(x, it, rec, true)
	}
	return Record{}, alreadyDecided(cur)
}

// sameRecord returns cur for a retry that matches it in every field the caller controls.
func sameRecord(cur, rec Record) (Record, error) {
	if cur.Kind == rec.Kind && cur.Decision == rec.Decision && cur.Text == rec.Text && cur.Source == rec.Source && cur.By == rec.By {
		return cur, nil
	}
	return Record{}, alreadyDecided(cur)
}

func alreadyDecided(winner Record) *Error {
	return &Error{Code: CodeAlreadyDecided, What: "item", Valid: "an item that is still open at this revision", Winner: &winner,
		Fix: "this revision was already decided; read the item to see the winning record"}
}

// storeRecord inserts rec, appends its event (and answer_superseded when it displaces an agent's
// answer) and records the last seq on the item. The insert comes first so a conflict fails before
// anything else is written.
func (b *Bus) storeRecord(x Tx, it Item, rec Record, supersedes bool) (Record, error) {
	if err := x.InsertDecision(rec); err != nil {
		return Record{}, err
	}
	types := []EventType{EventAnswerRecorded}
	if rec.Kind == RecordApproval {
		types = []EventType{EventApprovalRecorded}
	}
	if supersedes {
		types = append(types, EventAnswerSuperseded)
	}
	return rec, b.appendEvents(x, it, rec.Recorded, types)
}

// appendEvents appends one event per type for it and sets it.LastSeq to the last.
func (b *Bus) appendEvents(x Tx, it Item, now time.Time, types []EventType) error {
	for _, t := range types {
		seq, err := x.AppendEvent(Event{Swarm: it.Swarm, ItemID: it.ID, Type: t, Created: now})
		if err != nil {
			return err
		}
		it.LastSeq = seq
	}
	it.Updated = now
	return x.UpdateItem(it)
}

// RelayWords stores the owner's words as a node in the item's swarm, or Duet, passes them on.
// The relay is always an agent's; it appends words_relayed and changes nothing else about the
// item's state, and never a decision.
func (b *Bus) RelayWords(ctx context.Context, p Principal, in RelayInput) (Relay, error) {
	if !mayRelay(p) {
		return Relay{}, forbidden("a node credential or Duet's", "only a node in the item's swarm or Duet relays the owner's words")
	}
	var r Relay
	err := b.s.Tx(ctx, func(x Tx) error {
		it, err := b.relayable(x, p, in)
		if err != nil {
			return err
		}
		now := b.now().UTC()
		r = Relay{ID: NewID("rly"), ItemID: it.ID, Revision: it.Revision, Text: in.Text, Source: relaySource(p), By: p.Name,
			HumanTS: in.HumanTS.UTC(), Recorded: now}
		if err := x.InsertRelay(r); err != nil {
			return err
		}
		return b.appendEvents(x, it, now, []EventType{EventWordsRelayed})
	})
	if err != nil {
		return Relay{}, mapStoreErr(err)
	}
	return r, nil
}

// mayRelay: Duet, or a named node with a valid swarm.
func mayRelay(p Principal) bool {
	return p.Kind == PrincipalDuet || p.Kind == PrincipalNode && ValidSwarm(p.Swarm) && p.Name != ""
}

func relaySource(p Principal) string {
	if p.Kind == PrincipalDuet {
		return "duet"
	}
	return "node:" + p.Name
}

// relayable: the item is visible to p, at the revision the caller saw, and the words are allowed.
func (b *Bus) relayable(x Tx, p Principal, in RelayInput) (Item, error) {
	it, found, err := x.ItemByID(in.ItemID)
	if err != nil {
		return Item{}, err
	}
	if !found || !b.visible(p, it) {
		return Item{}, notFoundErr()
	}
	if in.Revision != it.Revision {
		return Item{}, staleErr(it.Revision)
	}
	return it, b.checkWords(it.Swarm, in.Text)
}
