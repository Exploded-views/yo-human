package bus_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func (e *env) breakChannel(t *testing.T, status int) {
	t.Helper()
	err := e.d.Tx(context.Background(), func(x bus.Tx) error {
		return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: status})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInboxAndItemCarryNoDeliveryWhileHealthy(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	in, err := e.b.Inbox(context.Background(), duet, 0)
	v, err2 := e.b.Item(context.Background(), duet, it.ID)
	if err != nil || err2 != nil || in.Delivery != nil || v.Delivery != nil {
		t.Errorf("inbox delivery %+v (%v), item delivery %+v (%v), want nil", in.Delivery, err, v.Delivery, err2)
	}
}

func TestInboxAndItemShowABrokenChannel(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	e.breakChannel(t, 401)
	in, err := e.b.Inbox(context.Background(), duet, 0)
	if err != nil || in.Delivery == nil || !in.Delivery.Broken || in.Delivery.Status != 401 || !in.Delivery.Since.Equal(t0) {
		t.Errorf("inbox delivery = %+v (%v), want broken with status 401 since t0", in.Delivery, err)
	}
	v, err := e.b.Item(context.Background(), duet, it.ID)
	if err != nil || v.Delivery == nil || !v.Delivery.Broken || v.Delivery.Status != 401 {
		t.Errorf("item delivery = %+v (%v), want the same", v.Delivery, err)
	}
}

func TestItemShowsDeliveryToDuetOnly(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	e.breakChannel(t, 403)
	v, err := e.b.Item(context.Background(), node("ci-1", "crew"), it.ID)
	if err != nil || v.Delivery != nil {
		t.Errorf("a node sees delivery %+v (%v), want nil: channel health is Duet's", v.Delivery, err)
	}
}

func TestWaitingCountsTheGroupsOnly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if n, err := e.b.Waiting(ctx); err != nil || n != 0 {
		t.Fatalf("empty: %d, %v", n, err)
	}
	e.post(t, node("ci-1", "crew"), notice("a"))
	q := notice("b")
	q.Kind, q.Action = bus.KindQuestion, bus.ActionNone
	e.post(t, node("ci-1", "crew"), q)
	good := notice("c")
	good.Urgency = bus.UrgencyInterruptWhenGood
	e.post(t, node("ci-1", "crew"), good) // the interrupt lane is not in a group
	e.post(t, duet, notice("d"))          // Duet's own posts are not waiting for Duet
	if n, err := e.b.Waiting(ctx); err != nil || n != 1 {
		t.Errorf("Waiting = %d, %v, want 1: the question, since a notice has no inbox group", n, err)
	}
}

// failing is a Store whose DeliveryState or ListInboxItems fails.
type failing struct {
	bus.Store
	delivery, inbox bool
}

type failingTx struct {
	bus.Tx
	f failing
}

func (f failing) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return f.Store.Tx(ctx, func(x bus.Tx) error { return fn(failingTx{x, f}) })
}

func (t failingTx) DeliveryState(now time.Time) (*bus.DeliveryState, error) {
	if t.f.delivery {
		return nil, errDisk
	}
	return t.Tx.DeliveryState(now)
}

func (t failingTx) ListInboxItems(since int64) ([]bus.Item, error) {
	if t.f.inbox {
		return nil, errDisk
	}
	return t.Tx.ListInboxItems(since)
}

func TestReadsReportAStoreFailureWithoutItsText(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	ctx := context.Background()
	bd := bus.New(failing{Store: e.d, delivery: true}, e.now, e.l)
	bi := bus.New(failing{Store: e.d, inbox: true}, e.now, e.l)
	_, err1 := bd.Inbox(ctx, duet, 0)
	_, err2 := bd.Item(ctx, duet, it.ID)
	_, err3 := bi.Waiting(ctx)
	for i, err := range []error{err1, err2, err3} {
		if be, ok := bus.AsError(err); !ok || be.Code != bus.CodeInternal || strings.Contains(err.Error(), "secret") {
			t.Errorf("call %d: %v, want an internal error that names no path", i, err)
		}
	}
}
