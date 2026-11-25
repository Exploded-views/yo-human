package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// outboxCalls is every Task 11 store method, each as a call that returns only its error.
func outboxCalls(x bus.Tx) map[string]func() error {
	return map[string]func() error{
		"InsertOutbox":   func() error { _, err := x.InsertOutbox(outboxRow(bus.OutboxTest, "", t0)); return err },
		"UpdateOutbox":   func() error { return x.UpdateOutbox(bus.OutboxRow{ID: 1, State: bus.OutboxSent}) },
		"PendingOutbox":  func() error { _, err := x.PendingOutbox(t0, 10); return err },
		"FailedSince":    func() error { _, err := x.FailedSince(t0); return err },
		"AllOutbox":      func() error { _, err := x.AllOutbox(); return err },
		"LastPush":       func() error { _, _, err := x.LastPush("i"); return err },
		"InsertPush":     func() error { return x.InsertPush(bus.Push{Source: bus.PushNudge, SourceID: 1, ItemID: "i", At: t0}) },
		"Pushes":         func() error { _, err := x.Pushes("i"); return err },
		"LastPageSent":   func() error { _, _, err := x.LastPageSent("s"); return err },
		"NudgeState":     func() error { _, err := x.NudgeState(); return err },
		"PutNudgeState":  func() error { return x.PutNudgeState(bus.NudgeState{}) },
		"Deferred":       func() error { _, err := x.Deferred(); return err },
		"PutDeferred":    func() error { return x.PutDeferred(bus.Deferred{ItemID: "i", DueAt: t0}) },
		"DeleteDeferred": func() error { return x.DeleteDeferred("i") },
		"BrokenFlag":     func() error { _, err := x.BrokenFlag(); return err },
		"PutBrokenFlag":  func() error { return x.PutBrokenFlag(bus.BrokenFlag{}) },
		"DeliveryState":  func() error { _, err := x.DeliveryState(t0); return err },
	}
}

func eachCallFails(t *testing.T, d *db.DB, names ...string) {
	t.Helper()
	for _, name := range names {
		_ = d.Tx(context.Background(), func(x bus.Tx) error {
			if err := outboxCalls(x)[name](); err == nil {
				t.Errorf("%s: no error", name)
			}
			return nil
		})
	}
}

func TestOutboxStoreReportsQueryFailures(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path, "DROP TABLE outbox", "DROP TABLE owner_pushes", "DROP TABLE nudge_state", "DROP TABLE nudge_deferred", "DROP TABLE delivery_flag")
	var all []string
	for name := range outboxCalls(nil) {
		all = append(all, name)
	}
	eachCallFails(t, d, all...)
}

func TestOutboxStoreReportsUnreadableTimes(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path,
		"INSERT INTO outbox (kind, swarm, item_id, revision, level, title, body, state, next_at, created) VALUES ('test', 's', '', 0, 'passive', 't', 'b', 'sent', 'bad', 'bad')",
		"INSERT INTO owner_pushes (source, source_id, item_id, revision, at) VALUES ('nudge', 1, 'i', 1, 'bad')",
		"UPDATE nudge_state SET last_sent = 'bad'",
		"INSERT INTO nudge_deferred (item_id, due_at) VALUES ('i', 'bad')",
		"UPDATE delivery_flag SET since = 'bad'",
	)
	eachCallFails(t, d, "AllOutbox", "Pushes", "NudgeState", "Deferred", "BrokenFlag", "DeliveryState")
	rawExec(t, path, "UPDATE nudge_state SET last_sent = '', failed_at = 'bad'")
	eachCallFails(t, d, "NudgeState")
	rawExec(t, path, "UPDATE nudge_state SET failed_at = ''", "UPDATE outbox SET state = 'sent', sent_at = 'bad', kind = 'page'")
	eachCallFails(t, d, "LastPageSent")
	rawExec(t, path, "UPDATE owner_pushes SET at = 'bad'")
	eachCallFails(t, d, "LastPush")
}

func TestLatestTreatsAnEmptyTimeAsNone(t *testing.T) {
	d, path := openTemp(t)
	rawExec(t, path, "INSERT INTO outbox (kind, swarm, item_id, revision, level, title, body, state, next_at, created) VALUES ('page', 's', '', 0, 'passive', 't', 'b', 'sent', '', '')")
	var at time.Time
	var found bool
	tx(t, d, func(x bus.Tx) (err error) { at, found, err = x.LastPageSent("s"); return })
	if found || !at.IsZero() {
		t.Errorf("a sent page with no time: %v %v, want none", at, found)
	}
}
