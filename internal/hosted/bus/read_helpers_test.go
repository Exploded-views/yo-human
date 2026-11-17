package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// draft is a plain item of kind k to the owner, with no action.
func draft(k bus.Kind, handle string, u bus.Urgency) bus.Draft {
	return bus.Draft{Kind: k, Severity: bus.SeverityNormal, To: "human", Handle: handle, Title: "Title " + handle,
		Summary: "Summary " + handle, Action: bus.ActionNone, Urgency: u}
}

// toCrew is a Duet post addressed to the crew swarm.
func toCrew(handle string) bus.Draft {
	d := draft(bus.KindQuestion, handle, bus.UrgencyDigest)
	d.To = "crew"
	return d
}

func (e *env) inbox(t *testing.T, since int64) bus.Inbox {
	t.Helper()
	in, err := e.b.Inbox(context.Background(), duet, since)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	return in
}

func ids(ss []bus.Summary) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}

func (e *env) answers(t *testing.T, p bus.Principal, since int64, wait time.Duration) bus.Updates {
	t.Helper()
	u, err := e.b.Answers(context.Background(), p, since, wait)
	if err != nil {
		t.Fatalf("Answers: %v", err)
	}
	return u
}

// putAnswer stores an agent answer for the item's current revision and appends the event Task 6
// appends, the way the write path does.
func (e *env) putAnswer(t *testing.T, it bus.Item, text string) {
	t.Helper()
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		now := e.now()
		if err := x.InsertDecision(bus.Record{ItemID: it.ID, Revision: it.Revision, Kind: bus.RecordAnswer,
			ActorKind: bus.ActorAgent, Text: text, Source: "duet", By: "owner", HumanTS: now, Recorded: now}); err != nil {
			return err
		}
		seq, err := x.AppendEvent(bus.Event{Swarm: it.Swarm, ItemID: it.ID, Type: bus.EventAnswerRecorded, Created: now})
		if err != nil {
			return err
		}
		it.LastSeq = seq
		return x.UpdateItem(it)
	})
	if err != nil {
		t.Fatal(err)
	}
}
