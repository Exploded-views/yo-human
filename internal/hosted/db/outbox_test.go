package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func outboxRow(kind bus.OutboxKind, itemID string, created time.Time) bus.OutboxRow {
	return bus.OutboxRow{Kind: kind, Swarm: "duet", ItemID: itemID, Revision: 2, Level: "active", Title: "Title one", Body: "Body one",
		State: bus.OutboxPending, NextAt: created, Created: created}
}

func addRow(t *testing.T, d *db.DB, r bus.OutboxRow) int64 {
	t.Helper()
	var id int64
	tx(t, d, func(x bus.Tx) (err error) { id, err = x.InsertOutbox(r); return })
	return id
}

func allRows(t *testing.T, d *db.DB) []bus.OutboxRow {
	t.Helper()
	var rows []bus.OutboxRow
	tx(t, d, func(x bus.Tx) (err error) { rows, err = x.AllOutbox(); return })
	return rows
}

func pendingDue(t *testing.T, d *db.DB, now time.Time, limit int) []bus.OutboxRow {
	t.Helper()
	var rows []bus.OutboxRow
	tx(t, d, func(x bus.Tx) (err error) { rows, err = x.PendingOutbox(now, limit); return })
	return rows
}

func ids(rows []bus.OutboxRow) []int64 {
	out := []int64{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOutboxRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	r := outboxRow(bus.OutboxDuetPost, "itm_1", t0)
	r.Attempts, r.LastStatus, r.Retried = 3, 500, true
	r.SentAt, r.FailedAt = t0.Add(time.Minute), t0.Add(2*time.Minute)
	r.State = bus.OutboxFailed
	id := addRow(t, d, r)
	second := addRow(t, d, outboxRow(bus.OutboxPage, "itm_2", t0))
	if id < 1 || second <= id {
		t.Fatalf("ids = %d, %d, want increasing from 1", id, second)
	}
	r.ID = id
	rows := allRows(t, d)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	if rows[0] != r {
		t.Fatalf("rows = %+v, want first %+v", rows, r)
	}
	if rows[1].SentAt != (time.Time{}) || rows[1].FailedAt != (time.Time{}) || rows[1].Kind != bus.OutboxPage {
		t.Errorf("a row never sent has times %v %v", rows[1].SentAt, rows[1].FailedAt)
	}
}

func TestOutboxRefusesAnUnknownKindOrState(t *testing.T) {
	d, _ := openTemp(t)
	for _, mutate := range []func(*bus.OutboxRow){func(r *bus.OutboxRow) { r.Kind = "loud" }, func(r *bus.OutboxRow) { r.State = "done" }} {
		r := outboxRow(bus.OutboxTest, "", t0)
		mutate(&r)
		err := d.Tx(context.Background(), func(x bus.Tx) error { _, err := x.InsertOutbox(r); return err })
		if err == nil {
			t.Errorf("row %+v was stored", r)
		}
	}
	if n := len(allRows(t, d)); n != 0 {
		t.Errorf("%d rows after refused inserts", n)
	}
}

func TestPendingOutboxIsDueOldestFirstAndLimited(t *testing.T) {
	d, _ := openTemp(t)
	now := t0.Add(time.Hour)
	mk := func(created time.Time, next time.Time, state bus.OutboxState) int64 {
		r := outboxRow(bus.OutboxDuetPost, "itm", created)
		r.NextAt, r.State = next, state
		return addRow(t, d, r)
	}
	newer := mk(t0.Add(2*time.Minute), now, bus.OutboxPending)
	older := mk(t0, now.Add(-time.Minute), bus.OutboxPending)
	tie := mk(t0, now.Add(-time.Hour), bus.OutboxPending)
	mk(t0, now.Add(time.Nanosecond), bus.OutboxPending) // not due yet
	mk(t0, now, bus.OutboxSent)
	mk(t0, now, bus.OutboxFailed)
	mk(t0, now, bus.OutboxSuperseded)
	if got := ids(pendingDue(t, d, now, 10)); !equalIDs(got, []int64{older, tie, newer}) {
		t.Errorf("due rows = %v, want %v (oldest created first, then id)", got, []int64{older, tie, newer})
	}
	if got := ids(pendingDue(t, d, now, 2)); !equalIDs(got, []int64{older, tie}) {
		t.Errorf("limit 2 = %v, want the two oldest", got)
	}
}

func TestUpdateOutboxChangesOnlyTheMutableFields(t *testing.T) {
	d, _ := openTemp(t)
	id := addRow(t, d, outboxRow(bus.OutboxDuetPost, "itm_1", t0))
	later := t0.Add(time.Hour)
	tx(t, d, func(x bus.Tx) error {
		return x.UpdateOutbox(bus.OutboxRow{ID: id, Kind: bus.OutboxPage, Swarm: "x", ItemID: "other", Revision: 9, Level: "passive", Title: "T2", Body: "B2",
			State: bus.OutboxSent, Attempts: 4, NextAt: later, Created: later, SentAt: later, FailedAt: later, LastStatus: 200, Retried: true})
	})
	rows := allRows(t, d)
	if len(rows) != 1 {
		t.Fatalf("%d rows after the update, want 1", len(rows))
	}
	got := rows[0]
	want := bus.OutboxRow{ID: id, Kind: bus.OutboxDuetPost, Swarm: "duet", ItemID: "itm_1", Revision: 2, Level: "passive", Title: "Title one", Body: "Body one",
		State: bus.OutboxSent, Attempts: 4, NextAt: later, Created: t0, SentAt: later, FailedAt: later, LastStatus: 200, Retried: true}
	if got != want {
		t.Errorf("row = %+v, want %+v", got, want)
	}
	err := d.Tx(context.Background(), func(x bus.Tx) error { return x.UpdateOutbox(bus.OutboxRow{ID: 999, State: bus.OutboxSent}) })
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("updating a missing row: %v, want ErrNotFound", err)
	}
}

