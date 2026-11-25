package nudge_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
	"github.com/Exploded-views/yo-human/internal/hosted/nudge"
)

func TestNudgeConfigRefusesLoudLevels(t *testing.T) {
	ok := nudge.Config{Window: time.Minute, MinSeverity: bus.SeverityNormal, Level: notify.LevelActive}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a valid config: %v", err)
	}
	for _, c := range []struct {
		name   string
		mutate func(*nudge.Config)
	}{
		{"critical", func(c *nudge.Config) { c.Level = notify.LevelCritical }},
		{"time-sensitive", func(c *nudge.Config) { c.Level = notify.LevelTimeSensitive }},
		{"no level", func(c *nudge.Config) { c.Level = "" }},
		{"no window", func(c *nudge.Config) { c.Window = 0 }},
		{"no severity", func(c *nudge.Config) { c.MinSeverity = "" }},
		{"unknown severity", func(c *nudge.Config) { c.MinSeverity = "urgent" }},
	} {
		cfg := ok
		c.mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", c.name, cfg)
		}
	}
	for _, l := range []notify.Level{notify.LevelPassive, notify.LevelActive} {
		cfg := ok
		cfg.Level = l
		if err := cfg.Validate(); err != nil {
			t.Errorf("level %s: %v", l, err)
		}
	}
}

func TestWorkerWithALoudConfigSendsNothing(t *testing.T) {
	r := newRig(t)
	r.cfg.Level = notify.LevelCritical
	r.restart()
	r.post(crewNode, question("q1"))
	if err := r.w.Tick(context.Background()); err == nil {
		t.Error("Tick accepted a critical nudge level")
	}
	if n := len(r.ch.all()); n != 0 {
		t.Errorf("%d sends from a worker with a loud level", n)
	}
}

func TestOneNudgeForBurst(t *testing.T) {
	r := newRig(t)
	for i := range 5 {
		r.post(crewNode, question("q"+string(rune('a'+i))))
		r.advance(10 * time.Second)
	}
	r.tick()
	got := r.ch.delivered()
	wantBodies(t, got, "5 new, 5 waiting for Claude")
	if m := got[0]; m.Title != "yo-human" || m.Group != "yo-human" || m.Level != notify.LevelActive || m.ID != "" {
		t.Errorf("message = %+v, want title and group yo-human, level active, no id", m)
	}
	r.tick()
	r.advance(time.Minute)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d sends after more ticks with nothing new", n)
	}
}

func TestNudgeCountsOnlyNew(t *testing.T) {
	r := newRig(t)
	for _, h := range []string{"a", "b", "c"} {
		r.post(crewNode, question(h))
	}
	r.tick()
	r.advance(31 * time.Minute)
	r.post(crewNode, question("d"))
	r.tick()
	wantBodies(t, r.ch.delivered(), "3 new, 3 waiting for Claude", "1 new, 4 waiting for Claude")
}

func TestNudgeCountsAnItemOnceWhateverItsEvents(t *testing.T) {
	r := newRig(t)
	d := question("a")
	r.post(crewNode, d)
	d.Title = "Which port now?"
	r.post(crewNode, d) // revised: a second event for the same item
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
}

func TestWindowCollapses(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.tick()
	r.advance(10 * time.Minute)
	r.post(crewNode, question("b"))
	r.tick()
	if n := len(r.ch.delivered()); n != 1 {
		t.Fatalf("%d sends 10 minutes after a nudge, want still 1", n)
	}
	r.advance(19*time.Minute + 59*time.Second)
	r.tick()
	if n := len(r.ch.delivered()); n != 1 {
		t.Fatalf("%d sends one second before the window ends, want 1", n)
	}
	r.advance(time.Second)
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude", "1 new, 2 waiting for Claude")
}

func TestLowSeverityAndDuetToCrewDontNudge(t *testing.T) {
	r := newRig(t)
	low := question("low")
	low.Severity = bus.SeverityLow
	r.post(crewNode, low)
	r.duetThread("to-crew")
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Fatalf("%d sends for a low item and Duet's own post to a crew", n)
	}
	high := question("high")
	high.Severity = bus.SeverityCritical // at or above the minimum counts
	r.post(crewNode, high)
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 2 waiting for Claude")
}

func TestOnlyDigestItemsAreCountedAndDuetPostsGoTheirOwnWay(t *testing.T) {
	r := newRig(t)
	good := question("good")
	good.Urgency = bus.UrgencyInterruptWhenGood
	r.post(crewNode, good)
	r.post(duet, question("duets")) // Duet to the owner: its own message, never a nudge's count
	r.tick()
	for _, m := range r.ch.delivered() {
		if strings.Contains(m.Body, "new") {
			t.Errorf("a nudge went out: %+v", m)
		}
	}
}

