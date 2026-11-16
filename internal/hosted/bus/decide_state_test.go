package bus_test

import (
	"slices"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func (e *env) countType(t *testing.T, ty bus.EventType) (n int) {
	t.Helper()
	for _, ev := range e.events(t) {
		if ev.Type == ty {
			n++
		}
	}
	return n
}

func TestSecondDifferentApprovalRejectedWithWinner(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	first, err := e.b.RecordApproval(ctx, passkey, approveIn(it))
	if err != nil {
		t.Fatal(err)
	}
	s := e.snap(t, it.ID)
	reject := approveIn(it)
	reject.Decision = bus.DecisionReject
	reworded := approveIn(it)
	reworded.Text = "yes, merge it now"
	for name, c := range map[string]struct {
		p  bus.Principal
		in bus.ApprovalInput
	}{
		"other decision":   {passkey, reject},
		"other text":       {passkey, reworded},
		"other credential": {bus.Principal{Kind: bus.PrincipalPasskey, Name: "tablet"}, approveIn(it)},
	} {
		_, err := e.b.RecordApproval(ctx, c.p, c.in)
		be := errWithCode(t, err, bus.CodeAlreadyDecided)
		if be.Winner == nil || !reflectEqual(*be.Winner, first) {
			t.Errorf("%s: Winner = %+v, want %+v", name, be.Winner, first)
		}
	}
	e.unchanged(t, "second approvals", s)
}

func TestIdenticalRetryIsIdempotent(t *testing.T) {
	e := newEnv(t)
	a, q := e.approvalItem(t), e.questionItem(t)
	for name, call := range map[string]func() (bus.Record, error){
		"approval": func() (bus.Record, error) { return e.b.RecordApproval(ctx, passkey, approveIn(a)) },
		"answer":   func() (bus.Record, error) { return e.b.RecordAnswer(ctx, duet, answerIn(q)) },
	} {
		first, err := call()
		if err != nil {
			t.Fatal(err)
		}
		s := e.snap(t, first.ItemID)
		again, err := call()
		if err != nil || !reflectEqual(again, first) {
			t.Errorf("%s retry = %+v, %v; want %+v", name, again, err, first)
		}
		e.unchanged(t, name+" retry", s)
	}
}

func TestRevisionVoidsThenReapprove(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	it := e.approvalItem(t)
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(it)); err != nil {
		t.Fatal(err)
	}
	d := mergeApproval("m7")
	d.Title = "Merge PR 7 (rebased)"
	r2 := e.post(t, p, d).Item
	if r2.Revision != 2 || r2.State != bus.StateOpen {
		t.Fatalf("repost = revision %d, %s", r2.Revision, r2.State)
	}
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(r2)); err != nil {
		t.Fatal(err)
	}
	c, _ := e.d.Check(ctx)
	if c.Decisions != 2 {
		t.Errorf("decisions = %d, want both revisions kept", c.Decisions)
	}
	got, _ := e.stored(t, it.ID)
	rec1, _ := e.effective(t, it.ID, 1)
	rec2, _ := e.effective(t, it.ID, 2)
	same(t, "state and records", []any{got.State, got.Revision, rec1.Revision, rec2.Revision}, []any{bus.StateDecided, 2, 1, 2})
}

func TestHumanAnswerSupersedesAgent(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	agent, err := e.b.RecordAnswer(ctx, duet, answerIn(q))
	if err != nil || agent.ActorKind != bus.ActorAgent {
		t.Fatal(agent, err)
	}
	in := answerIn(q)
	in.Text = "use 9090"
	human, err := e.b.RecordAnswer(ctx, deskP, in)
	if err != nil || human.ActorKind != bus.ActorHuman || human.Text != "use 9090" {
		t.Fatalf("desk answer = %+v, %v", human, err)
	}
	eff, _ := e.effective(t, q.ID, 1)
	c, _ := e.d.Check(ctx)
	got, _ := e.stored(t, q.ID)
	same(t, "effective, rows, superseded events, state, last seq", []any{eff.ActorKind, c.Decisions, e.countType(t, bus.EventAnswerSuperseded), got.State},
		[]any{bus.ActorHuman, 2, 1, bus.StateDecided})
	evs := e.events(t)
	if last := evs[len(evs)-1]; got.LastSeq != last.Seq || last.Swarm != "crew" || last.ItemID != q.ID {
		t.Errorf("last seq %d, last event %+v", got.LastSeq, last)
	}
}

func TestRetriesAfterSupersede(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	if _, err := e.b.RecordAnswer(ctx, duet, answerIn(q)); err != nil {
		t.Fatal(err)
	}
	in := answerIn(q)
	in.Text = "use 9090"
	human, err := e.b.RecordAnswer(ctx, deskP, in)
	if err != nil {
		t.Fatal(err)
	}
	// Duet's retry is now refused, the desk's retry is idempotent.
	_, err = e.b.RecordAnswer(ctx, duet, answerIn(q))
	if be := errWithCode(t, err, bus.CodeAlreadyDecided); be.Winner == nil || be.Winner.ActorKind != bus.ActorHuman {
		t.Errorf("agent retry Winner = %+v", be.Winner)
	}
	if again, err := e.b.RecordAnswer(ctx, deskP, in); err != nil || !reflectEqual(again, human) {
		t.Errorf("desk retry = %+v, %v", again, err)
	}
}

