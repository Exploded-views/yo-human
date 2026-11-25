package bus

import (
	"context"
	"encoding/json"
	"time"
)

// Store is the bus's one way to read and write state. Every call runs in a transaction that
// commits when fn returns nil and rolls back otherwise. internal/hosted/db implements it.
type Store interface {
	Tx(ctx context.Context, fn func(Tx) error) error
}

// Tx is one transaction. Not safe for use after fn returns or from another goroutine.
type Tx interface {
	ItemByID(id string) (Item, bool, error)
	ItemByHandle(swarm, handle string) (Item, bool, error)
	InsertItem(Item) error
	UpdateItem(Item) error
	ListOpenForHuman() ([]Item, error)
	// ListThreadItems returns the items Duet started (from_kind duet, addressed to a swarm), open
	// or decided, whose last_seq is above both since and acked_seq, in last_seq order.
	ListThreadItems(since int64) ([]Item, error)
	// ListInboxItems returns the items addressed to the human, open or decided, whose last_seq is
	// above both since and acked_seq, oldest first. (Task 7: a reply on a decided item is news.)
	ListInboxItems(since int64) ([]Item, error)

	Decision(itemID string, rev int) (Record, bool, error)
	// DecisionRecorded returns the record of the given kind whose Recorded time equals recorded,
	// for the item (the human's before an agent's, if both match).
	//
	// THE EVENT CONVENTION (binds every write path, Task 6 included): a write that stores an answer
	// or approval appends its answer_recorded, approval_recorded or answer_superseded event in the
	// same transaction with Event.Created equal to the record's Recorded. Answers finds the record an
	// event wrote by that equality, because an event names no revision.
	DecisionRecorded(itemID string, kind RecordKind, recorded time.Time) (Record, bool, error)
	// InsertDecision stores a decision row. Convention for readers of the event log: the event
	// the bus appends for a row (answer_recorded, approval_recorded, words_relayed for a relay)
	// has Created equal to the row's Recorded, and answer_superseded has Created equal to the
	// human row's Recorded. An event has no row id or revision, so a reader finds its row by
	// (item, Recorded): that row's revision is the event's, and the agent row it supersedes is
	// the agent row of the same item and revision. Pinned by TestSupersededEventIdentifiesRows.
	InsertDecision(Record) error

	InsertRelay(Relay) error
	Relays(itemID string) ([]Relay, error)

	InsertMessage(Message) error
	Messages(threadID string) ([]Message, error)

	// AppendEvent returns the new event's seq. EventsSince returns events with seq greater
	// than since, in seq order, at most limit; an empty swarm means all.
	AppendEvent(Event) (int64, error)
	EventsSince(swarm string, since int64, limit int) ([]Event, error)

	PutStatus(StatusDoc) error
	Statuses(swarm string) ([]StatusDoc, error)

	// The outbox, the owner-push ledger, the nudge's memory and the delivery flag (Task 11).
	// InsertOutbox returns the new row's id. PendingOutbox returns the pending rows whose NextAt
	// is not after now, oldest first, at most limit. UpdateOutbox rewrites every field but ID,
	// Kind, Swarm, ItemID, Revision, Title, Body and Created. FailedSince returns the failed rows
	// whose FailedAt is not before since. LastPush is the newest ledger time for the item, at any
	// revision; LastPageSent is the newest SentAt of a page row of the swarm. A zero NudgeState
	// and BrokenFlag are what nothing has written yet. PutDeferred inserts or replaces.
	// DeliveryState is nil while delivery is healthy.
	InsertOutbox(OutboxRow) (int64, error)
	UpdateOutbox(OutboxRow) error
	PendingOutbox(now time.Time, limit int) ([]OutboxRow, error)
	FailedSince(since time.Time) ([]OutboxRow, error)
	AllOutbox() ([]OutboxRow, error)
	LastPush(itemID string) (time.Time, bool, error)
	InsertPush(Push) error
	Pushes(itemID string) ([]Push, error)
	LastPageSent(swarm string) (time.Time, bool, error)
	NudgeState() (NudgeState, error)
	PutNudgeState(NudgeState) error
	Deferred() ([]Deferred, error)
	PutDeferred(Deferred) error
	DeleteDeferred(itemID string) error
	BrokenFlag() (BrokenFlag, error)
	PutBrokenFlag(BrokenFlag) error
	DeliveryState(now time.Time) (*DeliveryState, error)
}

// Relay is the owner's words passed on by a node. Its actor kind is always agent, and it
// never decides an item.
type Relay struct {
	ID, ItemID        string
	Revision          int
	Text, Source, By  string
	HumanTS, Recorded time.Time
}

// Message is one entry in an item's thread.
type Message struct {
	ID, ThreadID, From string
	FromKind           PrincipalKind
	Body               string
	Severity           Severity
	Urgency            Urgency
	Created            time.Time
}

// EventType names what an event records.
type EventType string

// The event types. The desk_* events belong to no swarm (Swarm is empty, which ValidName never
// allows for a swarm), so EventsSince for a swarm never returns them.
const (
	EventItemPosted        EventType = "item_posted"
	EventItemRevised       EventType = "item_revised"
	EventAnswerRecorded    EventType = "answer_recorded"
	EventApprovalRecorded  EventType = "approval_recorded"
	EventApprovalVoided    EventType = "approval_voided"
	EventMessageAdded      EventType = "message_added"
	EventStatusPushed      EventType = "status_pushed"
	EventWordsRelayed      EventType = "words_relayed"
	EventItemEscalated     EventType = "item_escalated"
	EventDeskSigninStarted EventType = "desk_signin_started"
	EventDeskMinted        EventType = "desk_token_minted"
	EventAnswerSuperseded  EventType = "answer_superseded"
)

// Event is one row of the append-only event log.
type Event struct {
	Seq                       int64
	Swarm, ItemID             string
	Type                      EventType
	Created                   time.Time
	Starter, Phrase, UserCode string
	Expires                   time.Time
}

// StatusDoc is a swarm's latest status document.
type StatusDoc struct {
	Swarm   string
	Doc     json.RawMessage
	Updated time.Time
}
