package db_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func TestItemRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	want := item("itm_1", "crew", "first")
	insertItem(t, d, want)
	var byID, byHandle bus.Item
	var ok1, ok2 bool
	tx(t, d, func(x bus.Tx) (err error) {
		if byID, ok1, err = x.ItemByID("itm_1"); err != nil {
			return err
		}
		byHandle, ok2, err = x.ItemByHandle("crew", "first")
		return err
	})
	if !ok1 || !ok2 {
		t.Fatalf("found by id %v, by handle %v", ok1, ok2)
	}
	if !reflect.DeepEqual(byID, want) {
		t.Fatalf("ItemByID:\n got %+v\nwant %+v", byID, want)
	}
	if !reflect.DeepEqual(byHandle, want) {
		t.Fatalf("ItemByHandle:\n got %+v\nwant %+v", byHandle, want)
	}
}

func TestItemEmptyRiskFlagsRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	it := item("itm_1", "crew", "first")
	it.RiskFlags, it.Untrusted = nil, false
	insertItem(t, d, it)
	var got bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, _, err = x.ItemByID("itm_1"); return })
	if got.Untrusted || len(got.RiskFlags) != 0 {
		t.Fatalf("RiskFlags = %v, want none", got.RiskFlags)
	}
}

func TestItemMissingIsNotAnError(t *testing.T) {
	d, _ := openTemp(t)
	tx(t, d, func(x bus.Tx) error {
		if _, ok, err := x.ItemByID("nope"); ok || err != nil {
			t.Errorf("ItemByID: %v, %v", ok, err)
		}
		if _, ok, err := x.ItemByHandle("crew", "nope"); ok || err != nil {
			t.Errorf("ItemByHandle: %v, %v", ok, err)
		}
		return nil
	})
}

func TestItemHandleUniquePerSwarm(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "same"))
	insertItem(t, d, item("itm_2", "client", "same"))
	err := d.Tx(ctxb(), func(x bus.Tx) error { return x.InsertItem(item("itm_3", "crew", "same")) })
	if err == nil || !db.IsConflict(err) {
		t.Fatalf("duplicate (swarm, handle) gave %v, want a conflict", err)
	}
}

func TestUpdateItem(t *testing.T) {
	d, _ := openTemp(t)
	it := item("itm_1", "crew", "first")
	insertItem(t, d, it)
	it.Revision, it.Title, it.RiskFlags, it.LastSeq = 2, "Retitled", []string{"x"}, 9
	it.Updated = t0.Add(time.Hour)
	tx(t, d, func(x bus.Tx) error { return x.UpdateItem(it) })
	var got bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, _, err = x.ItemByID("itm_1"); return })
	if !reflect.DeepEqual(got, it) {
		t.Fatalf("got %+v want %+v", got, it)
	}
	err := d.Tx(ctxb(), func(x bus.Tx) error { return x.UpdateItem(item("itm_zz", "crew", "z")) })
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateItem of a missing item gave %v, want ErrNotFound", err)
	}
}

func TestListOpenForHuman(t *testing.T) {
	d, _ := openTemp(t)
	a := item("itm_a", "crew", "a")
	b := item("itm_b", "client", "b")
	b.Created = t0.Add(time.Second)
	toCrew := item("itm_d", "crew", "d")
	toCrew.To = "crew"
	for _, it := range []bus.Item{b, a, question("itm_c", "crew", "c"), toCrew} {
		insertItem(t, d, it)
	}
	if err := insertRecord(d, bus.Record{ItemID: "itm_c", Revision: 1, Kind: bus.RecordAnswer, ActorKind: bus.ActorAgent, Recorded: t0}); err != nil {
		t.Fatal(err)
	}
	var got []bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.ListOpenForHuman(); return })
	if len(got) != 2 || got[0].ID != "itm_a" || got[1].ID != "itm_b" {
		t.Fatalf("got %d items, want itm_a then itm_b: %+v", len(got), got)
	}
}

