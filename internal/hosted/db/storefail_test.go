package db_test

import (
	"context"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// busCalls is the bus tables' store methods, each as a call that returns only its error.
func busCalls(x bus.Tx) map[string]func() error {
	return map[string]func() error{
		"ItemByID":         func() error { _, _, err := x.ItemByID("i"); return err },
		"ItemByHandle":     func() error { _, _, err := x.ItemByHandle("s", "h"); return err },
		"InsertItem":       func() error { return x.InsertItem(item("i", "s", "h")) },
		"UpdateItem":       func() error { return x.UpdateItem(item("i", "s", "h")) },
		"ListOpenForHuman": func() error { _, err := x.ListOpenForHuman(); return err },
		"ListThreadItems":  func() error { _, err := x.ListThreadItems(0); return err },
		"ListInboxItems":   func() error { _, err := x.ListInboxItems(0); return err },
		"Decision":         func() error { _, _, err := x.Decision("i", 1); return err },
		"DecisionRecorded": func() error { _, _, err := x.DecisionRecorded("i", bus.RecordKind(""), t0); return err },
		"InsertDecision":   func() error { return x.InsertDecision(bus.Record{ItemID: "i", Revision: 1, Recorded: t0}) },
		"InsertRelay":      func() error { return x.InsertRelay(bus.Relay{ItemID: "i", Recorded: t0}) },
		"Relays":           func() error { _, err := x.Relays("i"); return err },
		"AppendEvent":      func() error { _, err := x.AppendEvent(bus.Event{Swarm: "s", ItemID: "i", Created: t0}); return err },
		"EventsSince":      func() error { _, err := x.EventsSince("s", 0, 10); return err },
		"InsertMessage":    func() error { return x.InsertMessage(bus.Message{ThreadID: "i", Created: t0}) },
		"Messages":         func() error { _, err := x.Messages("i"); return err },
		"PutStatus":        func() error { return x.PutStatus(bus.StatusDoc{Swarm: "s", Updated: t0}) },
		"Statuses":         func() error { _, err := x.Statuses("s"); return err },
	}
}

func TestBusStoreReportsQueryFailures(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path, "DROP TABLE relays", "DROP TABLE messages", "DROP TABLE events", "DROP TABLE statuses", "DROP TABLE decisions", "DROP TABLE items")
	_ = d.Tx(context.Background(), func(x bus.Tx) error {
		for name, call := range busCalls(x) {
			if err := call(); err == nil {
				t.Errorf("%s on a missing table: no error", name)
			}
		}
		return nil
	})
}

func TestDeliveryIsNilWhileDeliveryIsHealthy(t *testing.T) {
	d, _ := openTemp(t)
	ds, err := d.Delivery(context.Background(), t0)
	if err != nil || ds != nil {
		t.Errorf("Delivery = %v, %v, want nil, nil", ds, err)
	}
}

func TestDeliveryReportsQueryFailures(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path, "DROP TABLE nudge_state")
	if _, err := d.Delivery(context.Background(), t0); err == nil {
		t.Error("Delivery without the nudge table: no error")
	}
	rawExec(t, path, "DROP TABLE outbox")
	if _, err := d.Delivery(context.Background(), t0); err == nil {
		t.Error("Delivery without the outbox table: no error")
	}
}

func TestDeliveryReportsUnreadableStatuses(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path, "UPDATE nudge_state SET stuck_since = '0001', stuck_status = 'x'")
	if _, err := d.Delivery(context.Background(), t0); err == nil {
		t.Error("Delivery read a stuck nudge whose status is unreadable")
	}
	rawExec(t, path, "UPDATE nudge_state SET stuck_since = '', stuck_status = 0, failed_at = '9999', last_status = 'x'")
	if _, err := d.Delivery(context.Background(), t0); err == nil {
		t.Error("Delivery read a refused nudge whose status is unreadable")
	}
}
