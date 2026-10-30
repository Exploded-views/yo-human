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

	Decision(itemID string, rev int) (Record, bool, error)
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
