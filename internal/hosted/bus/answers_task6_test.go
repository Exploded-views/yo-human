package bus_test

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// recordUpdates is the updates of u that carry a record, with nothing else.
func recordUpdates(t *testing.T, u bus.Updates) []bus.Update {
	t.Helper()
	var out []bus.Update
	for _, up := range u.Events {
		switch up.Type {
		case bus.EventAnswerRecorded, bus.EventAnswerSuperseded, bus.EventApprovalRecorded:
			out = append(out, up)
		case bus.EventWordsRelayed:
			t.Errorf("a relay came back as an update: %+v", up)
		}
	}
	return out
}

// wantRecord fails unless up is of type ty and carries a record of kind, actor, text and revision.
func wantRecord(t *testing.T, up bus.Update, ty bus.EventType, actor bus.ActorKind, text string, rev int) {
	t.Helper()
	got := []any{up.Type, up.Revision, up.Record != nil}
	if up.Record != nil {
		got = append(got, up.Record.ActorKind, up.Record.Text, up.Record.Revision)
		same(t, "update "+string(ty), got, []any{ty, rev, true, actor, text, rev})
		return
	}
	t.Errorf("update %s has no record: %+v", ty, up)
}

// Every record the real write paths store comes back through Answers on its own event: an agent
// answer, a desk answer that supersedes it, an approval at revision 1, a relay (not returned), then
// a repost to revision 2 and its approval. No test helper writes any of it.
func TestAnswersReadsTask6Writes(t *testing.T) {
	e := newEnv(t)
	q := e.questionItem(t)
	if _, err := e.b.RecordAnswer(ctx, duet, bus.AnswerInput{ItemID: q.ID, Revision: 1, Text: "agent says go", HumanTS: t0}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.RecordAnswer(ctx, deskP, bus.AnswerInput{ItemID: q.ID, Revision: 1, Text: "human says hold", HumanTS: t0}); err != nil {
		t.Fatal(err)
	}
	a := e.approvalItem(t)
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(a)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.RelayWords(ctx, duet, relayIn(a)); err != nil {
		t.Fatal(err)
	}
	d := mergeApproval("m7")
	d.Title = "Merge PR 7, second take"
	if r := e.post(t, cinode, d); r.Outcome != bus.OutcomeRevised {
		t.Fatalf("outcome = %s", r.Outcome)
	}
	a2, _ := e.stored(t, a.ID)
	in := approveIn(a2)
	in.Text = "yes, the second take"
	if _, err := e.b.RecordApproval(ctx, passkey, in); err != nil {
		t.Fatal(err)
	}
	got := recordUpdates(t, e.answers(t, cinode, 0, 0))
	if len(got) != 5 {
		t.Fatalf("%d record updates, want 5: %+v", len(got), got)
	}
	wantRecord(t, got[0], bus.EventAnswerRecorded, bus.ActorAgent, "agent says go", 1)
	wantRecord(t, got[1], bus.EventAnswerRecorded, bus.ActorHuman, "human says hold", 1)
	wantRecord(t, got[2], bus.EventAnswerSuperseded, bus.ActorHuman, "human says hold", 1)
	wantRecord(t, got[3], bus.EventApprovalRecorded, bus.ActorHuman, "yes, merge it", 1)
	wantRecord(t, got[4], bus.EventApprovalRecorded, bus.ActorHuman, "yes, the second take", 2)
}
