package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func TestNudgeStateStartsZeroAndRoundTrips(t *testing.T) {
	d, _ := openTemp(t)
	var got bus.NudgeState
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.NudgeState(); return })
	if got != (bus.NudgeState{}) {
		t.Errorf("fresh state = %+v, want zero", got)
	}
	want := bus.NudgeState{LastSeq: 42, Count: 7, LastSent: t0, FailedAt: t0.Add(time.Minute), LastStatus: 400, StuckSince: t0.Add(2 * time.Minute), StuckStatus: 503}
	tx(t, d, func(x bus.Tx) error { return x.PutNudgeState(want) })
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.NudgeState(); return })
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
	tx(t, d, func(x bus.Tx) error { return x.PutNudgeState(bus.NudgeState{LastSeq: 1}) })
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.NudgeState(); return })
	if got != (bus.NudgeState{LastSeq: 1}) {
		t.Errorf("state after a second put = %+v, want the second alone", got)
	}
}

func TestDeferredPutReplacesAndDeleteRemoves(t *testing.T) {
	d, _ := openTemp(t)
	list := func() (out []bus.Deferred) {
		tx(t, d, func(x bus.Tx) (err error) { out, err = x.Deferred(); return })
		return out
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("fresh deferred = %+v", got)
	}
	tx(t, d, func(x bus.Tx) error {
		for _, df := range []bus.Deferred{{ItemID: "b", DueAt: t0.Add(2 * time.Hour)}, {ItemID: "a", DueAt: t0.Add(time.Hour)}, {ItemID: "b", DueAt: t0.Add(3 * time.Hour)}} {
			if err := x.PutDeferred(df); err != nil {
				return err
			}
		}
		return nil
	})
	got := list()
	if len(got) != 2 || got[0].ItemID != "a" || got[1].ItemID != "b" || !got[1].DueAt.Equal(t0.Add(3*time.Hour)) {
		t.Errorf("deferred = %+v, want a then b, with b's second due time", got)
	}
	tx(t, d, func(x bus.Tx) error { return x.DeleteDeferred("a") })
	tx(t, d, func(x bus.Tx) error { return x.DeleteDeferred("nobody") }) // deleting nothing is no error
	if got := list(); len(got) != 1 || got[0].ItemID != "b" {
		t.Errorf("deferred after delete = %+v, want b", got)
	}
}

func TestBrokenFlagRoundTrip(t *testing.T) {
	d, path := openTemp(t)
	var got bus.BrokenFlag
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.BrokenFlag(); return })
	if got != (bus.BrokenFlag{}) {
		t.Fatalf("fresh flag = %+v", got)
	}
	want := bus.BrokenFlag{Raised: true, Since: t0, Status: 403}
	tx(t, d, func(x bus.Tx) error { return x.PutBrokenFlag(want) })
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d2, err := db.Open(path) // the flag survives a restart
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d2.Close() }()
	tx(t, d2, func(x bus.Tx) (err error) { got, err = x.BrokenFlag(); return })
	if got != want {
		t.Errorf("flag after reopen = %+v, want %+v", got, want)
	}
	tx(t, d2, func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{}) })
	tx(t, d2, func(x bus.Tx) (err error) { got, err = x.BrokenFlag(); return })
	if got != (bus.BrokenFlag{}) {
		t.Errorf("flag after clearing = %+v", got)
	}
}

func deliveryAt(t *testing.T, d *db.DB, now time.Time) (ds *bus.DeliveryState) {
	t.Helper()
	tx(t, d, func(x bus.Tx) (err error) { ds, err = x.DeliveryState(now); return })
	return ds
}

func TestDeliveryStateIsNilWhenHealthy(t *testing.T) {
	d, _ := openTemp(t)
	now := t0.Add(48 * time.Hour)
	if ds := deliveryAt(t, d, now); ds != nil {
		t.Errorf("empty database: %+v, want nil", ds)
	}
	r := outboxRow(bus.OutboxDuetPost, "itm", t0)
	r.State, r.SentAt = bus.OutboxSent, t0
	addRow(t, d, r)
	held := outboxRow(bus.OutboxDuetPost, "itm", t0)
	held.NextAt = now.Add(50 * time.Minute) // held by the cap, not overdue
	addRow(t, d, held)
	if ds := deliveryAt(t, d, now); ds != nil {
		t.Errorf("a sent row and a held one: %+v, want nil", ds)
	}
}

func TestDeliveryStateBrokenCarriesSinceAndStatus(t *testing.T) {
	d, _ := openTemp(t)
	tx(t, d, func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: 401}) })
	ds := deliveryAt(t, d, t0.Add(time.Minute))
	if ds == nil || !ds.Broken || !ds.Since.Equal(t0) || ds.Status != 401 || ds.Failed != 0 || ds.Pending != 0 {
		t.Errorf("state = %+v, want broken since t0 with status 401", ds)
	}
}

