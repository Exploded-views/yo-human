package db_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func ctxb() context.Context { return context.Background() }

func record(kind bus.RecordKind, actor bus.ActorKind, rev int) bus.Record {
	r := bus.Record{
		ItemID: "itm_1", Revision: rev, Kind: kind, ActorKind: actor,
		Text: "yes", Source: "duet", By: "owner", SourceMessage: "msg-1",
		HumanTS: t0, Recorded: t0.Add(time.Second),
	}
	if kind == bus.RecordApproval {
		r.Decision = bus.DecisionApprove
	}
	return r
}

func insertRecord(d *db.DB, r bus.Record) error {
	return d.Tx(ctxb(), func(x bus.Tx) error { return x.InsertDecision(r) })
}

func TestDecisionUniquePerRevision(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 1)); err != nil {
		t.Fatal(err)
	}
	err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 1))
	if err == nil || !db.IsConflict(err) {
		t.Fatalf("second agent record for the same revision gave %v, want a conflict", err)
	}
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorHuman, 1)); err != nil {
		t.Fatalf("the human record for the same revision: %v", err)
	}
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 2)); err != nil {
		t.Fatalf("an agent record for another revision: %v", err)
	}
}

func TestDecisionRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	want := record(bus.RecordApproval, bus.ActorHuman, 1)
	if err := insertRecord(d, want); err != nil {
		t.Fatal(err)
	}
	var got bus.Record
	var ok, ok2 bool
	tx(t, d, func(x bus.Tx) (err error) {
		got, ok, err = x.Decision("itm_1", 1)
		if err == nil {
			_, ok2, err = x.Decision("itm_1", 2)
		}
		return err
	})
	if !ok || ok2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("ok=%v ok2=%v got %+v want %+v", ok, ok2, got, want)
	}
}

func TestDecisionPrefersHuman(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	agent, human := record(bus.RecordAnswer, bus.ActorAgent, 1), record(bus.RecordAnswer, bus.ActorHuman, 1)
	agent.Text, human.Text = "from the agent", "from the human"
	for _, r := range []bus.Record{human, agent} {
		if err := insertRecord(d, r); err != nil {
			t.Fatal(err)
		}
	}
	var got bus.Record
	tx(t, d, func(x bus.Tx) (err error) { got, _, err = x.Decision("itm_1", 1); return })
	if got.ActorKind != bus.ActorHuman {
		t.Fatalf("effective decision is %q's, want the human's", got.ActorKind)
	}
}

func TestDatabaseRefusesAgentApproval(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	err := insertRecord(d, record(bus.RecordApproval, bus.ActorAgent, 1))
	if err == nil {
		t.Fatal("an agent approval was stored")
	}
	if db.IsConflict(err) {
		t.Fatalf("the CHECK refusal reads as a conflict: %v", err)
	}
}

func TestInsertDecisionMarksOnlyThatRevisionDecided(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	state := func() bus.State {
		var it bus.Item
		tx(t, d, func(x bus.Tx) (err error) { it, _, err = x.ItemByID("itm_1"); return })
		return it.State
	}
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 2)); err != nil {
		t.Fatal(err)
	}
	if state() != bus.StateOpen {
		t.Fatal("a record for another revision decided the item")
	}
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 1)); err != nil {
		t.Fatal(err)
	}
	if state() != bus.StateDecided {
		t.Fatal("a record for the current revision did not decide the item")
	}
}

func TestRelayDoesNotDecide(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	want := bus.Relay{ID: "rel_1", ItemID: "itm_1", Revision: 1, Text: "go ahead", Source: "duet", By: "owner", HumanTS: t0, Recorded: t0}
	var got []bus.Relay
	var it bus.Item
	tx(t, d, func(x bus.Tx) error {
		if err := x.InsertRelay(want); err != nil {
			return err
		}
		var err error
		if got, err = x.Relays("itm_1"); err != nil {
			return err
		}
		it, _, err = x.ItemByID("itm_1")
		return err
	})
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("relays = %+v, want %+v", got, want)
	}
	if it.State != bus.StateOpen {
		t.Fatal("a relay decided the item")
	}
}
