package bus

import "time"

// OutboxKind says why a message is in the outbox.
type OutboxKind string

// The outbox kinds.
const (
	OutboxDuetPost   OutboxKind = "duet_post"
	OutboxEscalation OutboxKind = "escalation"
	OutboxPage       OutboxKind = "page"
	OutboxTest       OutboxKind = "test"
)

// OutboxState is where an outbox row is in its life: pending until a channel accepts it (sent),
// refuses it (failed), or the bus no longer wants it sent (superseded).
type OutboxState string

// The outbox states.
const (
	OutboxPending    OutboxState = "pending"
	OutboxSent       OutboxState = "sent"
	OutboxFailed     OutboxState = "failed"
	OutboxSuperseded OutboxState = "superseded"
)

// OutboxRow is one message the owner must be told about, written in the same transaction as the
// post or escalation it carries and sent at least once. Level is a notify level's name: bus
// imports no other hosted package. ItemID and Revision are empty and zero for a row that names
// no item (a test push).
type OutboxRow struct {
	ID                                int64
	Kind                              OutboxKind
	Swarm, ItemID                     string
	Revision                          int
	Level, Title, Body                string
	State                             OutboxState
	Attempts                          int
	NextAt, Created, SentAt, FailedAt time.Time
	LastStatus                        int
	Retried                           bool
}

// PushSource is what wrote an owner-push ledger row: an outbox row, or a collapsed nudge.
type PushSource string

// The push sources.
const (
	PushOutbox PushSource = "outbox"
	PushNudge  PushSource = "nudge"
)

// Push is one row of the owner-push ledger: item ItemID (at Revision) reached the owner's phone
// at At. SourceID is the outbox id, or the nudge's sequence number.
type Push struct {
	Source   PushSource
	SourceID int64
	ItemID   string
	Revision int
	At       time.Time
}

// NudgeState is the collapsed nudge's memory. LastSeq is the last event it has looked at;
// LastSent starts the window; Count is the number of nudges sent, which names the next one.
// LastStatus and FailedAt record the last nudge a channel refused.
type NudgeState struct {
	LastSeq, Count     int64
	LastSent, FailedAt time.Time
	LastStatus         int
}

// Deferred is an item a nudge may not count until DueAt, because it reached the owner less than
// the push window ago.
type Deferred struct {
	ItemID string
	DueAt  time.Time
}

// BrokenFlag is the sticky mark that the channel itself is refused (401 or 403). It survives a
// restart and only a 2xx clears it.
type BrokenFlag struct {
	Raised bool
	Since  time.Time
	Status int
}

// What makes a DeliveryState non-nil besides a broken channel.
const (
	// DeliveryFailedWindow is how long a refused message counts against delivery.
	DeliveryFailedWindow = 24 * time.Hour
	// DeliveryOverdue is how long a due, unsent row may wait before delivery is called stuck.
	DeliveryOverdue = 10 * time.Minute
)

// DeliveryState says the owner's phone may not be hearing from the bus: the channel is broken,
// a message was refused in the last DeliveryFailedWindow, or a pending row is DeliveryOverdue
// past its time. It is nil while delivery is healthy. Status is the broken channel's status,
// else the last refused message's. Failed counts only the last 24 hours, so one refused message
// doesn't flag delivery forever; Pending counts every pending row.
type DeliveryState struct {
	Broken                  bool
	Since                   time.Time
	Status, Failed, Pending int
}