func TestDeliveryStateFailedAgesOutAfter24Hours(t *testing.T) {
	d, _ := openTemp(t)
	r := outboxRow(bus.OutboxDuetPost, "itm", t0)
	r.State, r.FailedAt, r.LastStatus = bus.OutboxFailed, t0, 400
	addRow(t, d, r)
	if ds := deliveryAt(t, d, t0.Add(24*time.Hour-time.Nanosecond)); ds == nil || ds.Failed != 1 || ds.Status != 400 || ds.Broken {
		t.Errorf("just under 24 h: %+v, want one failed with status 400", ds)
	}
	if ds := deliveryAt(t, d, t0.Add(24*time.Hour)); ds != nil {
		t.Errorf("at 24 h: %+v, want nil", ds)
	}
}

func TestDeliveryStateCountsARefusedNudge(t *testing.T) {
	d, _ := openTemp(t)
	tx(t, d, func(x bus.Tx) error {
		return x.PutNudgeState(bus.NudgeState{LastSent: t0, FailedAt: t0, LastStatus: 413})
	})
	ds := deliveryAt(t, d, t0.Add(time.Hour))
	if ds == nil || ds.Failed != 1 || ds.Status != 413 {
		t.Errorf("state = %+v, want the refused nudge counted with its status", ds)
	}
	if ds := deliveryAt(t, d, t0.Add(24*time.Hour)); ds != nil {
		t.Errorf("a refused nudge after 24 h: %+v, want nil", ds)
	}
}

func TestDeliveryStatePendingIsOverdueAfterTenMinutes(t *testing.T) {
	d, _ := openTemp(t)
	r := outboxRow(bus.OutboxDuetPost, "itm", t0)
	addRow(t, d, r) // due at t0
	if ds := deliveryAt(t, d, t0.Add(10*time.Minute)); ds != nil {
		t.Errorf("due 10 min ago: %+v, want nil: not yet overdue", ds)
	}
	ds := deliveryAt(t, d, t0.Add(10*time.Minute+time.Nanosecond))
	if ds == nil || ds.Pending != 1 || ds.Broken || ds.Failed != 0 {
		t.Errorf("overdue: %+v, want one pending", ds)
	}
	addRow(t, d, outboxRow(bus.OutboxPage, "itm_2", t0.Add(time.Hour))) // not overdue, but still pending
	if ds := deliveryAt(t, d, t0.Add(11*time.Minute)); ds == nil || ds.Pending != 2 {
		t.Errorf("pending counts every pending row: %+v, want 2", ds)
	}
}

func TestOutboxRollsBackWithItsTransaction(t *testing.T) {
	d, _ := openTemp(t)
	_ = d.Tx(context.Background(), func(x bus.Tx) error {
		if _, err := x.InsertOutbox(outboxRow(bus.OutboxTest, "", t0)); err != nil {
			return err
		}
		return context.Canceled
	})
	if n := len(allRows(t, d)); n != 0 {
		t.Errorf("%d rows after a rolled-back transaction", n)
	}
}

func TestDeliveryStateCountsARowStillRetryingAfterTenMinutes(t *testing.T) {
	d, _ := openTemp(t)
	r := outboxRow(bus.OutboxDuetPost, "itm", t0)
	r.Attempts, r.NextAt = 3, t0.Add(time.Hour) // backing off: next_at is always ahead while the worker runs
	addRow(t, d, r)
	if ds := deliveryAt(t, d, t0.Add(10*time.Minute)); ds != nil {
		t.Errorf("a retrying row 10 min old: %+v, want nil", ds)
	}
	ds := deliveryAt(t, d, t0.Add(10*time.Minute+time.Nanosecond))
	if ds == nil || ds.Pending != 1 || ds.Broken {
		t.Errorf("a retrying row older than 10 min: %+v, want one pending", ds)
	}
	held := outboxRow(bus.OutboxDuetPost, "itm_2", t0)
	held.NextAt = t0.Add(time.Hour) // held by the cap: never tried, so not stuck
	addRow(t, d, held)
	if ds := deliveryAt(t, d, t0.Add(20*time.Minute)); ds == nil || ds.Pending != 2 {
		t.Errorf("pending = %+v, want both rows counted but only the tried one making it unhealthy", ds)
	}
}

func TestDeliveryStateCountsANudgeThatKeepsFailing(t *testing.T) {
	d, _ := openTemp(t)
	tx(t, d, func(x bus.Tx) error { return x.PutNudgeState(bus.NudgeState{StuckSince: t0, StuckStatus: 503}) })
	if ds := deliveryAt(t, d, t0.Add(10*time.Minute)); ds != nil {
		t.Errorf("a nudge failing for 10 min: %+v, want nil", ds)
	}
	ds := deliveryAt(t, d, t0.Add(10*time.Minute+time.Nanosecond))
	if ds == nil || ds.Status != 503 || ds.Pending != 1 {
		t.Errorf("a nudge failing for over 10 min: %+v, want status 503", ds)
	}
}
