package db_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestListThreadItems(t *testing.T) {
	d, _ := openTemp(t)
	mk := func(id string, kind bus.PrincipalKind, to string, last, acked int64) bus.Item {
		it := question(id, "crew", "h-"+id)
		it.FromKind, it.To, it.LastSeq, it.AckedSeq = kind, to, last, acked
		return it
	}
	for _, it := range []bus.Item{
		mk("itm_a", bus.PrincipalDuet, "crew", 5, 0),   // listed
		mk("itm_b", bus.PrincipalDuet, "crew", 9, 2),   // listed, after a
		mk("itm_c", bus.PrincipalDuet, "crew", 4, 4),   // acked up to its last activity
		mk("itm_d", bus.PrincipalNode, "crew", 8, 0),   // not Duet's
		mk("itm_e", bus.PrincipalDuet, "human", 8, 0),  // addressed to the owner, not a thread
		mk("itm_f", bus.PrincipalDuet, "client", 3, 0), // at or below since
	} {
		insertItem(t, d, it)
	}
	var got []bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.ListThreadItems(3); return })
	if len(got) != 2 || got[0].ID != "itm_a" || got[1].ID != "itm_b" {
		t.Fatalf("got %d items, want itm_a then itm_b: %+v", len(got), got)
	}
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.ListThreadItems(5); return })
	if len(got) != 1 || got[0].ID != "itm_b" {
		t.Errorf("since 5: got %+v, want only itm_b", got)
	}
}

func TestListThreadItemsIncludesDecidedItems(t *testing.T) {
	d, _ := openTemp(t)
	it := question("itm_a", "crew", "h")
	it.FromKind, it.To, it.LastSeq, it.AckedSeq = bus.PrincipalDuet, "crew", 5, 0
	insertItem(t, d, it)
	tx(t, d, func(x bus.Tx) error {
		r := record(bus.RecordAnswer, bus.ActorAgent, 1)
		r.ItemID = "itm_a"
		return x.InsertDecision(r)
	})
	var got []bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.ListThreadItems(0); return })
	if len(got) != 1 || got[0].State != bus.StateDecided {
		t.Errorf("a decided item's new activity is news: got %+v", got)
	}
}

func TestListInboxItems(t *testing.T) {
	d, _ := openTemp(t)
	mk := func(id, to string, kind bus.PrincipalKind, last, acked int64) bus.Item {
		it := question(id, "crew", "h-"+id)
		it.To, it.FromKind, it.LastSeq, it.AckedSeq = to, kind, last, acked
		it.Created = t0.Add(time.Duration(last) * time.Second)
		return it
	}
	for _, it := range []bus.Item{
		mk("itm_a", "human", bus.PrincipalNode, 5, 0),
		mk("itm_b", "human", bus.PrincipalNode, 9, 2),
		mk("itm_c", "human", bus.PrincipalNode, 4, 4), // acked
		mk("itm_d", "human", bus.PrincipalNode, 3, 0), // at since
		mk("itm_e", "crew", bus.PrincipalDuet, 8, 0),  // not addressed to the owner
		mk("itm_f", "human", bus.PrincipalNode, 7, 0), // decided below
	} {
		insertItem(t, d, it)
	}
	tx(t, d, func(x bus.Tx) error {
		r := record(bus.RecordAnswer, bus.ActorAgent, 1)
		r.ItemID = "itm_f"
		return x.InsertDecision(r)
	})
	var got []bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.ListInboxItems(3); return })
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	want := []string{"itm_a", "itm_f", "itm_b"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v (open and decided, oldest first)", ids, want)
	}
}
