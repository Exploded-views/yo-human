package bus_test

import (
	"context"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

var cinode = node("ci-1", "crew")

// pageAll reads every page after 0 with no wait and returns how often each seq came back and how
// many pages had events. It fails the test on a page over 100 events or a Next that is not the
// last seq returned.
func pageAll(t *testing.T, e *env, p bus.Principal) (seen map[int64]int, pages int) {
	t.Helper()
	seen = map[int64]int{}
	var since int64
	for {
		u := e.answers(t, p, since, 0)
		if len(u.Events) == 0 {
			if u.Next != since {
				t.Errorf("an empty page moved Next from %d to %d", since, u.Next)
			}
			return seen, pages
		}
		if len(u.Events) > 100 {
			t.Fatalf("a page of %d events, want at most 100", len(u.Events))
		}
		for _, ev := range u.Events {
			seen[ev.Seq]++
		}
		if last := u.Events[len(u.Events)-1].Seq; u.Next != last {
			t.Errorf("Next = %d, want the last seq returned, %d", u.Next, last)
		}
		since = u.Next
		pages++
	}
}

func TestAnswersSinceCursorNoLoss(t *testing.T) {
	e := newEnv(t)
	for i := range 250 {
		e.post(t, duet, toCrew("h"+string(rune('a'+i/26))+string(rune('a'+i%26))))
	}
	seen, pages := pageAll(t, e, cinode)
	if len(seen) != 250 || pages != 3 {
		t.Errorf("saw %d distinct events in %d pages, want 250 in 3", len(seen), pages)
	}
	for seq, n := range seen {
		if n != 1 {
			t.Errorf("event %d returned %d times", seq, n)
		}
	}
}

// nodeActivity makes the node's own events: a post, a reply, a revision and a status push.
func nodeActivity(t *testing.T, e *env) bus.Item {
	t.Helper()
	ctx := context.Background()
	mine := e.post(t, cinode, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	if _, err := e.b.Reply(ctx, cinode, mine.ID, "from me", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	rev := draft(bus.KindQuestion, "q", bus.UrgencyDigest)
	rev.Title = "changed"
	e.post(t, cinode, rev)
	if err := e.b.PushStatus(ctx, cinode, validStatus("crew")); err != nil {
		t.Fatal(err)
	}
	return mine
}

func TestAnswersExcludesWhatTheNodeCaused(t *testing.T) {
	e := newEnv(t)
	nodeActivity(t, e)
	u := e.answers(t, cinode, 0, 0)
	if len(u.Events) != 0 {
		t.Fatalf("the node's own events came back: %+v", u.Events)
	}
	if u.Next == 0 {
		t.Error("Next stayed at 0: a poller would read its own events again forever")
	}
}

func TestAnswersReturnsWhatOthersDid(t *testing.T) {
	e := newEnv(t)
	mine := nodeActivity(t, e)
	next := e.answers(t, cinode, 0, 0).Next
	if _, err := e.b.Reply(context.Background(), duet, mine.ID, "from duet", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	other := e.post(t, node("ci-2", "crew"), draft(bus.KindQuestion, "q2", bus.UrgencyDigest)).Item
	u := e.answers(t, cinode, next, 0)
	if len(u.Events) != 2 || u.Events[0].Type != bus.EventMessageAdded || u.Events[1].ItemID != other.ID {
		t.Fatalf("events = %+v, want duet's message and the other node's post", u.Events)
	}
	wantMessageUpdate(t, u.Events[0], mine.ID)
}

func wantMessageUpdate(t *testing.T, m bus.Update, itemID string) {
	t.Helper()
	if m.Message == nil || m.Message.Body != "from duet" || m.ItemID != itemID || m.Handle != "q" || m.Revision != 2 || m.Record != nil {
		t.Errorf("message update = %+v", m)
	}
}

func TestAnswersCarryTheRecord(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, cinode, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	cur, _ := e.stored(t, it.ID)
	e.putAnswer(t, cur, "go ahead")
	u := e.answers(t, cinode, 0, 0)
	var got *bus.Update
	for i := range u.Events {
		if u.Events[i].Type == bus.EventAnswerRecorded {
			got = &u.Events[i]
		}
	}
	if got == nil || got.Record == nil || got.Record.Text != "go ahead" || got.Record.Kind != bus.RecordAnswer || got.Handle != "q" || got.Revision != 1 {
		t.Fatalf("answer update = %+v", got)
	}
}

func TestAnswersScopedToSwarm(t *testing.T) {
	e := newEnv(t)
	e.post(t, duet, toCrew("for-crew"))
	e.post(t, duet, func() bus.Draft { d := toCrew("for-client"); d.To = "client"; return d }())
	crew := e.answers(t, cinode, 0, 0)
	if len(crew.Events) != 1 || crew.Events[0].Handle != "for-crew" {
		t.Fatalf("crew sees %+v", crew.Events)
	}
	client := e.answers(t, node("ci-9", "client"), 0, 0)
	if len(client.Events) != 1 || client.Events[0].Handle != "for-client" {
		t.Fatalf("client sees %+v", client.Events)
	}
}

func TestAnswersOnlyForNodes(t *testing.T) {
	e := newEnv(t)
	for _, p := range []bus.Principal{duet, {Kind: bus.PrincipalDesk}, {Kind: bus.PrincipalPasskey}, {Kind: bus.PrincipalExecutor},
		{Kind: bus.PrincipalNode, Name: "x", Swarm: "duet"}, {}} {
		_, err := e.b.Answers(context.Background(), p, 0, 0)
		wantCode(t, err, bus.CodeForbidden)
	}
}
