package bus_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// faulty is a Store whose nth call (1-based) of the named Tx method fails with an error that
// carries a path and a word the caller must never see.
type faulty struct {
	bus.Store
	method string
	nth    int
	calls  int
}

var errDisk = errors.New("disk /secret/path on fire")

func (f *faulty) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return f.Store.Tx(ctx, func(x bus.Tx) error { return fn(&faultyTx{Tx: x, f: f}) })
}

type faultyTx struct {
	bus.Tx
	f *faulty
}

func (t *faultyTx) hit(method string) error {
	if method != t.f.method {
		return nil
	}
	if t.f.calls++; t.f.calls == t.f.nth {
		return errDisk
	}
	return nil
}

func (t *faultyTx) ItemByID(id string) (bus.Item, bool, error) {
	if err := t.hit("ItemByID"); err != nil {
		return bus.Item{}, false, err
	}
	return t.Tx.ItemByID(id)
}

func (t *faultyTx) ItemByHandle(s, h string) (bus.Item, bool, error) {
	if err := t.hit("ItemByHandle"); err != nil {
		return bus.Item{}, false, err
	}
	return t.Tx.ItemByHandle(s, h)
}

func (t *faultyTx) InsertItem(it bus.Item) error {
	if err := t.hit("InsertItem"); err != nil {
		return err
	}
	return t.Tx.InsertItem(it)
}

func (t *faultyTx) UpdateItem(it bus.Item) error {
	if err := t.hit("UpdateItem"); err != nil {
		return err
	}
	return t.Tx.UpdateItem(it)
}

func (t *faultyTx) AppendEvent(ev bus.Event) (int64, error) {
	if err := t.hit("AppendEvent"); err != nil {
		return 0, err
	}
	return t.Tx.AppendEvent(ev)
}

func (t *faultyTx) Decision(id string, rev int) (bus.Record, bool, error) {
	if err := t.hit("Decision"); err != nil {
		return bus.Record{}, false, err
	}
	return t.Tx.Decision(id, rev)
}

func (t *faultyTx) InsertMessage(m bus.Message) error {
	if err := t.hit("InsertMessage"); err != nil {
		return err
	}
	return t.Tx.InsertMessage(m)
}

// TestStoreFailureIsInternalAndRollsBack: every store call a write makes can fail; the caller
// gets code internal with no detail of the cause, and nothing is left half written.
func TestStoreFailureIsInternalAndRollsBack(t *testing.T) {
	changed := mergeApproval("m7")
	changed.Title = "Merge PR 7 again"
	repost := func(b *bus.Bus, id string) error {
		_, err := b.Post(context.Background(), node("ci-1", "crew"), changed)
		return err
	}
	create := func(b *bus.Bus, id string) error {
		_, err := b.Post(context.Background(), node("ci-1", "crew"), notice("new"))
		return err
	}
	reply := func(b *bus.Bus, id string) error {
		_, err := b.Reply(context.Background(), node("ci-1", "crew"), id, "hi", bus.SeverityNormal)
		return err
	}
	for _, c := range []struct {
		name, method string
		nth          int
		op           func(*bus.Bus, string) error
	}{
		{"create: handle lookup", "ItemByHandle", 1, create},
		{"create: event", "AppendEvent", 1, create},
		{"create: insert", "InsertItem", 1, create},
		{"revise: handle lookup", "ItemByHandle", 1, repost},
		{"revise: event", "AppendEvent", 1, repost},
		{"revise: decision read", "Decision", 1, repost},
		{"revise: void event", "AppendEvent", 2, repost},
		{"revise: update", "UpdateItem", 1, repost},
		{"reply: item read", "ItemByID", 1, reply},
		{"reply: message", "InsertMessage", 1, reply},
		{"reply: event", "AppendEvent", 1, reply},
		{"reply: update", "UpdateItem", 1, reply},
	} {
		e := newEnv(t)
		it := e.post(t, node("ci-1", "crew"), mergeApproval("m7")).Item
		approveRevision(t, e, it.ID, 1)
		before := len(e.events(t))
		f := &faulty{Store: e.d, method: c.method, nth: c.nth}
		err := c.op(bus.New(f, e.now, e.l), it.ID)
		be := errWithCode(t, err, bus.CodeInternal)
		if strings.Contains(be.What+be.Valid+be.Fix+be.Error(), "secret") {
			t.Errorf("%s: the error leaks the cause: %v", c.name, be)
		}
		got, _ := e.stored(t, it.ID)
		same(t, c.name+": state after rollback", []any{len(e.events(t)), got.Revision, got.State, len(e.messages(t, it.ID))},
			[]any{before, 1, bus.StateDecided, 0})
	}
}
