package bus_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// readSetup makes what each read has something to read: a node's question with a Duet reply and
// an answer, a Duet item with a node's reply, and a status. It returns the question's id and the
// cursor just before its events.
func readSetup(t *testing.T) (e *env, questionID string) {
	t.Helper()
	e = newEnv(t)
	ctx := context.Background()
	q := e.post(t, node("ci-2", "crew"), draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	if _, err := e.b.Reply(ctx, duet, q.ID, "from duet", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.stored(t, q.ID)
	e.putAnswer(t, cur, "yes")
	d := e.post(t, duet, toCrew("d")).Item
	if _, err := e.b.Reply(ctx, node("ci-2", "crew"), d.ID, "to duet", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	if err := e.b.PushStatus(ctx, node("ci-2", "crew"), validStatus("crew")); err != nil {
		t.Fatal(err)
	}
	return e, q.ID
}

// TestReadStoreFailureIsInternal: every store call a read makes can fail; the caller gets code
// internal with no detail of the cause.
func TestReadStoreFailureIsInternal(t *testing.T) {
	ctx := context.Background()
	answers := func(since int64) func(*bus.Bus, string) error {
		return func(b *bus.Bus, _ string) error { _, err := b.Answers(ctx, cinode, since, 0); return err }
	}
	for _, c := range []struct {
		name, method string
		nth          int
		op           func(*bus.Bus, string) error
	}{
		{"inbox: inbox items", "ListInboxItems", 1, func(b *bus.Bus, _ string) error { _, err := b.Inbox(ctx, duet, 0); return err }},
		{"inbox: thread items", "ListThreadItems", 1, func(b *bus.Bus, _ string) error { _, err := b.Inbox(ctx, duet, 0); return err }},
		{"inbox: thread messages", "Messages", 1, func(b *bus.Bus, _ string) error { _, err := b.Inbox(ctx, duet, 0); return err }},
		{"item: read", "ItemByID", 1, func(b *bus.Bus, id string) error { _, err := b.Item(ctx, duet, id); return err }},
		{"item: messages", "Messages", 1, func(b *bus.Bus, id string) error { _, err := b.Item(ctx, duet, id); return err }},
		{"item: decision", "Decision", 1, func(b *bus.Bus, id string) error { _, err := b.Item(ctx, duet, id); return err }},
		{"ack: read", "ItemByID", 1, func(b *bus.Bus, id string) error { return b.Ack(ctx, duet, id, 1<<40) }},
		{"ack: update", "UpdateItem", 1, func(b *bus.Bus, id string) error { return b.Ack(ctx, duet, id, 1<<40) }},
		{"answers: events", "EventsSince", 1, answers(0)},
		{"answers: item of a post", "ItemByID", 1, answers(0)},
		{"answers: item of a message", "ItemByID", 1, answers(1)},
		{"answers: messages", "Messages", 1, answers(1)},
		{"answers: item of a record", "ItemByID", 1, answers(2)},
		{"answers: decision", "DecisionRecorded", 1, answers(2)},
		{"status push: store", "PutStatus", 1, func(b *bus.Bus, _ string) error { return b.PushStatus(ctx, cinode, validStatus("crew")) }},
		{"status push: event", "AppendEvent", 1, func(b *bus.Bus, _ string) error { return b.PushStatus(ctx, cinode, validStatus("crew")) }},
		{"status read", "Statuses", 1, func(b *bus.Bus, _ string) error { _, err := b.SwarmStatus(ctx, duet, ""); return err }},
	} {
		e, id := readSetup(t)
		f := &faulty{Store: e.d, method: c.method, nth: c.nth}
		err := c.op(bus.New(f, e.now, e.l), id)
		be := errWithCode(t, err, bus.CodeInternal)
		if strings.Contains(be.What+be.Valid+be.Fix+be.Error(), "secret") {
			t.Errorf("%s: the error leaks the cause: %v", c.name, be)
		}
	}
}

// The first events of the setup are the question's post (seq 1), the Duet reply (2) and the answer
// (3), which is why answers(1) and answers(2) start at a message and at a record.
func TestAnswersEventsOfEveryKindCarryTheirData(t *testing.T) {
	e, id := readSetup(t)
	got := updatesOf(e.answers(t, cinode, 0, 0), id)
	if m := got[bus.EventMessageAdded]; m.Message == nil || m.Message.From != "duet" || m.ItemID != id {
		t.Errorf("message update = %+v", m)
	}
	if r := got[bus.EventAnswerRecorded]; r.Record == nil || r.Record.Text != "yes" || r.Handle != "q" {
		t.Errorf("answer update = %+v", r)
	}
	if p := got[bus.EventItemPosted]; p.ItemID != id || p.Handle != "q" {
		t.Errorf("post update = %+v", p)
	}
}

// updatesOf maps the page's updates for one item by event type.
func updatesOf(u bus.Updates, itemID string) map[bus.EventType]bus.Update {
	got := map[bus.EventType]bus.Update{}
	for _, ev := range u.Events {
		if ev.ItemID == itemID {
			got[ev.Type] = ev
		}
	}
	return got
}

// An event whose item is gone (the store never deletes one, but an event is read on its own) is
// still returned, with only what the event itself says.
func TestAnswersEventForAMissingItem(t *testing.T) {
	e := newEnv(t)
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		for _, ty := range []bus.EventType{bus.EventItemPosted, bus.EventMessageAdded, bus.EventAnswerRecorded} {
			if _, err := x.AppendEvent(bus.Event{Swarm: "crew", ItemID: "itm_gone", Type: ty, Created: t0}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	u := e.answers(t, cinode, 0, 0)
	if len(u.Events) != 3 {
		t.Fatalf("%d updates, want 3", len(u.Events))
	}
	for _, ev := range u.Events {
		wantBare(t, ev)
	}
}

func wantBare(t *testing.T, ev bus.Update) {
	t.Helper()
	if ev.ItemID != "itm_gone" || ev.Handle != "" || ev.Revision != 0 || ev.Record != nil || ev.Message != nil {
		t.Errorf("update = %+v", ev)
	}
}

// A message event whose message cannot be found (none made at that instant) is returned without one.
func TestAnswersMessageEventWithNoMessage(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-2", "crew"), draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		_, err := x.AppendEvent(bus.Event{Swarm: "crew", ItemID: it.ID, Type: bus.EventMessageAdded, Created: t0})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	u := e.answers(t, cinode, 0, 0)
	if last := u.Events[len(u.Events)-1]; last.Type != bus.EventMessageAdded || last.Message != nil {
		t.Errorf("last update = %+v", last)
	}
}

// An approval event gets the approval's record, not an answer's, and none when the revision has no
// approval of its kind.
func TestAnswersRecordIsOfTheEventsKind(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-2", "crew"), mergeApproval("m7")).Item
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		if err := x.InsertDecision(bus.Record{ItemID: it.ID, Revision: 1, Kind: bus.RecordApproval, Decision: bus.DecisionApprove,
			ActorKind: bus.ActorHuman, Text: "ok", Source: "passkey", By: "owner", HumanTS: t0, Recorded: t0}); err != nil {
			return err
		}
		for _, ty := range []bus.EventType{bus.EventApprovalRecorded, bus.EventAnswerRecorded} {
			if _, err := x.AppendEvent(bus.Event{Swarm: "crew", ItemID: it.ID, Type: ty, Created: t0}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	u := e.answers(t, cinode, 0, 0)
	byType := map[bus.EventType]bus.Update{}
	for _, ev := range u.Events {
		byType[ev.Type] = ev
	}
	if r := byType[bus.EventApprovalRecorded].Record; r == nil || r.Kind != bus.RecordApproval {
		t.Errorf("approval update's record = %+v", r)
	}
	if r := byType[bus.EventAnswerRecorded].Record; r != nil {
		t.Errorf("an answer event got the approval's record: %+v", r)
	}
}

func TestAnswersAlreadyCancelledContext(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u, err := e.b.Answers(ctx, cinode, 7, time.Second)
	if !errors.Is(err, context.Canceled) || u.Next != 7 {
		t.Errorf("Answers = %+v, %v, want context.Canceled and Next == since", u, err)
	}
}

// cancelOnRead is a store whose first event read cancels the caller's context and fails.
type cancelOnRead struct {
	bus.Store
	cancel context.CancelFunc
}

func (c cancelOnRead) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return c.Store.Tx(ctx, func(x bus.Tx) error { return fn(cancelTx{x, c.cancel}) })
}

type cancelTx struct {
	bus.Tx
	cancel context.CancelFunc
}

func (c cancelTx) EventsSince(string, int64, int) ([]bus.Event, error) {
	c.cancel()
	return nil, errDisk
}

// A read that fails because the caller went away reports that, not an internal error.
func TestAnswersCancelledDuringTheRead(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	b := bus.New(cancelOnRead{e.d, cancel}, e.now, e.l)
	_, err := b.Answers(ctx, cinode, 0, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestStatusPushRejectsTooManyWaiting(t *testing.T) {
	e := newEnv(t)
	entries := strings.TrimSuffix(strings.Repeat(`"ob-1",`, 65), ",")
	doc := `{"swarm":"crew","sessions":[],"waiting_on_human":[` + entries + `],"main_health":"ok","spend":0,"ts":1}`
	err := e.b.PushStatus(context.Background(), cinode, json.RawMessage(doc))
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "waiting_on_human" {
		t.Errorf("What = %q", be.What)
	}
}
