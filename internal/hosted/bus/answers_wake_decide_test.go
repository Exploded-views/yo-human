package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A waiting Answers call is woken by every write that appends an event it returns, the owner's
// answer and approval included: this is the call's whole purpose, an agent hearing back.
func TestAnswersLongPollWakesOnAnswerAndApproval(t *testing.T) {
	for _, c := range []struct {
		name string
		item func(*env) bus.Item
		do   func(*env, bus.Item) error
		want bus.EventType
	}{
		{"an answer", func(e *env) bus.Item { return e.questionItem(t) },
			func(e *env, it bus.Item) error { _, err := e.b.RecordAnswer(ctx, duet, answerIn(it)); return err }, bus.EventAnswerRecorded},
		{"an approval", func(e *env) bus.Item { return e.approvalItem(t) },
			func(e *env, it bus.Item) error { _, err := e.b.RecordApproval(ctx, passkey, approveIn(it)); return err }, bus.EventApprovalRecorded},
	} {
		e := newEnv(t)
		it := c.item(e)
		next := e.answers(t, cinode, 0, 0).Next
		ft := newFakeTimer(t, e.b)
		res := poll(e.b, context.Background(), cinode, next, 20*time.Second)
		ft.waitStarted(t)
		if err := c.do(e, it); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		r := within(t, res, 5*time.Second, c.name)
		if r.err != nil || len(r.u.Events) != 1 || r.u.Events[0].Type != c.want {
			t.Errorf("%s: result = %+v, want the one %s event", c.name, r, c.want)
		}
	}
}

// TestAnswersLongPollWakesOnHumanSupersede: the desk's human answer over an agent's appends two
// events, and the waiter returns both.
func TestAnswersLongPollWakesOnHumanSupersede(t *testing.T) {
	e := newEnv(t)
	it := e.questionItem(t)
	if _, err := e.b.RecordAnswer(ctx, duet, answerIn(it)); err != nil {
		t.Fatal(err)
	}
	next := e.answers(t, cinode, 0, 0).Next
	ft := newFakeTimer(t, e.b)
	res := poll(e.b, context.Background(), cinode, next, 20*time.Second)
	ft.waitStarted(t)
	if _, err := e.b.RecordAnswer(ctx, deskP, answerIn(it)); err != nil {
		t.Fatal(err)
	}
	r := within(t, res, 5*time.Second, "supersede")
	if len(r.u.Events) != 2 || r.u.Events[0].Type != bus.EventAnswerRecorded || r.u.Events[1].Type != bus.EventAnswerSuperseded {
		t.Errorf("result = %+v, want answer_recorded then answer_superseded", r)
	}
}
