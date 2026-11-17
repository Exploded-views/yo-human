package bus_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// approve stores the owner's approval of the item's current revision and appends its event the way
// the write path does: in one transaction, with the event's Created equal to the record's Recorded.
func (e *env) approve(t *testing.T, id string) {
	t.Helper()
	it, _ := e.stored(t, id)
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		now := e.now()
		if err := x.InsertDecision(bus.Record{ItemID: id, Revision: it.Revision, Kind: bus.RecordApproval, Decision: bus.DecisionApprove,
			ActorKind: bus.ActorHuman, Text: fmt.Sprintf("ok for revision %d", it.Revision), Source: "passkey", By: "owner", HumanTS: now, Recorded: now}); err != nil {
			return err
		}
		seq, err := x.AppendEvent(bus.Event{Swarm: it.Swarm, ItemID: id, Type: bus.EventApprovalRecorded, Created: now})
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

// revise reposts the approval with a new title, which makes the next revision.
func (e *env) revise(t *testing.T, n int) {
	t.Helper()
	d := mergeApproval("m7")
	d.Title = fmt.Sprintf("Merge PR 7, take %d", n)
	if r := e.post(t, cinode, d); r.Outcome != bus.OutcomeRevised {
		t.Fatalf("outcome = %s", r.Outcome)
	}
}

func approvalUpdates(u bus.Updates) []bus.Update {
	var out []bus.Update
	for _, ev := range u.Events {
		if ev.Type == bus.EventApprovalRecorded {
			out = append(out, ev)
		}
	}
	return out
}

func TestAnswersRecordIsTheOneTheEventRecorded(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, cinode, mergeApproval("m7")).Item
	e.approve(t, it.ID)
	e.revise(t, 2)
	e.approve(t, it.ID)
	got := approvalUpdates(e.answers(t, cinode, 0, 0))
	if len(got) != 2 || got[0].Record == nil || got[1].Record == nil || got[0].Record.Revision != 1 || got[1].Record.Revision != 2 {
		t.Fatalf("approval updates = %+v, want records for revisions 1 then 2", got)
	}
	if got[0].Record.Text == got[1].Record.Text {
		t.Error("both events carry the same record")
	}
}

func TestAnswersRecordUpdateNamesTheRecordsRevision(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, cinode, mergeApproval("m7")).Item
	e.approve(t, it.ID)
	e.revise(t, 2) // the item is at revision 2 now; the approval was for revision 1
	got := approvalUpdates(e.answers(t, cinode, 0, 0))
	if len(got) != 1 || got[0].Record == nil || got[0].Revision != 1 || got[0].Record.Revision != 1 {
		t.Fatalf("approval update = %+v, want Revision 1 and a record for revision 1", got)
	}
}

func TestAnswersRecordBeyondSixteenRevisions(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, cinode, mergeApproval("m7")).Item
	e.revise(t, 2)
	e.revise(t, 3)
	e.approve(t, it.ID) // revision 3
	for n := 4; n <= 20; n++ {
		e.revise(t, n)
	}
	got := approvalUpdates(e.answers(t, cinode, 0, 0))
	if len(got) != 1 || got[0].Record == nil || got[0].Record.Revision != 3 || got[0].Revision != 3 {
		t.Fatalf("approval update = %+v, want the record for revision 3", got)
	}
}

// A page that reads more than the cap of events stops there: Next moves to the last event read,
// the call makes a bounded number of reads, and it does not wait for events it hasn't scanned.
func TestAnswersScanIsBounded(t *testing.T) {
	e := newEnv(t)
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		for range 5000 {
			if _, err := x.AppendEvent(bus.Event{Swarm: "crew", Type: bus.EventStatusPushed, Created: t0}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	b := bus.New(countingStore{e.d, &reads}, e.now, e.l)
	newFakeTimer(t, b)
	r := within(t, poll(b, context.Background(), cinode, 0, 20*time.Second), 5*time.Second, "capped call")
	if r.err != nil || len(r.u.Events) != 0 || r.u.Next != bus.MaxScan {
		t.Errorf("result = %+v, want no events and Next == %d", r, bus.MaxScan)
	}
	if got := reads.Load(); got > bus.MaxScan/bus.MaxUpdates {
		t.Errorf("%d event reads, want at most %d", got, bus.MaxScan/bus.MaxUpdates)
	}
}

// Messages in one thread have strictly increasing times even under a frozen clock, so an event
// finds its own message and not an earlier one made at the same instant.
func TestAnswersMessageMatchSurvivesEqualTimestamps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	b := bus.New(e.d, func() time.Time { return t0 }, e.l)
	it, err := b.Post(ctx, cinode, draft(bus.KindQuestion, "q", bus.UrgencyDigest))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		who  bus.Principal
		body string
	}{{cinode, "from the node"}, {duet, "from duet"}} {
		if _, err := b.Reply(ctx, p.who, it.Item.ID, p.body, bus.SeverityNormal, bus.UrgencyDigest); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.Answers(ctx, cinode, 0, 0)
	if err != nil || len(u.Events) != 1 || u.Events[0].Message == nil || u.Events[0].Message.Body != "from duet" {
		t.Fatalf("answers = %+v, %v, want duet's message and only it", u, err)
	}
}
