package nudge_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestSameItemNotPushedTwiceWithin60m(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "first")
	r.tick() // minute 0: out at once
	r.advance(10 * time.Minute)
	r.duetSays(it, "second")
	r.tick()
	r.advance(10 * time.Minute)
	r.duetSays(it, "third")
	r.tick()
	r.advance(39 * time.Minute)
	r.tick() // minute 59
	wantBodies(t, r.pushes(), "first")
	r.advance(time.Minute)
	r.tick() // minute 60: the two held posts, as one
	wantBodies(t, r.pushes(), "first", "third (+1 more)")
	r.advance(time.Hour)
	r.tick()
	if n := len(r.pushes()); n != 2 {
		t.Errorf("%d pushes, want 2", n)
	}
	st := r.states()
	if st[1] != bus.OutboxSent || st[2] != bus.OutboxSent || st[3] != bus.OutboxSuperseded {
		t.Errorf("states = %v, want the first sent, the lowest of the held two sent and the other superseded", st)
	}
}

func TestNewRevisionWaitsForItemWindow(t *testing.T) {
	r := newRig(t)
	d := quietItem("a")
	it := r.post(crewNode, d)
	r.duetSays(it, "at revision one")
	r.tick()
	r.advance(5 * time.Minute)
	d.Title = "Which port now?"
	it = r.post(crewNode, d)
	r.advance(5 * time.Minute)
	r.duetSays(it, "at revision two") // minute 10
	r.tick()
	r.advance(49 * time.Minute)
	r.tick()
	wantBodies(t, r.pushes(), "at revision one")
	r.advance(time.Minute)
	r.tick()
	wantBodies(t, r.pushes(), "at revision one", "at revision two")
}

func TestHeldDuetPostSupersededOnDecision(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "first")
	r.tick()
	r.advance(10 * time.Minute)
	r.duetSays(it, "held")
	r.tick()
	if _, err := r.b.RecordAnswer(ctxb(), duet, bus.AnswerInput{ItemID: it.ID, Revision: 1, Text: "use 8080", HumanTS: t0}); err != nil {
		t.Fatal(err)
	}
	r.advance(time.Hour)
	r.tick()
	wantBodies(t, r.pushes(), "first")
	if st := r.states()[2]; st != bus.OutboxSuperseded {
		t.Errorf("the held post is %s, want superseded", st)
	}
}

func TestHeldDuetPostSupersededOnRepost(t *testing.T) {
	r := newRig(t)
	d := quietItem("a")
	it := r.post(crewNode, d)
	r.duetSays(it, "first")
	r.tick()
	r.advance(10 * time.Minute)
	r.duetSays(it, "held")
	r.tick()
	d.Title = "Which port now?"
	r.post(crewNode, d)
	r.advance(time.Hour)
	r.tick()
	wantBodies(t, r.pushes(), "first")
	if st := r.states()[2]; st != bus.OutboxSuperseded {
		t.Errorf("the held post is %s, want superseded", st)
	}
}

func TestEscalationBypassesCap(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "first")
	r.tick()
	r.advance(30 * time.Minute)
	r.queue(bus.OutboxEscalation, it, "yo-human", "escalated")
	r.tick()
	wantBodies(t, r.pushes(), "first", "escalated")
	if n := len(r.ledger(it.ID)); n != 2 {
		t.Errorf("%d ledger rows, want the escalation recorded too", n)
	}
}

func TestCoalescedPushHasStableID(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "first")
	r.tick()
	r.advance(10 * time.Minute)
	r.duetSays(it, "second")
	r.advance(10 * time.Minute)
	r.duetSays(it, "third")
	r.tick()
	r.advance(40 * time.Minute) // minute 60
	r.ch.script(statusErr(500))
	r.tick()
	r.advance(30 * time.Second)
	r.tick()
	var got []string
	all := r.ch.all()
	if len(all) > 0 {
		all = all[1:]
	}
	for _, a := range all {
		got = append(got, a.m.ID+" "+a.m.Body)
	}
	want := idOf(2) + " third (+1 more)"
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Errorf("attempts = %q, want the same id and text twice: %q", got, want)
	}
}

func TestProbeRespectsCapAndSupersede(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.pushed(it.ID, t0.Add(-40*time.Minute))
	r.duetSays(it, "held by the cap")
	r.do(func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: 401}) })
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Fatalf("%d sends: the probe sent a row the cap holds", n)
	}
	if got := r.row(1).NextAt; !got.Equal(t0.Add(20 * time.Minute)) {
		t.Errorf("next_at = %v, want the push plus 60 minutes", got)
	}
	if !r.flag().Raised {
		t.Error("the flag was cleared without a 2xx")
	}
}

func ctxb() context.Context { return context.Background() }
