package bus_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

type outcome struct {
	rec bus.Record
	err error
}

// race runs every call at once, released together, and returns their outcomes in order.
func race(calls []func() outcome) []outcome {
	out := make([]outcome, len(calls))
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for i, c := range calls {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			out[i] = c()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return out
}

// oneWinner fails the test unless exactly one outcome succeeded and every other is
// already_decided with that one as its Winner.
func oneWinner(t *testing.T, out []outcome) *bus.Record {
	t.Helper()
	var winner *bus.Record
	for _, o := range out {
		if o.err == nil {
			if winner != nil {
				t.Fatalf("two answers won: %+v and %+v", *winner, o.rec)
			}
			winner = &o.rec
		}
	}
	if winner == nil {
		t.Fatal("no answer won")
	}
	for i, o := range out {
		if o.err == nil {
			continue
		}
		be := errWithCode(t, o.err, bus.CodeAlreadyDecided)
		if be.Winner == nil || !reflectEqual(*be.Winner, *winner) {
			t.Errorf("answer %d: Winner = %+v, want %+v", i, be.Winner, *winner)
		}
	}
	return winner
}

func TestConcurrentAnswersOneWins(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	var calls []func() outcome
	for i := range 20 {
		in := answerIn(q)
		in.Text = fmt.Sprintf("answer %d", i)
		calls = append(calls, func() outcome { r, err := e.b.RecordAnswer(ctx, duet, in); return outcome{r, err} })
	}
	for i := range 10 {
		in := relayIn(q)
		in.Text = fmt.Sprintf("relay %d", i)
		calls = append(calls, func() outcome {
			_, err := e.b.RelayWords(ctx, node(fmt.Sprintf("n-%d", i), "crew"), in)
			return outcome{err: err}
		})
	}
	out := race(calls)
	winner := oneWinner(t, out[:20])
	for i, o := range out[20:] {
		if o.err != nil {
			t.Errorf("relay %d: %v", i, o.err)
		}
	}
	c, _ := e.d.Check(ctx)
	same(t, "decisions, relays, answer events", []any{c.Decisions, c.Relays, e.countType(t, bus.EventAnswerRecorded)}, []any{1, 10, 1})
	if eff, _ := e.effective(t, q.ID, 1); eff.ActorKind != bus.ActorAgent || !reflectEqual(eff, *winner) {
		t.Errorf("stored decision = %+v", eff)
	}
}

func TestConcurrentDeskAndDuetAnswers(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	var calls []func() outcome
	for i := range 10 {
		d, h := answerIn(q), answerIn(q)
		d.Text, h.Text = fmt.Sprintf("duet %d", i), fmt.Sprintf("desk %d", i)
		calls = append(calls,
			func() outcome { r, err := e.b.RecordAnswer(ctx, duet, d); return outcome{r, err} },
			func() outcome { r, err := e.b.RecordAnswer(ctx, deskP, h); return outcome{r, err} })
	}
	humans, agents := 0, 0
	for i, o := range race(calls) {
		switch {
		case o.err != nil:
			wantCode(t, o.err, bus.CodeAlreadyDecided)
		case i%2 == 0:
			agents++
		default:
			humans++
		}
	}
	c, _ := e.d.Check(ctx)
	eff, _ := e.effective(t, q.ID, 1)
	same(t, "human winners, decisions, effective, superseded events", []any{humans, c.Decisions, eff.ActorKind, e.countType(t, bus.EventAnswerSuperseded)},
		[]any{1, 1 + agents, bus.ActorHuman, agents})
	if agents > 1 {
		t.Errorf("%d agent rows", agents)
	}
}
