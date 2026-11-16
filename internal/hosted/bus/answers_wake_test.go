package bus_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// wakeOnWrite parks a cinode waiter at the current cursor with a 20 s wait, runs write, and
// requires the waiter back within 200 ms of it with the timeout timer stopped, never fired.
func wakeOnWrite(t *testing.T, e *env, write func()) bus.Updates {
	t.Helper()
	next := e.answers(t, cinode, 0, 0).Next
	ft := newFakeTimer(t, e.b)
	res := poll(e.b, context.Background(), cinode, next, 20*time.Second)
	ft.waitStarted(t)
	start := time.Now()
	write()
	r := within(t, res, 5*time.Second, "waiter")
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("waiter returned %v after the write, want within 200ms", took)
	}
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	return r.u
}

func TestAnswersLongPollWakesOnAnswer(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	u := wakeOnWrite(t, e, func() {
		if _, err := e.b.RecordAnswer(ctx, duet, answerIn(q)); err != nil {
			t.Fatal(err)
		}
	})
	if len(u.Events) != 1 || u.Events[0].Type != bus.EventAnswerRecorded || u.Events[0].Record == nil || u.Events[0].Record.Text != "use 8080" {
		t.Errorf("updates = %+v, want the answer", u.Events)
	}
}

func TestAnswersLongPollWakesOnApproval(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	u := wakeOnWrite(t, e, func() {
		if _, err := e.b.RecordApproval(ctx, passkey, approveIn(it)); err != nil {
			t.Fatal(err)
		}
	})
	if len(u.Events) != 1 || u.Events[0].Type != bus.EventApprovalRecorded || u.Events[0].Record == nil || u.Events[0].Record.Decision != bus.DecisionApprove {
		t.Errorf("updates = %+v, want the approval", u.Events)
	}
}

func TestAnswersLongPollWakesOnSupersede(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	if _, err := e.b.RecordAnswer(ctx, duet, answerIn(q)); err != nil {
		t.Fatal(err)
	}
	u := wakeOnWrite(t, e, func() {
		in := answerIn(q)
		in.Text = "use 9090"
		if _, err := e.b.RecordAnswer(ctx, deskP, in); err != nil {
			t.Fatal(err)
		}
	})
	var got []bus.EventType
	for _, up := range u.Events {
		got = append(got, up.Type)
	}
	same(t, "update types", got, []bus.EventType{bus.EventAnswerRecorded, bus.EventAnswerSuperseded})
}

func TestAnswersLongPollIgnoresAnswerInOtherSwarm(t *testing.T) {
	e := newEnv(t)
	q := e.post(t, node("ci-9", "client"), askQuestion("q-client")).Item
	next := e.answers(t, cinode, 0, 0).Next
	ft := newFakeTimer(t, e.b)
	res := poll(e.b, context.Background(), cinode, next, 20*time.Second)
	ft.waitStarted(t)
	if _, err := e.b.RecordAnswer(ctx, duet, answerIn(q)); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-res:
		t.Fatalf("an answer in another swarm woke the waiter: %+v", r)
	case <-time.After(100 * time.Millisecond):
	}
	ft.fire <- time.Now()
	within(t, res, 5*time.Second, "waiter after its timeout")
}

// A relay's words_relayed is not an update, but the write still wakes the swarm's waiters (the
// rule on Bus.committed): the waiter looks again, finds nothing for it and waits on.
func TestAnswersLongPollWakesOnRelay(t *testing.T) {
	e := newEnv(t)
	var reads atomic.Int32
	b := bus.New(countingStore{e.d, &reads}, e.now, e.l)
	q := e.questionItem(t)
	ft := newFakeTimer(t, b)
	res := poll(b, context.Background(), cinode, e.answers(t, cinode, 0, 0).Next, 20*time.Second)
	ft.waitStarted(t)
	before := reads.Load()
	if _, err := b.RelayWords(ctx, duet, relayIn(q)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for reads.Load() == before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if reads.Load() == before {
		t.Error("a relay did not wake the waiter: it never read again")
	}
	ft.fire <- time.Now()
	within(t, res, 5*time.Second, "waiter after its timeout")
}