func TestAgentAfterHumanRefused(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	human, err := e.b.RecordAnswer(ctx, deskP, answerIn(q))
	if err != nil {
		t.Fatal(err)
	}
	s := e.snap(t, q.ID)
	in := answerIn(q)
	in.Text = "something else"
	_, err = e.b.RecordAnswer(ctx, duet, in)
	if be := errWithCode(t, err, bus.CodeAlreadyDecided); be.Winner == nil || !reflectEqual(*be.Winner, human) {
		t.Errorf("Winner = %+v, want %+v", be.Winner, human)
	}
	_, err = e.b.RecordAnswer(ctx, deskP, in) // a second, different human answer: first wins
	wantCode(t, err, bus.CodeAlreadyDecided)
	e.unchanged(t, "refused answers", s)
	if n := e.countType(t, bus.EventAnswerSuperseded); n != 0 {
		t.Errorf("superseded events = %d", n)
	}
}

func TestRecordedEvents(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	if _, err := e.b.RecordAnswer(ctx, duet, answerIn(q)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(a)); err != nil {
		t.Fatal(err)
	}
	want := []bus.EventType{bus.EventItemPosted, bus.EventItemPosted, bus.EventAnswerRecorded, bus.EventApprovalRecorded}
	if got := types(e.events(t)); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func (e *env) relays(t *testing.T, id string) (out []bus.Relay) {
	t.Helper()
	if err := e.d.Tx(ctx, func(x bus.Tx) (err error) { out, err = x.Relays(id); return err }); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) inOpenList(t *testing.T, id string) bool {
	t.Helper()
	found := false
	err := e.d.Tx(ctx, func(x bus.Tx) error {
		items, err := x.ListOpenForHuman()
		for _, o := range items {
			found = found || o.ID == id
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func relayIn(it bus.Item) bus.RelayInput {
	return bus.RelayInput{ItemID: it.ID, Revision: it.Revision, Text: "approved, go ahead", HumanTS: t0}
}

func TestRelayNeverResolves(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	r, err := e.b.RelayWords(ctx, node("ci-1", "crew"), relayIn(it))
	if err != nil || r.Text != "approved, go ahead" || r.ID == "" || r.ItemID != it.ID || r.Revision != 1 || r.By != "ci-1" {
		t.Fatalf("relay = %+v, %v", r, err)
	}
	got, _ := e.stored(t, it.ID)
	c, _ := e.d.Check(ctx)
	_, decided := e.effective(t, it.ID, 1)
	same(t, "state, decided, decisions, relays, words_relayed events", []any{got.State, decided, c.Decisions, c.Relays, e.countType(t, bus.EventWordsRelayed)},
		[]any{bus.StateOpen, false, 0, 1, 1})
	stored := e.relays(t, it.ID)
	same(t, "stored relay texts, open list", []any{len(stored), e.inOpenList(t, it.ID)}, []any{1, true})
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(it)); err != nil {
		t.Errorf("passkey approval after a relay: %v", err)
	}
}

func TestRelayFromNodesAndDuet(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	for _, p := range []bus.Principal{node("ci-1", "crew"), node("ci-2", "crew"), duet} {
		r, err := e.b.RelayWords(ctx, p, relayIn(it))
		if err != nil || r.By != p.Name {
			t.Errorf("%s relay = %+v, %v", p.Name, r, err)
		}
	}
	s := e.snap(t, it.ID)
	for _, p := range []bus.Principal{passkey, deskP, {Kind: bus.PrincipalExecutor, Name: "exec"}, {Kind: bus.PrincipalNode, Name: "x", Swarm: "duet"}, {Kind: bus.PrincipalNode, Swarm: "crew"}} {
		_, err := e.b.RelayWords(ctx, p, relayIn(it))
		wantCode(t, err, bus.CodeForbidden)
	}
	_, err := e.b.RelayWords(ctx, node("w-1", "client"), relayIn(it)) // a node of another swarm
	wantCode(t, err, bus.CodeNotFound)
	_, err = e.b.RelayWords(ctx, duet, bus.RelayInput{ItemID: "itm_nope", Revision: 1, Text: "x"})
	wantCode(t, err, bus.CodeNotFound)
	stale := relayIn(it)
	stale.Revision = 3
	_, err = e.b.RelayWords(ctx, duet, stale)
	wantCode(t, err, bus.CodeStaleRevision)
	for _, text := range []string{"", "  ", "bad \xff"} {
		in := relayIn(it)
		in.Text = text
		_, err = e.b.RelayWords(ctx, duet, in)
		wantCode(t, err, bus.CodeInvalid)
	}
	same(t, "relays after the refusals", e.snap(t, it.ID).c.Relays, s.c.Relays)
	e.unchanged(t, "refused relays", s)
}

func TestWalledRelayOnlyGoOrHold(t *testing.T) {
	e := newEnvWalled(t, "client")
	d := typed("12")
	d.Kind = bus.KindQuestion
	it := e.post(t, walledNode, d).Item
	in := relayIn(it)
	_, err := e.b.RelayWords(ctx, walledNode, in)
	wantCode(t, err, bus.CodeInvalid)
	in.Text = "hold sl:12"
	if _, err := e.b.RelayWords(ctx, walledNode, in); err != nil {
		t.Errorf("hold sl:12: %v", err)
	}
}

func TestDeskCannotPost(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	s := e.snap(t, it.ID)
	_, err := e.b.Post(ctx, deskP, notice("d1"))
	wantCode(t, err, bus.CodeForbidden)
	_, err = e.b.Reply(ctx, deskP, it.ID, "hello", bus.SeverityNormal, bus.UrgencyDigest)
	wantCode(t, err, bus.CodeForbidden)
	// Escalate is Task 11b's; its own test will cover it there.
	e.unchanged(t, "desk writes", s)
}
