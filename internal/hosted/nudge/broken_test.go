package nudge_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestChannelBrokenRaisesFlag(t *testing.T) {
	r := newRig(t)
	a := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "one")
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "two")
	r.ch.script(statusErr(401))
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Fatalf("%d attempts, want the pass to stop at the broken channel", n)
	}
	if row := r.row(a); row.State != bus.OutboxPending || row.Attempts != 0 {
		t.Errorf("row = %+v, want pending and not counted as an attempt", row)
	}
	if f := r.flag(); !f.Raised || f.Status != 401 || !f.Since.Equal(t0) {
		t.Errorf("flag = %+v, want raised at t0 with status 401", f)
	}
	if ds := r.delivery(); ds == nil || !ds.Broken || ds.Status != 401 {
		t.Errorf("delivery = %+v", ds)
	}
}

func TestBrokenChannelIsProbedEveryFiveMinutesAndClearsOnA2xx(t *testing.T) {
	r := newRig(t)
	a := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "one")
	b := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "two")
	r.ch.script(statusErr(401))
	r.tick()
	r.advance(4*time.Minute + 59*time.Second)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Fatalf("%d attempts inside the probe spacing", n)
	}
	r.advance(time.Second) // minute 5: a probe, refused again with a different status
	r.ch.script(statusErr(403))
	r.tick()
	if f := r.flag(); !f.Raised || f.Status != 403 || !f.Since.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("flag = %+v, want re-raised at minute 5 with status 403", f)
	}
	r.advance(5 * time.Minute) // minute 10: the probe is accepted, and the rest goes
	r.tick()
	got := r.ch.delivered()
	if len(got) != 2 || got[0].ID != idOf(a) || got[1].ID != idOf(b) {
		t.Errorf("delivered %+v, want both rows", got)
	}
	if r.flag().Raised || r.delivery() != nil {
		t.Errorf("flag %+v delivery %+v after a 2xx, want both clear", r.flag(), r.delivery())
	}
}

func TestBrokenSendsOnlyTheProbe(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("n1")) // a nudge is due too
	for range 3 {
		r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "x")
	}
	r.do(func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: 401}) })
	r.ch.setDefault(statusErr(401))
	r.tickEvery(time.Minute, 4*time.Minute)
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d attempts in four minutes, want one probe and nothing else", n)
	}
	if all := r.ch.all(); len(all) == 1 && all[0].m.ID != idOf(1) {
		t.Errorf("the probe was %+v, want the oldest outbox row", all[0].m)
	}
}

func TestBrokenFlagReRaisedAfterRestartWithBadKey(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "x")
	r.ch.script(statusErr(401))
	r.tick()
	r.advance(time.Minute)
	r.restart()
	r.ch.setDefault(statusErr(401))
	r.tick() // a new worker probes at once
	if n := len(r.ch.all()); n != 2 {
		t.Fatalf("%d attempts, want the restart's probe", n)
	}
	if f := r.flag(); !f.Raised || !f.Since.Equal(t0.Add(time.Minute)) {
		t.Errorf("flag = %+v, want still raised, with the new time", f)
	}
}

func TestBrokenFlagStaysWhenTheOnlyRowIsSuperseded(t *testing.T) {
	r := newRig(t)
	d := quietItem("a")
	it := r.post(crewNode, d)
	r.duetSays(it, "x")
	r.do(func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: 401}) })
	d.Title = "Which port now?"
	r.post(crewNode, d) // the revision moves on
	r.restart()
	r.tick()
	if st := r.states()[1]; st != bus.OutboxSuperseded {
		t.Errorf("row is %s, want superseded", st)
	}
	if !r.flag().Raised || len(r.ch.all()) != 0 {
		t.Errorf("flag %+v, %d sends: nothing went through, so the flag stays", r.flag(), len(r.ch.all()))
	}
}

func TestBrokenFlagClearsOnFirst2xx(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "x")
	r.do(func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: 401}) })
	r.restart()
	if !r.flag().Raised {
		t.Fatal("the flag did not survive the restart")
	}
	r.tick()
	if r.flag().Raised || len(r.ch.delivered()) != 1 {
		t.Errorf("flag %+v, %d delivered, want the probe's 200 to clear it", r.flag(), len(r.ch.delivered()))
	}
}

func TestOutboxTestClearsBrokenFlag(t *testing.T) {
	r := newRig(t)
	r.do(func(x bus.Tx) error { return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: t0, Status: 401}) })
	r.tickEvery(5*time.Minute, 30*time.Minute)
	if !r.flag().Raised || r.delivery() == nil || len(r.ch.all()) != 0 {
		t.Fatalf("flag %+v after idle ticks, want it still raised with nothing sent", r.flag())
	}
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(statusErr(401))
	r.advance(5 * time.Minute)
	r.tick()
	if !r.flag().Raised {
		t.Fatal("a 401 on the test push cleared the flag")
	}
	r.advance(5 * time.Minute)
	r.tick()
	if r.flag().Raised {
		t.Errorf("flag %+v, want it cleared by the test push's 200", r.flag())
	}
}

func TestRefusedAgesOutAfter24h(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "x")
	r.ch.script(statusErr(400))
	r.tick()
	if ds := r.delivery(); ds == nil || ds.Failed != 1 || ds.Status != 400 {
		t.Fatalf("delivery = %+v, want one refused with status 400", ds)
	}
	r.advance(24 * time.Hour)
	if ds := r.delivery(); ds != nil {
		t.Errorf("delivery after 24 h = %+v, want nil", ds)
	}
}

func TestNudgeBrokenRaisesTheFlagAndRollsBack(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.ch.script(statusErr(401))
	r.tick()
	if f := r.flag(); !f.Raised || f.Status != 401 {
		t.Fatalf("flag = %+v", f)
	}
	r.advance(5 * time.Minute)
	r.tick() // the probe is the nudge itself, state rolled back, so it is due again
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
	if r.flag().Raised {
		t.Error("the flag survived a 2xx")
	}
}
