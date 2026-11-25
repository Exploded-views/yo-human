package nudge_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A shutdown that lands while a send is in flight must not lose the bookkeeping of a push that
// went out: the ledger row (or the per-item cap breaks after the restart) and, for a failed one,
// the rollback of the nudge's state (or the nudge is silently lost).
func cancelledTick(t *testing.T, r *rig) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.ch.onSend = cancel
	if err := r.w.Tick(ctx); err != nil {
		t.Errorf("Tick = %v, want the bookkeeping done on a context the shutdown does not end", err)
	}
}

func TestShutdownBetweenAnOutboxSendAndItsLedgerWrite(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "hello")
	cancelledTick(t, r)
	if st := r.states()[1]; st != bus.OutboxSent {
		t.Errorf("row is %s, want sent", st)
	}
	if n := len(r.ledger(it.ID)); n != 1 {
		t.Errorf("%d ledger rows, want the push recorded", n)
	}
}

func TestShutdownBetweenANudgeSendAndItsLedgerWrite(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, question("a"))
	cancelledTick(t, r)
	if n := len(r.ledger(it.ID)); n != 1 {
		t.Errorf("%d ledger rows, want the nudge recorded", n)
	}
}

func TestShutdownAfterATransientNudgeFailureStillRestoresTheState(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.ch.script(statusErr(500))
	cancelledTick(t, r)
	r.ch.onSend = nil
	r.advance(time.Second)
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
}

func TestShutdownAfterABrokenChannelStillRaisesTheFlag(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "x")
	r.ch.script(statusErr(401))
	cancelledTick(t, r)
	if !r.flag().Raised {
		t.Error("the flag was not raised")
	}
}
