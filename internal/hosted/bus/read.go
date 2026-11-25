package bus

import (
	"context"
	"time"
)

// Summary is one line of the inbox. Every text field is untrusted data. Kind and Urgency are
// added to the plan's fields so an item in the Interrupt group still says what it is.
type Summary struct {
	ID, Handle, From, Swarm string
	Severity                Severity
	Title, Summary          string
	Revision                int
	Created                 time.Time
	Link                    string
	Untrusted               bool
	Kind                    Kind
	Urgency                 Urgency
	LastSeq                 int64
}

// Inbox is what Duet reads.
type Inbox struct {
	Groups    map[Kind][]Summary
	Interrupt []Summary
	Threads   []Summary
	Next      int64
	// Delivery is non-nil when the owner's phone may not be hearing from the bus.
	Delivery *DeliveryState
}

// ItemView is an item with its thread and the decision for its current revision.
type ItemView struct {
	Item     Item
	Messages []Message
	Decision *Record
	// Delivery is set for Duet only, and only while delivery is not healthy.
	Delivery *DeliveryState
}

// inboxKinds are the five kinds that have an inbox group. A notice has none.
var inboxKinds = []Kind{KindApproval, KindQuestion, KindBlocker, KindFinished, KindFailed}

func forbiddenUnlessDuet(p Principal) *Error {
	if p.Kind == PrincipalDuet {
		return nil
	}
	return &Error{Code: CodeForbidden, What: "credential", Valid: "Duet's credential", Fix: "this tool is Duet's; a node reads its own swarm with answers"}
}

func summarize(it Item) Summary {
	return Summary{ID: it.ID, Handle: it.Handle, From: it.From, Swarm: it.Swarm, Severity: it.Severity, Title: it.Title,
		Summary: it.Summary, Revision: it.Revision, Created: it.Created, Link: it.Link, Untrusted: it.Untrusted,
		Kind: it.Kind, Urgency: it.Urgency, LastSeq: it.LastSeq}
}

// Inbox is what needs the owner's attention, for Duet only: the items addressed to the owner
// that someone else posted, open or decided, with activity after both the ack mark and since, grouped by kind.
// An interrupt-when-good item sits in Interrupt and in no kind group, read-only: reading
// changes nothing, and the bus never pushes on that lane. Threads are Duet's own items whose
// latest message came from someone else. Next is the highest seq of anything returned, or
// since when nothing is.
func (b *Bus) Inbox(ctx context.Context, p Principal, since int64) (Inbox, error) {
	if e := forbiddenUnlessDuet(p); e != nil {
		return Inbox{}, e
	}
	in := newInbox(since)
	err := b.s.Tx(ctx, func(x Tx) error {
		var err error
		if in.Delivery, err = x.DeliveryState(b.now()); err != nil {
			return err
		}
		open, err := x.ListInboxItems(since)
		if err != nil {
			return err
		}
		for _, it := range open {
			in.addOpen(it)
		}
		threads, err := x.ListThreadItems(since)
		if err != nil {
			return err
		}
		for _, it := range threads {
			if err := in.addThread(x, it); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Inbox{}, mapStoreErr(err)
	}
	return in, nil
}

func newInbox(since int64) Inbox {
	in := Inbox{Groups: map[Kind][]Summary{}, Interrupt: []Summary{}, Threads: []Summary{}, Next: since}
	for _, k := range inboxKinds {
		in.Groups[k] = []Summary{}
	}
	return in
}

func (in *Inbox) addOpen(it Item) {
	g, ok := in.Groups[it.Kind]
	if !ok || it.FromKind == PrincipalDuet {
		return
	}
	if it.Urgency == UrgencyInterruptWhenGood {
		in.Interrupt = append(in.Interrupt, summarize(it))
	} else {
		in.Groups[it.Kind] = append(g, summarize(it))
	}
	in.Next = max(in.Next, it.LastSeq)
}

func (in *Inbox) addThread(x Tx, it Item) error {
	msgs, err := x.Messages(it.ID)
	if err != nil {
		return err
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].FromKind == PrincipalDuet {
		return nil
	}
	in.Threads = append(in.Threads, summarize(it))
	in.Next = max(in.Next, it.LastSeq)
	return nil
}

// Item is one item with its thread and the decision for its current revision, for anyone who
// may see it. An item the caller can't see and one that doesn't exist give the same error.
func (b *Bus) Item(ctx context.Context, p Principal, id string) (ItemView, error) {
	var v ItemView
	err := b.s.Tx(ctx, func(x Tx) error {
		it, found, err := x.ItemByID(id)
		if err != nil {
			return err
		}
		if !found || !b.visible(p, it) {
			return notFoundErr()
		}
		v.Item = it
		if v.Messages, err = x.Messages(it.ID); err != nil {
			return err
		}
		rec, ok, err := x.Decision(it.ID, it.Revision)
		if err != nil {
			return err
		}
		if ok {
			v.Decision = &rec
		}
		if p.Kind == PrincipalDuet { // channel health is Duet's to see
			v.Delivery, err = x.DeliveryState(b.now())
		}
		return err
	})
	if err != nil {
		return ItemView{}, mapStoreErr(err)
	}
	return v, nil
}

// Ack hides an item from the inbox until it has new activity after what Duet saw: it sets
// AckedSeq to seenSeq (the LastSeq of the summary Duet read), at most the item's LastSeq, and never
// lowers it. Duet only.
func (b *Bus) Ack(ctx context.Context, p Principal, id string, seenSeq int64) error {
	if e := forbiddenUnlessDuet(p); e != nil {
		return e
	}
	err := b.s.Tx(ctx, func(x Tx) error {
		it, found, err := x.ItemByID(id)
		if err != nil {
			return err
		}
		if !found {
			return notFoundErr()
		}
		it.AckedSeq = max(it.AckedSeq, min(seenSeq, it.LastSeq))
		return x.UpdateItem(it)
	})
	return mapStoreErr(err)
}

// Waiting is how many items the inbox holds for Claude: the items in its groups, not the
// interrupt lane. A collapsed nudge reports it.
func (b *Bus) Waiting(ctx context.Context) (int, error) {
	in := newInbox(0)
	err := b.s.Tx(ctx, func(x Tx) error {
		open, err := x.ListInboxItems(0)
		for _, it := range open {
			in.addOpen(it)
		}
		return err
	})
	if err != nil {
		return 0, mapStoreErr(err)
	}
	n := 0
	for _, g := range in.Groups {
		n += len(g)
	}
	return n, nil
}
