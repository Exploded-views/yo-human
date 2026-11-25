package nudge_test

import (
	"testing"
	"time"
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