func TestFailedSince(t *testing.T) {
	d, _ := openTemp(t)
	mk := func(state bus.OutboxState, failed time.Time) int64 {
		r := outboxRow(bus.OutboxDuetPost, "itm", t0)
		r.State, r.FailedAt = state, failed
		return addRow(t, d, r)
	}
	edge := mk(bus.OutboxFailed, t0.Add(10*time.Minute))
	newer := mk(bus.OutboxFailed, t0.Add(20*time.Minute))
	mk(bus.OutboxFailed, t0.Add(10*time.Minute-time.Nanosecond))
	mk(bus.OutboxPending, t0.Add(30*time.Minute))
	var rows []bus.OutboxRow
	tx(t, d, func(x bus.Tx) (err error) { rows, err = x.FailedSince(t0.Add(10 * time.Minute)); return })
	if got := ids(rows); !equalIDs(got, []int64{edge, newer}) {
		t.Errorf("failed since = %v, want %v", got, []int64{edge, newer})
	}
}

func addPush(d *db.DB, src bus.PushSource, id int64, item string, rev int, at time.Time) error {
	return d.Tx(context.Background(), func(x bus.Tx) error {
		return x.InsertPush(bus.Push{Source: src, SourceID: id, ItemID: item, Revision: rev, At: at})
	})
}

func TestLedgerKeepsOneRowPerSourceAndItem(t *testing.T) {
	d, _ := openTemp(t)
	for _, err := range []error{addPush(d, bus.PushOutbox, 1, "itm_1", 1, t0), addPush(d, bus.PushNudge, 1, "itm_1", 2, t0.Add(time.Minute)),
		addPush(d, bus.PushOutbox, 2, "itm_2", 1, t0.Add(time.Hour))} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := addPush(d, bus.PushOutbox, 1, "itm_1", 3, t0.Add(2*time.Minute)); !db.IsConflict(err) {
		t.Errorf("a second ledger row for one source and item: %v, want a conflict", err)
	}
}

