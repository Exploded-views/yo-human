package bus

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"
)

// Reply adds a message to the thread of item threadID. The thread is the item's id. An item
// the caller can't see and an item that doesn't exist give the same error. The urgency is the
// poster's lane for the message, required like an item's.
func (b *Bus) Reply(ctx context.Context, p Principal, threadID, body string, sev Severity, urg Urgency) (Message, error) {
	if e := checkReply(p, body, sev, urg); e != nil {
		return Message{}, e
	}
	var m Message
	err := b.s.Tx(ctx, func(x Tx) error {
		it, found, err := x.ItemByID(threadID)
		if err != nil {
			return err
		}
		if !found || !b.visible(p, it) {
			return notFoundErr()
		}
		if err := b.checkSwarmMessage(p, it.Swarm, body); err != nil {
			return err
		}
		m, err = b.appendMessage(x, it, p, body, sev, urg)
		return err
	})
	if err != nil {
		return Message{}, mapStoreErr(err)
	}
	return m, nil
}

// checkReply is every check that needs nothing from the store.
func checkReply(p Principal, body string, sev Severity, urg Urgency) *Error {
	if e := mayWrite(p); e != nil {
		return e
	}
	if e := checkBody(body, sev); e != nil {
		return e
	}
	return checkUrgency(p, urg)
}

func (b *Bus) appendMessage(x Tx, it Item, p Principal, body string, sev Severity, urg Urgency) (Message, error) {
	now := b.now().UTC()
	m := Message{ID: NewID("msg"), ThreadID: it.ID, From: p.Name, FromKind: p.Kind, Body: body, Severity: sev, Urgency: urg, Created: now}
	if err := x.InsertMessage(m); err != nil {
		return Message{}, err
	}
	seq, err := x.AppendEvent(Event{Swarm: it.Swarm, ItemID: it.ID, Type: EventMessageAdded, Created: now})
	if err != nil {
		return Message{}, err
	}
	it.LastSeq, it.Updated = seq, now
	return m, x.UpdateItem(it)
}

func checkBody(body string, sev Severity) *Error {
	switch {
	case strings.TrimSpace(body) == "":
		return invalid("body", "1 to 65536 bytes of text")
	case len(body) > MaxBody:
		return invalid("body", "1 to 65536 bytes of text")
	case !utf8.ValidString(body) || strings.ContainsFunc(body, isRefusedRune):
		return invalid("body", "valid UTF-8 with no control, format or separator characters (newline, tab, ZWNJ and ZWJ are allowed)")
	case !slices.Contains(severities, sev):
		return invalid("severity", "low, normal, high or critical")
	}
	return nil
}
