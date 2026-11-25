package nudge_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A blip fails the nudge and Duet's post once. A minute later the nudge goes out (a 2xx), and
// the cap holds the Duet post for the hour after it. The post waits by design: every send since
// the blip succeeded, so delivery is healthy.
func TestAHeldRowThatFailedOnceInABlipIsNotAnOutage(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, question("h1"))
	r.duetSays(it, "I will look")
	r.ch.script(statusErr(503), statusErr(503))
	r.tick() // t0: the blip fails both sends
	if got := r.rows()[0].Attempts; got != 1 {
		t.Fatalf("the row has %d attempts after the blip, want 1", got)
	}
	r.advance(time.Minute)
	r.tick() // the nudge goes out; the cap holds the post
	if len(r.ch.delivered()) != 1 {
		t.Fatalf("delivered %q, want only the nudge", bodies(r.ch.delivered()))
	}
	r.tickEvery(time.Minute, 15*time.Minute)
	if ds := r.delivery(); ds != nil {
		t.Fatalf("delivery = %+v at t+16m with the channel healthy, want nil", ds)
	}
}

// A 2xx from before the row's last attempt does not make it healthy: the channel failed it after.
func TestARowTriedAfterTheLast2xxStillCountsAsStuck(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("h2"))
	r.ch.script(nil) // a 2xx for the test push below
	r.queue("test", it, "t", "ping")
	r.tick()
	id := r.queue("escalation", it, "e", "escalated")
	r.ch.setDefault(statusErr(503))
	r.advance(time.Minute)
	r.tick() // the escalation fails once
	r.ch.setDefault(statusErr(503))
	r.tickEvery(time.Minute, 15*time.Minute)
	if r.row(id).Attempts == 0 {
		t.Fatal("the escalation was never tried")
	}
	if ds := r.delivery(); ds == nil || ds.Pending == 0 {
		t.Fatalf("delivery = %+v, want an outage with a pending row", ds)
	}
}

// The same blip for a page: it fails once, a second page for its item goes out alone while it is
// in backoff, and the per-item page hour then holds the first. It waits by design, and every send
// since the blip succeeded, so delivery is healthy from t0+10m until the hold ends.
func TestAHeldPageThatFailedOnceInABlipIsNotAnOutage(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("h3"))
	first := r.queue(bus.OutboxPage, it, "page", "first page")
	r.ch.script(statusErr(503))
	r.tick() // t0: the blip fails the first page
	if got := r.row(first).Attempts; got != 1 {
		t.Fatalf("the page has %d attempts after the blip, want 1", got)
	}
	r.advance(10 * time.Second)
	r.queue(bus.OutboxPage, it, "page", "second page")
	r.tick() // the second page goes out alone; the first is in backoff
	wantBodies(t, r.ch.delivered(), "second page")
	r.advance(10*time.Minute - 10*time.Second)
	for r.clk.now().Before(t0.Add(time.Hour)) {
		r.tick()
		if ds := r.delivery(); ds != nil {
			t.Fatalf("delivery = %+v at t0+%v with the channel healthy, want nil", ds, r.clk.now().Sub(t0))
		}
		r.advance(time.Minute)
	}
	if got := r.row(first).State; got != bus.OutboxPending {
		t.Fatalf("the held page is %s, want pending: a held row is never dropped", got)
	}
}

// A Duet post and a page that the cap never holds back off by doubling: 30 s, 1 m, 2 m, 4 m, 5 m.
func TestADuetPostAndAPageBackOffByDoubling(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute}
	for _, kind := range []bus.OutboxKind{bus.OutboxDuetPost, bus.OutboxPage} {
		t.Run(string(kind), func(t *testing.T) {
			r := newRig(t)
			it := r.post(crewNode, quietItem("h4"))
			id := r.queue(kind, it, "t", "body")
			r.ch.setDefault(statusErr(503))
			var got []time.Duration
			for range want {
				r.tick()
				wait := r.row(id).NextAt.Sub(r.clk.now())
				got = append(got, wait)
				r.advance(wait)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("waits = %v, want %v", got, want)
				}
			}
		})
	}
}