func seedLedger(t *testing.T, d *db.DB) {
	t.Helper()
	for _, err := range []error{addPush(d, bus.PushOutbox, 1, "itm_1", 1, t0), addPush(d, bus.PushNudge, 1, "itm_1", 2, t0.Add(time.Minute))} {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLastPushIsTheNewestOfAnyRevision(t *testing.T) {
	d, _ := openTemp(t)
	seedLedger(t, d)
	var last time.Time
	var found, other bool
	tx(t, d, func(x bus.Tx) (err error) {
		last, found, err = x.LastPush("itm_1")
		if err == nil {
			_, other, err = x.LastPush("itm_9")
		}
		return err
	})
	if !found || !last.Equal(t0.Add(time.Minute)) || other {
		t.Errorf("LastPush(itm_1) = %v %v, itm_9 found=%v; want the newest of any revision, and none for another item", last, found, other)
	}
}

func TestPushesAreInTimeOrder(t *testing.T) {
	d, _ := openTemp(t)
	seedLedger(t, d)
	var rows []bus.Push
	tx(t, d, func(x bus.Tx) (err error) { rows, err = x.Pushes("itm_1"); return })
	if len(rows) != 2 || rows[0].Source != bus.PushOutbox || rows[1].Source != bus.PushNudge || rows[1].Revision != 2 {
		t.Errorf("Pushes = %+v, want the two rows for itm_1 in time order", rows)
	}
}

func TestLastPageSentIsPerSwarmAndPagesOnly(t *testing.T) {
	d, _ := openTemp(t)
	mk := func(kind bus.OutboxKind, swarm string, state bus.OutboxState, sent time.Time) {
		r := outboxRow(kind, "itm", t0)
		r.Swarm, r.State, r.SentAt = swarm, state, sent
		addRow(t, d, r)
	}
	mk(bus.OutboxPage, "crew", bus.OutboxSent, t0.Add(time.Minute))
	mk(bus.OutboxPage, "crew", bus.OutboxSent, t0.Add(5*time.Minute))
	mk(bus.OutboxPage, "crew", bus.OutboxPending, t0.Add(9*time.Minute))
	mk(bus.OutboxDuetPost, "crew", bus.OutboxSent, t0.Add(8*time.Minute))
	mk(bus.OutboxPage, "other", bus.OutboxSent, t0.Add(7*time.Minute))
	var at time.Time
	var found, none bool
	tx(t, d, func(x bus.Tx) (err error) {
		at, found, err = x.LastPageSent("crew")
		if err != nil {
			return err
		}
		_, none, err = x.LastPageSent("client")
		return err
	})
	if !found || !at.Equal(t0.Add(5*time.Minute)) || none {
		t.Errorf("LastPageSent(crew) = %v %v, client found=%v; want the newest sent page of that swarm", at, found, none)
	}
}

func TestLastPageSentForItemIsThatItemsPagesOnly(t *testing.T) {
	d, _ := openTemp(t)
	mk := func(kind bus.OutboxKind, item string, state bus.OutboxState, sent time.Time) {
		r := outboxRow(kind, item, t0)
		r.State, r.SentAt = state, sent
		addRow(t, d, r)
	}
	mk(bus.OutboxPage, "itm_1", bus.OutboxSent, t0.Add(time.Minute))
	mk(bus.OutboxPage, "itm_1", bus.OutboxSent, t0.Add(5*time.Minute))
	mk(bus.OutboxPage, "itm_1", bus.OutboxPending, t0.Add(9*time.Minute))
	mk(bus.OutboxDuetPost, "itm_1", bus.OutboxSent, t0.Add(8*time.Minute))
	mk(bus.OutboxPage, "itm_2", bus.OutboxSent, t0.Add(7*time.Minute))
	var at time.Time
	var found, none bool
	tx(t, d, func(x bus.Tx) (err error) {
		at, found, err = x.LastPageSentForItem("itm_1")
		if err == nil {
			_, none, err = x.LastPageSentForItem("itm_9")
		}
		return err
	})
	if !found || !at.Equal(t0.Add(5*time.Minute)) || none {
		t.Errorf("LastPageSentForItem(itm_1) = %v %v, itm_9 found=%v; want the newest sent page of that item only", at, found, none)
	}
}
