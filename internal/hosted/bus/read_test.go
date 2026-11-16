package bus_test

import (
	"context"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

var fiveKinds = []bus.Kind{bus.KindApproval, bus.KindQuestion, bus.KindBlocker, bus.KindFinished, bus.KindFailed}

// postFiveKinds posts one item of each of the five kinds from a crew node and returns their ids.
func postFiveKinds(t *testing.T, e *env) map[bus.Kind]string {
	t.Helper()
	ids := map[bus.Kind]string{}
	for _, k := range fiveKinds {
		d := draft(k, "h-"+string(k), bus.UrgencyDigest)
		if k == bus.KindApproval {
			d = mergeApproval("h-approval")
		}
		ids[k] = e.post(t, node("ci-1", "crew"), d).Item.ID
	}
	return ids
}

func TestInboxGroupsAndExcludesDuetPosts(t *testing.T) {
	e := newEnv(t)
	want := postFiveKinds(t, e)
	e.post(t, node("ci-1", "crew"), draft(bus.KindNotice, "h-notice", bus.UrgencyDigest)) // a notice is not in the inbox
	e.post(t, duet, draft(bus.KindQuestion, "h-duet", bus.UrgencyDigest))                 // Duet's own post to the owner
	e.post(t, duet, toCrew("h-crew"))                                                     // Duet's post to a crew
	other := e.post(t, node("ci-9", "client"), draft(bus.KindQuestion, "h-other", bus.UrgencyDigest)).Item.ID

	in := e.inbox(t, 0)
	want2 := map[bus.Kind][]string{}
	for k, id := range want {
		want2[k] = []string{id}
	}
	want2[bus.KindQuestion] = append(want2[bus.KindQuestion], other)
	for k, w := range want2 {
		same(t, string(k)+" group", ids(in.Groups[k]), w)
	}
	if len(in.Groups) != 5 {
		t.Errorf("groups = %d, want the five spec kinds", len(in.Groups))
	}
	same(t, "interrupt", ids(in.Interrupt), []string{})
}

func TestInboxSummaryFields(t *testing.T) {
	e := newEnv(t)
	e.post(t, node("ci-1", "crew"), draft(bus.KindQuestion, "h-question", bus.UrgencyDigest))
	s := e.inbox(t, 0).Groups[bus.KindQuestion][0]
	if s.Handle != "h-question" || s.From != "ci-1" || s.Swarm != "crew" || s.Title != "Title h-question" || s.Summary != "Summary h-question" ||
		s.Revision != 1 || s.Severity != bus.SeverityNormal || s.Created.IsZero() || s.Kind != bus.KindQuestion {
		t.Errorf("summary = %+v", s)
	}
}

func TestInboxInterruptLaneIsItsOwnGroup(t *testing.T) {
	e := newEnv(t)
	n := node("ci-1", "crew")
	iwg := e.post(t, n, draft(bus.KindQuestion, "wait-ok", bus.UrgencyInterruptWhenGood)).Item.ID
	pg := e.post(t, n, draft(bus.KindBlocker, "now", bus.UrgencyPage)).Item.ID
	e.post(t, n, draft(bus.KindNotice, "note", bus.UrgencyInterruptWhenGood)) // a notice is in no group, whatever its lane
	in := e.inbox(t, 0)
	same(t, "interrupt", ids(in.Interrupt), []string{iwg})
	if in.Interrupt[0].Urgency != bus.UrgencyInterruptWhenGood || in.Interrupt[0].Kind != bus.KindQuestion {
		t.Errorf("interrupt summary = %+v: it must say what it is", in.Interrupt[0])
	}
	same(t, "questions", ids(in.Groups[bus.KindQuestion]), []string{})
	same(t, "blockers", ids(in.Groups[bus.KindBlocker]), []string{pg})
}

func TestInboxIsReadOnly(t *testing.T) {
	e := newEnv(t)
	e.post(t, node("ci-1", "crew"), draft(bus.KindQuestion, "q", bus.UrgencyInterruptWhenGood))
	before := e.events(t)
	e.inbox(t, 0)
	e.inbox(t, 0)
	same(t, "events", e.events(t), before)
}

func TestInboxSinceAndNext(t *testing.T) {
	e := newEnv(t)
	n := node("ci-1", "crew")
	a := e.post(t, n, draft(bus.KindQuestion, "a", bus.UrgencyDigest)).Item
	in := e.inbox(t, 0)
	if in.Next != a.LastSeq {
		t.Errorf("Next = %d, want %d", in.Next, a.LastSeq)
	}
	same(t, "nothing new", ids(e.inbox(t, in.Next).Groups[bus.KindQuestion]), []string{})
	if got := e.inbox(t, in.Next).Next; got != in.Next {
		t.Errorf("an empty read moved Next to %d, want %d", got, in.Next)
	}
	b := e.post(t, n, draft(bus.KindQuestion, "b", bus.UrgencyDigest)).Item
	in2 := e.inbox(t, in.Next)
	same(t, "only the new one", ids(in2.Groups[bus.KindQuestion]), []string{b.ID})
	if in2.Next != b.LastSeq {
		t.Errorf("Next = %d, want %d", in2.Next, b.LastSeq)
	}
}

func TestInboxForbiddenForNode(t *testing.T) {
	e := newEnv(t)
	for _, p := range []bus.Principal{node("ci-1", "crew"), {Kind: bus.PrincipalDesk, Name: "desk"}, {Kind: bus.PrincipalPasskey},
		{Kind: bus.PrincipalExecutor}, {}} {
		_, err := e.b.Inbox(context.Background(), p, 0)
		wantCode(t, err, bus.CodeForbidden)
		wantCode(t, e.b.Ack(context.Background(), p, "itm_x"), bus.CodeForbidden)
	}
}

func TestAckHidesUntilNewActivity(t *testing.T) {
	e := newEnv(t)
	n := node("ci-1", "crew")
	it := e.post(t, n, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	if err := e.b.Ack(context.Background(), duet, it.ID); err != nil {
		t.Fatal(err)
	}
	same(t, "after ack", ids(e.inbox(t, 0).Groups[bus.KindQuestion]), []string{})
	got, _ := e.stored(t, it.ID)
	if got.AckedSeq != got.LastSeq || got.AckedSeq == 0 {
		t.Errorf("AckedSeq = %d, LastSeq = %d: ack must set one to the other", got.AckedSeq, got.LastSeq)
	}
	if _, err := e.b.Reply(context.Background(), n, it.ID, "more", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	same(t, "after new activity", ids(e.inbox(t, 0).Groups[bus.KindQuestion]), []string{it.ID})
}

func TestAckUnknownItemIsNotFound(t *testing.T) {
	e := newEnv(t)
	wantCode(t, e.b.Ack(context.Background(), duet, "itm_nope"), bus.CodeNotFound)
}

func TestInboxThreadsListDuetItemsWithNewMessagesFromOthers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	quiet := e.post(t, duet, toCrew("quiet")).Item
	talk := e.post(t, duet, toCrew("talk")).Item
	mine := e.post(t, duet, toCrew("mine")).Item
	if _, err := e.b.Reply(ctx, node("ci-1", "crew"), talk.ID, "an answer", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.Reply(ctx, duet, mine.ID, "my own note", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	in := e.inbox(t, 0)
	same(t, "threads", ids(in.Threads), []string{talk.ID})
	_ = quiet
	same(t, "duet's items are not in a group", ids(in.Groups[bus.KindQuestion]), []string{})
	if err := e.b.Ack(ctx, duet, talk.ID); err != nil {
		t.Fatal(err)
	}
	same(t, "acked", ids(e.inbox(t, 0).Threads), []string{})
	if _, err := e.b.Reply(ctx, node("ci-1", "crew"), talk.ID, "and more", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	same(t, "back", ids(e.inbox(t, 0).Threads), []string{talk.ID})
	same(t, "since the last", ids(e.inbox(t, e.inbox(t, 0).Next).Threads), []string{})
}

func TestItemIncludesThread(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	n := node("ci-1", "crew")
	it := e.post(t, n, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	for _, r := range []struct {
		p    bus.Principal
		body string
	}{{n, "first"}, {duet, "second"}} {
		if _, err := e.b.Reply(ctx, r.p, it.ID, r.body, bus.SeverityNormal, bus.UrgencyDigest); err != nil {
			t.Fatal(err)
		}
	}
	v, err := e.b.Item(ctx, duet, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision != nil || len(v.Messages) != 2 || v.Messages[0].Body != "first" || v.Messages[1].Body != "second" || v.Item.ID != it.ID {
		t.Errorf("view = %+v", v)
	}
}

func TestItemShowsOnlyTheCurrentRevisionsDecision(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	n := node("ci-1", "crew")
	it := e.post(t, n, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	cur, _ := e.stored(t, it.ID)
	e.putAnswer(t, cur, "yes")
	v, err := e.b.Item(ctx, n, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision == nil || v.Decision.Text != "yes" || v.Decision.Revision != 1 {
		t.Errorf("decision = %+v, want the answer for revision 1", v.Decision)
	}
	rev := draft(bus.KindQuestion, "q", bus.UrgencyDigest)
	rev.Title = "A changed title"
	if r := e.post(t, n, rev); r.Outcome != bus.OutcomeRevised {
		t.Fatalf("outcome = %s", r.Outcome)
	}
	v, _ = e.b.Item(ctx, n, it.ID)
	if v.Decision != nil || v.Item.Revision != 2 {
		t.Errorf("after a revision the answer to revision 1 must not show: %+v, revision %d", v.Decision, v.Item.Revision)
	}
}

func TestItemCrossSwarmIsIndistinguishableFromMissing(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	_, errOther := e.b.Item(context.Background(), node("ci-9", "client"), it.ID)
	_, errMissing := e.b.Item(context.Background(), node("ci-9", "client"), "itm_missing")
	a, b := errWithCode(t, errOther, bus.CodeNotFound), errWithCode(t, errMissing, bus.CodeNotFound)
	same(t, "error", *a, *b)
	if _, err := e.b.Item(context.Background(), bus.Principal{}, it.ID); err == nil {
		t.Error("a zero principal read an item")
	}
}
