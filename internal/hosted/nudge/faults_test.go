package nudge_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/nudge"
)

var errDisk = errors.New("disk /secret/path on fire")

// faultStore fails the nth call of one Tx method, once armed.
type faultStore struct {
	bus.Store
	method string
	nth    int32
	calls  atomic.Int32
	armed  atomic.Bool
	hit    atomic.Bool
}

func (f *faultStore) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return f.Store.Tx(ctx, func(x bus.Tx) error { return fn(faultTx{x, f}) })
}

type faultTx struct {
	bus.Tx
	f *faultStore
}

func (t faultTx) fail(method string) error {
	if !t.f.armed.Load() || method != t.f.method {
		return nil
	}
	if t.f.calls.Add(1) == t.f.nth {
		t.f.hit.Store(true)
		return errDisk
	}
	return nil
}

func (t faultTx) NudgeState() (bus.NudgeState, error) {
	if err := t.fail("NudgeState"); err != nil {
		return bus.NudgeState{}, err
	}
	return t.Tx.NudgeState()
}

func (t faultTx) PutNudgeState(n bus.NudgeState) error {
	if err := t.fail("PutNudgeState"); err != nil {
		return err
	}
	return t.Tx.PutNudgeState(n)
}

func (t faultTx) EventsSince(s string, since int64, n int) ([]bus.Event, error) {
	if err := t.fail("EventsSince"); err != nil {
		return nil, err
	}
	return t.Tx.EventsSince(s, since, n)
}

func (t faultTx) ItemByID(id string) (bus.Item, bool, error) {
	if err := t.fail("ItemByID"); err != nil {
		return bus.Item{}, false, err
	}
	return t.Tx.ItemByID(id)
}

func (t faultTx) Messages(id string) ([]bus.Message, error) {
	if err := t.fail("Messages"); err != nil {
		return nil, err
	}
	return t.Tx.Messages(id)
}

func (t faultTx) Deferred() ([]bus.Deferred, error) {
	if err := t.fail("Deferred"); err != nil {
		return nil, err
	}
	return t.Tx.Deferred()
}

func (t faultTx) PutDeferred(d bus.Deferred) error {
	if err := t.fail("PutDeferred"); err != nil {
		return err
	}
	return t.Tx.PutDeferred(d)
}

func (t faultTx) DeleteDeferred(id string) error {
	if err := t.fail("DeleteDeferred"); err != nil {
		return err
	}
	return t.Tx.DeleteDeferred(id)
}

func (t faultTx) LastPush(id string) (time.Time, bool, error) {
	if err := t.fail("LastPush"); err != nil {
		return time.Time{}, false, err
	}
	return t.Tx.LastPush(id)
}

func (t faultTx) InsertPush(p bus.Push) error {
	if err := t.fail("InsertPush"); err != nil {
		return err
	}
	return t.Tx.InsertPush(p)
}

func (t faultTx) LastPageSent(s string) (time.Time, bool, error) {
	if err := t.fail("LastPageSent"); err != nil {
		return time.Time{}, false, err
	}
	return t.Tx.LastPageSent(s)
}

func (t faultTx) PendingOutbox(now time.Time, n int) ([]bus.OutboxRow, error) {
	if err := t.fail("PendingOutbox"); err != nil {
		return nil, err
	}
	return t.Tx.PendingOutbox(now, n)
}

func (t faultTx) UpdateOutbox(r bus.OutboxRow) error {
	if err := t.fail("UpdateOutbox"); err != nil {
		return err
	}
	return t.Tx.UpdateOutbox(r)
}

func (t faultTx) FailedSince(s time.Time) ([]bus.OutboxRow, error) {
	if err := t.fail("FailedSince"); err != nil {
		return nil, err
	}
	return t.Tx.FailedSince(s)
}

func (t faultTx) Decision(id string, rev int) (bus.Record, bool, error) {
	if err := t.fail("Decision"); err != nil {
		return bus.Record{}, false, err
	}
	return t.Tx.Decision(id, rev)
}

func (t faultTx) BrokenFlag() (bus.BrokenFlag, error) {
	if err := t.fail("BrokenFlag"); err != nil {
		return bus.BrokenFlag{}, err
	}
	return t.Tx.BrokenFlag()
}

func (t faultTx) PutBrokenFlag(f bus.BrokenFlag) error {
	if err := t.fail("PutBrokenFlag"); err != nil {
		return err
	}
	return t.Tx.PutBrokenFlag(f)
}

func (t faultTx) ListInboxItems(since int64) ([]bus.Item, error) {
	if err := t.fail("ListInboxItems"); err != nil {
		return nil, err
	}
	return t.Tx.ListInboxItems(since)
}

var faultMethods = []string{"NudgeState", "PutNudgeState", "EventsSince", "ItemByID", "Messages", "Deferred", "PutDeferred", "DeleteDeferred",
	"LastPush", "InsertPush", "LastPageSent", "PendingOutbox", "UpdateOutbox", "FailedSince", "Decision", "BrokenFlag", "PutBrokenFlag", "ListInboxItems"}

// busyRig is a database where one tick has something of everything to do: a nudge, a deferred
// item, a Duet message, a page, a held Duet message, a test row and a young refused row.
func busyRig(t *testing.T, chanErr error) (*rig, *faultStore) {
	t.Helper()
	r := newRig(t)
	fs := &faultStore{Store: r.d}
	r.w = nudge.NewWorker(r.b, fs, r.ch, r.cfg, r.clk.now)
	r.post(crewNode, question("n1"))
	th := r.duetThread("t1")
	r.pushed(th.ID, t0.Add(-20*time.Minute))
	r.reply(crewNode, th.ID, "held", bus.SeverityNormal, bus.UrgencyDigest)
	q := r.post(crewNode, quietItem("q1"))
	r.duetSays(q, "one")
	r.duetSays(q, "two")
	r.post(crewNode, pageItem("p1"))
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "x")
	failed := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "y")
	r.do(func(x bus.Tx) error {
		return x.UpdateOutbox(bus.OutboxRow{ID: failed, State: bus.OutboxFailed, FailedAt: t0.Add(-time.Minute), NextAt: t0})
	})
	r.ch.setDefault(chanErr)
	return r, fs
}

// TestAStoreFailureIsNeverSwallowed fails each store call a tick makes, in turn, under three
// channel answers (accepted, refused, and broken), and wants the tick to say so every time.
func TestAStoreFailureIsNeverSwallowed(t *testing.T) {
	for _, chanErr := range []error{nil, statusErr(400), statusErr(401), statusErr(500)} {
		for _, m := range faultMethods {
			for n := int32(1); n < 40; n++ {
				r, fs := busyRig(t, chanErr)
				fs.method, fs.nth = m, n
				fs.armed.Store(true)
				err := r.w.Tick(context.Background())
				if !fs.hit.Load() {
					break
				}
				if err == nil {
					t.Errorf("channel %v: call %d of %s failed and Tick returned nil", chanErr, n, m)
				}
			}
		}
	}
}
