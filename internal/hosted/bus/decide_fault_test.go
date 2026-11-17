package bus_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func (t *faultyTx) InsertDecision(r bus.Record) error {
	if err := t.hit("InsertDecision"); err != nil {
		return err
	}
	return t.Tx.InsertDecision(r)
}

func (t *faultyTx) InsertRelay(r bus.Relay) error {
	if err := t.hit("InsertRelay"); err != nil {
		return err
	}
	return t.Tx.InsertRelay(r)
}

// TestDecideStoreFailureIsInternalAndRollsBack: every store call a decision or relay makes can
// fail; the caller gets internal with no detail of the cause, and nothing is half written.
func TestDecideStoreFailureIsInternalAndRollsBack(t *testing.T) {
	approve := func(b *bus.Bus, it bus.Item) error {
		_, err := b.RecordApproval(ctx, passkey, approveIn(it))
		return err
	}
	answer := func(b *bus.Bus, it bus.Item) error { _, err := b.RecordAnswer(ctx, duet, answerIn(it)); return err }
	supersede := func(b *bus.Bus, it bus.Item) error { _, err := b.RecordAnswer(ctx, deskP, answerIn(it)); return err }
	relay := func(b *bus.Bus, it bus.Item) error { _, err := b.RelayWords(ctx, duet, relayIn(it)); return err }
	relayApproval := func(b *bus.Bus, it bus.Item) error { _, err := b.RelayApproval(ctx, duet, approveIn(it)); return err }
	for _, c := range []struct {
		name, method string
		nth          int
		pre          bool // the question already has Duet's answer
		approval     bool
		op           func(*bus.Bus, bus.Item) error
	}{
		{"approval: item read", "ItemByID", 1, false, true, approve},
		{"approval: decision read", "Decision", 1, false, true, approve},
		{"approval: insert", "InsertDecision", 1, false, true, approve},
		{"approval: event", "AppendEvent", 1, false, true, approve},
		{"approval: update", "UpdateItem", 1, false, true, approve},
		{"answer: insert", "InsertDecision", 1, false, false, answer},
		{"supersede: insert", "InsertDecision", 1, true, false, supersede},
		{"supersede: event", "AppendEvent", 1, true, false, supersede},
		{"supersede: superseded event", "AppendEvent", 2, true, false, supersede},
		{"supersede: update", "UpdateItem", 1, true, false, supersede},
		{"relay: item read", "ItemByID", 1, false, true, relay},
		{"relay: insert", "InsertRelay", 1, false, true, relay},
		{"relay: event", "AppendEvent", 1, false, true, relay},
		{"relay: update", "UpdateItem", 1, false, true, relay},
		{"relay approval: decision read", "Decision", 1, false, true, relayApproval},
		{"relay approval: insert", "InsertRelay", 1, false, true, relayApproval},
	} {
		e := newEnv(t)
		it := e.questionItem(t)
		if c.approval {
			it = e.approvalItem(t)
		}
		if c.pre {
			if _, err := e.b.RecordAnswer(ctx, duet, answerIn(it)); err != nil {
				t.Fatal(err)
			}
		}
		s := e.snap(t, it.ID)
		f := &faulty{Store: e.d, method: c.method, nth: c.nth}
		err := c.op(bus.New(f, e.now, e.l), it)
		be := errWithCode(t, err, bus.CodeInternal)
		if strings.Contains(be.What+be.Valid+be.Fix+be.Error(), "secret") {
			t.Errorf("%s: the error leaks the cause: %v", c.name, be)
		}
		e.unchanged(t, c.name, s)
	}
}

// blind hides the stored decision from the first n reads, as a read that ran before a racing
// writer committed would see it, so the insert meets the database's unique constraint.
type blind struct {
	bus.Store
	hide int
}

func (s *blind) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return s.Store.Tx(ctx, func(x bus.Tx) error { return fn(&blindTx{Tx: x, s: s}) })
}

type blindTx struct {
	bus.Tx
	s *blind
}

func (t *blindTx) Decision(id string, rev int) (bus.Record, bool, error) {
	if t.s.hide > 0 {
		t.s.hide--
		return bus.Record{}, false, nil
	}
	return t.Tx.Decision(id, rev)
}

func TestUniqueConflictFromRacingWriterIsRuleSix(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	winner, err := e.b.RecordAnswer(ctx, duet, answerIn(q))
	if err != nil {
		t.Fatal(err)
	}
	s := e.snap(t, q.ID)
	b := bus.New(&blind{Store: e.d, hide: 1}, e.now, e.l)
	in := answerIn(q)
	in.Text = "use 9090"
	_, err = b.RecordAnswer(ctx, duet, in)
	if be := errWithCode(t, err, bus.CodeAlreadyDecided); be.Winner == nil || !reflectEqual(*be.Winner, winner) {
		t.Errorf("different answer: Winner = %+v, want %+v", be.Winner, winner)
	}
	e.unchanged(t, "lost race", s)
	b = bus.New(&blind{Store: e.d, hide: 1}, e.now, e.l)
	if again, err := b.RecordAnswer(ctx, duet, answerIn(q)); err != nil || !reflectEqual(again, winner) {
		t.Errorf("identical answer = %+v, %v; want the winner's record", again, err)
	}
	e.unchanged(t, "idempotent after a lost race", s)
	b = bus.New(&blind{Store: e.d, hide: 5}, e.now, e.l)
	_, err = b.RecordAnswer(ctx, duet, in)
	wantCode(t, err, bus.CodeInternal)
	e.unchanged(t, "a read that keeps missing the winner", s)
}
