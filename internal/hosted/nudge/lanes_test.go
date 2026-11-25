package nudge_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

func pageItem(handle string) bus.Draft {
	d := question(handle)
	d.Urgency = bus.UrgencyPage
	return d
}

func TestInterruptWhenGoodNeverPushes(t *testing.T) {
	r := newRig(t)
	for _, h := range []string{"a", "b", "c", "d", "e"} {
		r.post(crewNode, quietItem(h))
	}
	r.tickEvery(10*time.Minute, 3*time.Hour)
	if n := len(r.ch.all()); n != 0 {
		t.Fatalf("%d sends for the interrupt lane", n)
	}
	in, err := r.b.Inbox(ctxb(), duet, 0)
	if err != nil || len(in.Interrupt) != 5 {
		t.Errorf("Interrupt group = %d (%v), want all five", len(in.Interrupt), err)
	}
}

func TestPageNeverWaits(t *testing.T) {
	r := newRig(t)
	held := r.post(crewNode, quietItem("held"))
	r.duetSays(held, "first")
	r.post(crewNode, question("n1"))
	r.tick() // a nudge and Duet's message go out
	r.advance(time.Minute)
	r.duetSays(held, "held by the cap")
	r.post(crewNode, pageItem("down"))
	r.post(crewNode, question("n2"))
	r.tick()
	var texts []string
	for _, m := range r.ch.delivered() {
		texts = append(texts, m.Body)
	}
	if got := strings.Join(texts, "|"); !strings.Contains(got, "1 page for Claude") || strings.Contains(got, "held by the cap") || strings.Contains(got, "2 new") {
		t.Errorf("delivered %q, want the page alone in the minute after a nudge", got)
	}
}

func TestPageBoundedPerItemAndSwarm(t *testing.T) {
	r := newRig(t)
	d := pageItem("a")
	a := r.post(crewNode, d)
	r.tick() // minute 0: the first page, at once
	r.advance(time.Minute)
	r.post(crewNode, pageItem("b"))
	r.advance(time.Minute)
	r.post(crewNode, pageItem("c"))
	r.advance(time.Minute)
	d.Title = "Another page on a"
	r.post(crewNode, d) // the same item again, minute 3
	r.tick()
	wantBodies(t, r.pushes(), "1 page for Claude")
	r.advance(7 * time.Minute) // minute 10: the swarm's spacing has passed
	r.tick()
	wantBodies(t, r.pushes(), "1 page for Claude", "2 pages for Claude")
	r.advance(49 * time.Minute)
	r.tick() // minute 59
	r.advance(time.Minute)
	r.tick() // minute 60: the item's window is over
	wantBodies(t, r.pushes(), "1 page for Claude", "2 pages for Claude", "1 page for Claude")
	if n := len(r.ledger(a.ID)); n != 2 {
		t.Errorf("%d ledger rows for the paged item, want 2", n)
	}
}

func TestRepostEvery30mNudgesHourly(t *testing.T) {
	r := newRig(t)
	d := question("a")
	for i := range 7 { // minutes 0 to 180
		d.Title = "Which port? " + string(rune('a'+i))
		r.post(crewNode, d)
		r.tick()
		r.advance(30 * time.Minute)
	}
	var at []time.Duration
	for _, a := range r.ch.all() {
		at = append(at, a.at.Sub(t0))
	}
	want := []time.Duration{0, time.Hour, 2 * time.Hour, 3 * time.Hour}
	if len(at) != len(want) {
		t.Fatalf("nudges at %v, want %v", at, want)
	}
	for i := range want {
		if at[i] != want[i] {
			t.Fatalf("nudges at %v, want %v", at, want)
		}
	}
}

func TestDeferredItemNudgedWhenWindowOpens(t *testing.T) {
	r := newRig(t)
	th := r.duetThread("t1")
	r.pushed(th.ID, t0) // minute 0
	r.advance(20 * time.Minute)
	r.reply(crewNode, th.ID, "an answer", bus.SeverityNormal, bus.UrgencyDigest)
	r.tick()
	r.advance(39 * time.Minute)
	r.tick() // minute 59
	if n := len(r.ch.all()); n != 0 {
		t.Fatalf("%d sends before the item's window is over", n)
	}
	r.advance(time.Minute)
	r.tick() // minute 60, with no new event
	got := r.ch.delivered()
	if len(got) != 1 || !strings.HasPrefix(got[0].Body, "1 new") {
		t.Fatalf("delivered %+v, want one nudge counting the deferred item", got)
	}
	r.advance(time.Hour)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d sends, want the deferred row cleared by its nudge", n)
	}
}

func TestNoZeroNudge(t *testing.T) {
	r := newRig(t)
	a, b := r.duetThread("t1"), r.duetThread("t2")
	r.pushed(a.ID, t0)
	r.pushed(b.ID, t0)
	r.advance(20 * time.Minute)
	r.reply(crewNode, a.ID, "x", bus.SeverityNormal, bus.UrgencyDigest)
	r.reply(crewNode, b.ID, "y", bus.SeverityNormal, bus.UrgencyDigest)
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Errorf("%d sends with every eligible item held, want none", n)
	}
}

func TestNudgeSkipsRecentlyPushedItem(t *testing.T) {
	r := newRig(t)
	held, free := r.duetThread("held"), r.duetThread("free")
	r.pushed(held.ID, t0)
	r.advance(20 * time.Minute)
	r.reply(crewNode, held.ID, "x", bus.SeverityNormal, bus.UrgencyDigest)
	r.reply(crewNode, free.ID, "y", bus.SeverityNormal, bus.UrgencyDigest)
	r.tick()
	if got := r.ch.delivered(); len(got) != 1 || !strings.HasPrefix(got[0].Body, "1 new") {
		t.Fatalf("delivered %+v, want a nudge counting only the free item", got)
	}
	r.advance(40 * time.Minute)
	r.tick() // the window is open again (30 min) and the held item's hour is over
	if got := r.ch.delivered(); len(got) != 2 || !strings.HasPrefix(got[1].Body, "1 new") {
		t.Errorf("delivered %+v, want a second nudge counting the held item", got)
	}
}

func TestDuetRepliesAndItsOwnThreadNeverNudge(t *testing.T) {
	r := newRig(t)
	th := r.duetThread("t1")
	r.reply(duet, th.ID, "a question", bus.SeverityNormal, bus.UrgencyDigest)
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Errorf("%d sends for Duet's own message", n)
	}
	r.reply(crewNode, th.ID, "low", bus.SeverityLow, bus.UrgencyDigest)
	r.reply(crewNode, th.ID, "later", bus.SeverityNormal, bus.UrgencyInterruptWhenGood)
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Errorf("%d sends for a low reply and an interrupt-lane reply", n)
	}
	r.reply(crewNode, th.ID, "now", bus.SeverityNormal, bus.UrgencyDigest)
	r.tick()
	if got := r.ch.delivered(); len(got) != 1 || got[0].Level != notify.LevelActive {
		t.Errorf("delivered %+v, want one nudge", got)
	}
}
