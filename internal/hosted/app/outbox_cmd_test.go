package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func (a adminEnv) outboxRows() []bus.OutboxRow {
	a.t.Helper()
	d, err := db.Open(a.path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var rows []bus.OutboxRow
	if err := d.Tx(context.Background(), func(x bus.Tx) (err error) { rows, err = x.AllOutbox(); return }); err != nil {
		a.t.Fatal(err)
	}
	return rows
}

func (a adminEnv) seedDB() {
	a.t.Helper()
	a.create("token", "create", "--name", "ci-1", "--swarm", "crew") // creates the database
}

func TestOutboxTestQueuesOneTestPush(t *testing.T) {
	a := newAdmin(t)
	a.seedDB()
	code, out, errs := a.run("outbox", "test")
	if code != ExitOK || errs != "" || !strings.Contains(out, "queued") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	rows := a.outboxRows()
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	wantTestPush(t, rows[0])
	if code, _, _ := a.run("outbox", "test", "--db", a.path); code != ExitOK || len(a.outboxRows()) != 2 {
		t.Errorf("a second test push: code=%d rows=%d", code, len(a.outboxRows()))
	}
}

func wantTestPush(t *testing.T, r bus.OutboxRow) {
	t.Helper()
	if r.Kind != bus.OutboxTest || r.Title != "yo-human" || r.Body != "test push" || r.Level != "passive" || r.State != bus.OutboxPending || r.ItemID != "" {
		t.Errorf("row = %+v, want a pending passive test push naming no item", r)
	}
	if r.NextAt.After(time.Now()) || time.Since(r.Created) > time.Minute {
		t.Errorf("row times = %v %v, want due now", r.NextAt, r.Created)
	}
}

func TestOutboxRefusesAMissingDatabaseAndBadUsage(t *testing.T) {
	a := newAdmin(t)
	if code, out, errs := a.run("outbox", "test"); code != ExitFailed || out != "" || !strings.Contains(errs, "no database") {
		t.Errorf("missing database: code=%d out=%q err=%q", code, out, errs)
	}
	for _, args := range [][]string{{"outbox"}, {"outbox", "bogus"}, {"outbox", "test", "extra"}, {"outbox", "test", "--bogus"}} {
		if code, out, _ := a.run(args...); code != ExitUsage || out != "" {
			t.Errorf("%v: code=%d out=%q, want usage", args, code, out)
		}
	}
}

func TestUsageNamesOutbox(t *testing.T) {
	if !strings.Contains(usage, "outbox") {
		t.Error("the usage text does not mention outbox")
	}
}

func (a adminEnv) raiseBroken(status int) {
	a.t.Helper()
	d, err := db.Open(a.path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	err = d.Tx(context.Background(), func(x bus.Tx) error {
		return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), Status: status})
	})
	if err != nil {
		a.t.Fatal(err)
	}
}

func TestCheckExitsOneAndPrintsTheCountsWhileDeliveryIsNotHealthy(t *testing.T) {
	a := newAdmin(t)
	a.seedDB()
	a.raiseBroken(401)
	code, out, errs := a.run("check")
	for _, want := range []string{"integrity: ok\n", "tokens: 1\n", "delivery: broken\n", "delivery_since: 2026-11-02T09:00:00Z\n", "delivery_status: 401\n",
		"delivery_failed: 0\n", "delivery_pending: 0\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if code != ExitDegraded || errs == "" {
		t.Errorf("code=%d err=%q, want exit 1 with a word on stderr", code, errs)
	}
}

func TestCheckExitsOneForARefusedMessageAndAnOverduePushOnly(t *testing.T) {
	a := newAdmin(t)
	a.seedDB()
	d, err := db.Open(a.path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour)
	_ = d.Tx(context.Background(), func(x bus.Tx) error {
		_, err := x.InsertOutbox(bus.OutboxRow{Kind: bus.OutboxTest, Level: "passive", Title: "t", Body: "b", State: bus.OutboxPending, NextAt: old, Created: old})
		return err
	})
	_ = d.Close()
	code, out, _ := a.run("check")
	if code != ExitDegraded || !strings.Contains(out, "delivery: stuck\n") || !strings.Contains(out, "delivery_pending: 1\n") || strings.Contains(out, "delivery: broken") {
		t.Errorf("code=%d out=%q, want an overdue pending row called stuck", code, out)
	}
}

func TestCheckExitsOneForARowThatKeepsFailing(t *testing.T) {
	a := newAdmin(t)
	a.seedDB()
	d, err := db.Open(a.path)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Hour)
	_ = d.Tx(context.Background(), func(x bus.Tx) error {
		_, err := x.InsertOutbox(bus.OutboxRow{Kind: bus.OutboxTest, Level: "passive", Title: "t", Body: "b", State: bus.OutboxPending,
			Attempts: 4, NextAt: time.Now().UTC().Add(time.Minute), Created: created})
		return err
	})
	_ = d.Close()
	if code, out, _ := a.run("check"); code != ExitDegraded || !strings.Contains(out, "delivery_pending: 1\n") {
		t.Errorf("code=%d out=%q, want exit 1 for a row retried for an hour", code, out)
	}
}