func TestStateOnlyFromDecisions(t *testing.T) {
	d, _ := openTemp(t)
	it := item("itm_1", "crew", "first")
	it.State = bus.StateDecided
	insertItem(t, d, it)
	if itemState(t, d, "itm_1") != bus.StateOpen {
		t.Fatal("InsertItem stored the state it was given")
	}
	tx(t, d, func(x bus.Tx) error { return x.UpdateItem(it) })
	if itemState(t, d, "itm_1") != bus.StateOpen {
		t.Fatal("UpdateItem took the state it was given")
	}
}

func TestUpdateItemDerivesStateFromRevision(t *testing.T) {
	d, _ := openTemp(t)
	q := question("itm_1", "crew", "first")
	insertItem(t, d, q)
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 1)); err != nil {
		t.Fatal(err)
	}
	tx(t, d, func(x bus.Tx) error { return x.UpdateItem(q) })
	if itemState(t, d, "itm_1") != bus.StateDecided {
		t.Fatal("an update at the decided revision reopened the item")
	}
	q.Revision = 2
	tx(t, d, func(x bus.Tx) error { return x.UpdateItem(q) })
	if itemState(t, d, "itm_1") != bus.StateOpen {
		t.Fatal("a new revision without a decision stayed decided")
	}
}

func TestEmptySwarmRefused(t *testing.T) {
	d, _ := openTemp(t)
	err := d.Tx(ctxb(), func(x bus.Tx) error { return x.InsertItem(item("itm_1", "", "first")) })
	if err == nil {
		t.Fatal("an item with an empty swarm was stored")
	}
	err = d.Tx(ctxb(), func(x bus.Tx) error { return x.PutStatus(bus.StatusDoc{Doc: []byte(`{}`), Updated: t0}) })
	if err == nil {
		t.Fatal("a status with an empty swarm was stored")
	}
}

func TestUpdateItemKindChangeRefused(t *testing.T) {
	d, _ := openTemp(t)
	q := question("itm_1", "crew", "first")
	insertItem(t, d, q)
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 1)); err != nil {
		t.Fatal(err)
	}
	var before bus.Item
	tx(t, d, func(x bus.Tx) (err error) { before, _, err = x.ItemByID("itm_1"); return })
	q.Kind = bus.KindApproval
	err := d.Tx(ctxb(), func(x bus.Tx) error { return x.UpdateItem(q) })
	if !errors.Is(err, db.ErrKindMismatch) {
		t.Fatalf("a kind change at the same revision gave %v, want ErrKindMismatch", err)
	}
	var after bus.Item
	tx(t, d, func(x bus.Tx) (err error) { after, _, err = x.ItemByID("itm_1"); return })
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("the refused update changed the row:\n%+v\n%+v", before, after)
	}
}

func TestUpdateItemRevisionBackwardsRefused(t *testing.T) {
	d, _ := openTemp(t)
	q := question("itm_1", "crew", "first")
	insertItem(t, d, q)
	if err := insertRecord(d, record(bus.RecordAnswer, bus.ActorAgent, 1)); err != nil {
		t.Fatal(err)
	}
	q.Revision = 2
	tx(t, d, func(x bus.Tx) error { return x.UpdateItem(q) })
	var before bus.Item
	tx(t, d, func(x bus.Tx) (err error) { before, _, err = x.ItemByID("itm_1"); return })
	q.Revision = 1
	err := d.Tx(ctxb(), func(x bus.Tx) error { return x.UpdateItem(q) })
	if !errors.Is(err, db.ErrStaleRevision) {
		t.Fatalf("revision 2 to 1 gave %v, want ErrStaleRevision", err)
	}
	var after bus.Item
	tx(t, d, func(x bus.Tx) (err error) { after, _, err = x.ItemByID("itm_1"); return })
	if !reflect.DeepEqual(before, after) || after.State != bus.StateOpen {
		t.Fatalf("the refused update changed the row:\n%+v\n%+v", before, after)
	}
}
