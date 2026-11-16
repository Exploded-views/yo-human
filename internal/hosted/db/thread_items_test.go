package db_test

import (
	"testing"

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

func TestListThreadItemsSkipsDecidedItems(t *testing.T) {
	d, _ := openTemp(t)
	it := question("itm_a", "crew", "h")
	it.FromKind, it.To, it.LastSeq, it.AckedSeq = bus.PrincipalDuet, "crew", 5, 0
	insertItem(t, d, it)
	tx(t, d, func(x bus.Tx) error {
		return x.InsertDecision(func() bus.Record { r := record(bus.RecordAnswer, bus.ActorAgent, 1); r.ItemID = "itm_a"; return r }())
	})
	var got []bus.Item
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.ListThreadItems(0); return })
	if len(got) != 0 {
		t.Errorf("a decided item is listed: %+v", got)
	}
}