func TestFailedSendRetriesNextTick(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.ch.script(statusErr(500))
	r.tick()
	if n := len(r.ch.delivered()); n != 0 {
		t.Fatalf("%d delivered after a 500", n)
	}
	r.advance(time.Second) // far inside the window: the failed nudge never started one
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
	if n := len(r.ch.all()); n != 2 {
		t.Errorf("%d attempts, want the failure and its retry", n)
	}
	r.advance(time.Minute)
	r.tick()
	if n := len(r.ch.all()); n != 2 {
		t.Errorf("%d attempts after a success, want no more", n)
	}
}

func TestRefusedNudgeNotRetried(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.ch.script(statusErr(400))
	r.tick()
	r.advance(time.Minute)
	r.tick()
	r.advance(time.Hour)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Fatalf("%d attempts, want the refused nudge tried once", n)
	}
	if ds := r.delivery(); ds == nil || ds.Failed != 1 || ds.Status != 400 || ds.Broken {
		t.Errorf("delivery = %+v, want one refused message with status 400", ds)
	}
	r.advance(24 * time.Hour)
	if ds := r.delivery(); ds != nil {
		t.Errorf("delivery after 24 h = %+v, want nil", ds)
	}
}

func TestRefusedNudgeIsNotACrash(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.ch.script(statusErr(400))
	r.tick()
	r.restart()
	r.advance(time.Hour)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d attempts after a restart, want the refused nudge not re-sent", n)
	}
}

func TestRestartDoesNotResend(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.tick()
	r.restart()
	r.tick()
	r.advance(time.Hour)
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
}

func TestNudgeCarriesNoItemText(t *testing.T) {
	r := newRig(t)
	d := question("HANDLE-SECRET-FAKE")
	d.Title, d.Summary = "TITLE-SECRET-FAKE", "SUMMARY-SECRET-FAKE"
	r.post(crewNode, d)
	r.tick()
	for _, a := range r.ch.all() {
		all := a.m.ID + a.m.Title + a.m.Body + a.m.Group
		for _, s := range []string{"HANDLE-SECRET", "TITLE-SECRET", "SUMMARY-SECRET", "ob-", "itm_"} {
			if strings.Contains(all, s) {
				t.Errorf("the nudge %+v carries %q", a.m, s)
			}
		}
	}
	if len(r.ch.all()) != 1 {
		t.Fatal("no nudge went out")
	}
}

func TestWorkerIgnoresDeskEvents(t *testing.T) {
	r := newRig(t)
	r.do(func(x bus.Tx) error {
		for _, ty := range []bus.EventType{bus.EventDeskSigninStarted, bus.EventDeskMinted} {
			if _, err := x.AppendEvent(bus.Event{Type: ty, Created: t0, Starter: "mac"}); err != nil {
				return err
			}
		}
		return nil
	})
	r.tick()
	r.advance(time.Hour)
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Errorf("%d sends for desk events", n)
	}
}

func TestRunTicksOnStart(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.w.Run(ctx); close(done) }()
	waitFor(t, func() bool { return len(r.ch.delivered()) == 1 })
	cancel()
	<-done
}

func TestRunTicksOnKickAndOnTheInterval(t *testing.T) {
	r := newRig(t)
	r.w.SetInterval(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.w.Run(ctx); close(done) }()
	r.post(crewNode, question("a"))
	waitFor(t, func() bool { return len(r.ch.delivered()) == 1 }) // the interval alone
	r.advance(time.Hour)
	r.w.SetInterval(time.Hour) // not seen by the running loop; the kick is what wakes it
	r.post(crewNode, question("b"))
	r.w.Kick()
	waitFor(t, func() bool { return len(r.ch.delivered()) == 2 })
	cancel()
	<-done
}

func TestTicksNeverOverlap(t *testing.T) {
	r := newRig(t)
	r.post(crewNode, question("a"))
	r.ch.gate, r.ch.entered = make(chan struct{}), make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.w.Run(ctx); close(done) }()
	select { // the first tick is inside its send
	case <-r.ch.entered:
	case <-time.After(2 * time.Second):
		close(r.ch.gate)
		cancel()
		t.Fatal("the first tick never reached its send")
	}
	r.advance(time.Hour)
	r.post(crewNode, question("b"))
	r.w.Kick()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = r.w.Tick(ctx) }() // a direct Tick waits for the running one too
	select {
	case <-r.ch.entered:
		t.Fatal("a second send began while the first tick was still running")
	case <-time.After(150 * time.Millisecond):
	}
	close(r.ch.gate)
	cancel()
	<-done
	wg.Wait()
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
