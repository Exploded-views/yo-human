package bus_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// TestRelayApprovalAfterPasskeyDecisionIsAlreadyDecided: once a revision has a decision, Duet is
// told so, with the winner, instead of being handed a link to an approval that is closed. Nothing
// is stored.
func TestRelayApprovalAfterPasskeyDecisionIsAlreadyDecided(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(it)); err != nil {
		t.Fatal(err)
	}
	s := e.snap(t, it.ID)
	in := approveIn(it)
	in.Decision, in.Text = bus.DecisionReject, "no, hold it"
	_, err := e.b.RelayApproval(ctx, duet, in)
	be := errWithCode(t, err, bus.CodeAlreadyDecided)
	if be.Winner == nil || be.Winner.ActorKind != bus.ActorHuman || be.Winner.Decision != bus.DecisionApprove {
		t.Errorf("winner = %+v, want the passkey approval", be.Winner)
	}
	e.unchanged(t, "relay after a decision", s)
	if got := len(e.relays(t, it.ID)); got != 0 {
		t.Errorf("relays stored = %d, want 0", got)
	}
}

// TestRelayApprovalWakesWaiters: every committed write wakes, a relay of the owner's words
// included, as RelayWords does.
func TestRelayApprovalWakesWaiters(t *testing.T) {
	e := newEnv(t)
	var reads atomic.Int32
	b := bus.New(countingStore{e.d, &reads}, e.now, e.l)
	it := e.approvalItem(t)
	ft := newFakeTimer(t, b)
	res := poll(b, context.Background(), cinode, e.answers(t, cinode, 0, 0).Next, 20*time.Second)
	ft.waitStarted(t)
	before := reads.Load()
	if _, err := b.RelayApproval(ctx, duet, approveIn(it)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for reads.Load() == before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if reads.Load() == before {
		t.Error("RelayApproval did not wake the waiter: it never read again")
	}
	ft.fire <- time.Now()
	within(t, res, 5*time.Second, "waiter after its timeout")
}
