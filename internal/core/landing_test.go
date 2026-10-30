package core_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/core"
)

var pdCaps = map[string]channel.Capabilities{"pd": {ReachesHuman: true, NativeDedup: true, Resolve: true, Schedule: true}}

// landLate sends "page" to pd, resolves while that send is in flight, then lets it land with rcpt.
func landLate(t *testing.T, rcpt channel.Receipt) *env {
	t.Helper()
	e := newEnv(t, incCfg(0, "pd"), pdCaps)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fakes["pd"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		once.Do(func() { close(started) })
		<-release
		return rcpt, nil
	}
	done := make(chan error)
	go func() {
		_, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
		done <- err
	}()
	<-started
	e.resolve("k", "")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDeliveredSendLandingAfterResolveIsResolved(t *testing.T) {
	e := landLate(t, channel.Receipt{Instance: "pd", ProviderID: "PD-FAKE-1"})
	if got := e.fakes["pd"].Resolved; len(got) != 1 || got[0] != "k" {
		t.Errorf("Resolved %v; the alert that landed after the resolve must be closed", got)
	}
	if sentCount(e, "pd") != 1 {
		t.Errorf("sent %d; a resolve-capable channel is closed, not sent a note", sentCount(e, "pd"))
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d", q, in, d)
	}
}

func TestScheduledSendLandingAfterResolveIsCancelled(t *testing.T) {
	want := channel.Receipt{Instance: "pd", ProviderID: "PD-FAKE-1", Scheduled: true}
	e := landLate(t, want)
	if c := e.fakes["pd"].Cancelled; len(c) != 1 || c[0] != want {
		t.Errorf("Cancelled %+v, want %+v", c, want)
	}
	if len(e.fakes["pd"].Resolved) != 0 {
		t.Errorf("Resolved %v; nothing was delivered", e.fakes["pd"].Resolved)
	}
}

// oldLanding sends "old" (generation 1) to n and holds it in flight while the key is resolved and
// reopened as generation 2 (its own send fails, so it stays queued). If closeAgain, generation 2 is
// resolved too. Then the old send lands with rcpt.
func oldLanding(t *testing.T, rcpt channel.Receipt, closeAgain bool) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"n": {ReachesHuman: true, Schedule: true}}
	e := newEnv(t, incCfg(0, "n"), caps)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fakes["n"].SendFunc = func(_ context.Context, m channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		if m.Title == "old" {
			once.Do(func() { close(started) })
			<-release
			return rcpt, nil
		}
		if m.Title == "new" {
			return channel.Receipt{}, channel.Transient(errors.New("down"))
		}
		return channel.Receipt{Instance: "n"}, nil // a resolve note
	}
	done := make(chan struct{})
	go func() { e.say("page", "k", "old"); close(done) }()
	wait(t, started)
	e.resolve("k", "")
	e.say("page", "k", "new")
	if closeAgain {
		e.resolve("k", "")
	}
	close(release)
	wait(t, done)
	return e
}

func cancelledIDs(e *env) map[string]int {
	out := map[string]int{}
	for _, c := range e.fakes["n"].Cancelled {
		out[c.ProviderID]++
	}
	return out
}

func notes(e *env) int {
	n := 0
	for _, m := range e.fakes["n"].Sent {
		if m.Resolved {
			n++
		}
	}
	return n
}

var heldOld = channel.Receipt{Instance: "n", ProviderID: "SCHED-OLD", Scheduled: true}
var sentOld = channel.Receipt{Instance: "n", ProviderID: "SENT-OLD"}

// An older generation's held send was superseded the moment its key was resolved, whatever state the
// incident is in when it lands: it is cancelled, and never recorded on the current generation.
func TestOldGenerationHeldSendLandingIsCancelledWhileTheNewOneIsOpen(t *testing.T) {
	e := oldLanding(t, heldOld, false)
	if got := cancelledIDs(e); got["SCHED-OLD"] != 1 {
		t.Errorf("Cancelled %v; the old held page must be cancelled when it lands", got)
	}
	if inc := e.incident("k"); !inc.Open || len(inc.Reached) != 0 {
		t.Errorf("incident %+v; the old page is not the new incident's", inc)
	}
	e.resolve("k", "")
	if got := cancelledIDs(e); got["SCHED-OLD"] != 1 {
		t.Errorf("Cancelled %v after the final resolve; it must not fire", got)
	}
}

func TestOldGenerationHeldSendLandingIsCancelledWhileTheNewOneIsClosed(t *testing.T) {
	e := oldLanding(t, heldOld, true)
	if got := cancelledIDs(e); got["SCHED-OLD"] != 1 {
		t.Errorf("Cancelled %v; the old held page must be cancelled when it lands", got)
	}
}

// The human saw an older generation's delivered page; the current generation's resolve tells them.
func TestOldGenerationDeliveredLandingIsNotedOnTheNewResolve(t *testing.T) {
	e := oldLanding(t, sentOld, false)
	if inc := e.incident("k"); !inc.Open || len(inc.Reached) != 1 || len(inc.Reached[0].Delivered) == 0 {
		t.Fatalf("incident %+v; want the delivery recorded on the open generation", inc)
	}
	e.resolve("k", "")
	if notes(e) != 1 {
		t.Errorf("notes %d; the final resolve must tell the channel", notes(e))
	}
}

func TestOldGenerationDeliveredLandingIsNotedAtOnceWhileTheNewOneIsClosed(t *testing.T) {
	e := oldLanding(t, sentOld, true)
	if notes(e) != 1 {
		t.Errorf("notes %d; the late delivery needs a resolve note", notes(e))
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d", q, in, d)
	}
}

// quietN has one channel that can schedule, and holds each level in quiet hours (13:00 to 14:00) or
// not as asked. The clock is at 13:30.
func quietN(t *testing.T, holdLow, holdPage bool) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"n": {ReachesHuman: true, Schedule: true}}
	cfg := quietCfg("n")
	cfg.Levels[0].HoldInQuietHours, cfg.Levels[1].HoldInQuietHours = holdLow, holdPage
	e := newEnv(t, cfg, caps)
	e.clk.Advance(90 * time.Minute)
	e.fakes["n"].SendFunc = func(_ context.Context, _ channel.Message, s channel.SendSpec) (channel.Receipt, error) {
		if !s.DeliverAt.IsZero() {
			return channel.Receipt{Instance: "n", ProviderID: "SCHED-FAKE", Scheduled: true}, nil
		}
		return channel.Receipt{Instance: "n", ProviderID: "NOW-FAKE"}, nil
	}
	return e
}

// Scheduled, then delivered on the same instance: the held page must not fire after the resolve.
func TestResolveCancelsAHeldPageAfterALouderOneWasDelivered(t *testing.T) {
	e := quietN(t, true, false)
	if r := e.say("low", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("low: %q", r.Status)
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered {
		t.Fatalf("page: %q", r.Status)
	}
	e.resolve("k", "fixed")
	want := channel.Receipt{Instance: "n", ProviderID: "SCHED-FAKE", Scheduled: true}
	if c := e.fakes["n"].Cancelled; len(c) != 1 || c[0] != want {
		t.Errorf("Cancelled %+v, want the held page", c)
	}
	if got := e.fakes["n"].Sent; len(got) != 3 || !got[2].Resolved {
		t.Errorf("sent %d; want the two pages and a note, the page was delivered", len(got))
	}
}

// Delivered, then scheduled: cancel the held one and tell the channel it was resolved.
func TestResolveCancelsAndNotesWhenDeliveredThenScheduled(t *testing.T) {
	e := quietN(t, false, true)
	if r := e.say("low", "k", "t"); r.Status != core.StatusDelivered {
		t.Fatalf("low: %q", r.Status)
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("page: %q", r.Status)
	}
	e.resolve("k", "fixed")
	want := channel.Receipt{Instance: "n", ProviderID: "SCHED-FAKE", Scheduled: true}
	if c := e.fakes["n"].Cancelled; len(c) != 1 || c[0] != want {
		t.Errorf("Cancelled %+v, want the held page", c)
	}
	if got := e.fakes["n"].Sent; len(got) != 3 || !got[2].Resolved {
		t.Errorf("sent %d; want two pages and a note", len(got))
	}
}

// Every held send on an instance is cancelled, not just the latest or the first.
func TestResolveCancelsEveryHeldSendOnAnInstance(t *testing.T) {
	e := quietN(t, true, true)
	n := 0
	e.fakes["n"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		n++
		return channel.Receipt{Instance: "n", ProviderID: fmt.Sprintf("SCHED-FAKE-%d", n), Scheduled: true}, nil
	}
	e.say("low", "k", "t")
	e.say("page", "k", "t")
	e.resolve("k", "")
	got := map[string]bool{}
	for _, c := range e.fakes["n"].Cancelled {
		got[c.ProviderID] = true
	}
	if len(got) != 2 || !got["SCHED-FAKE-1"] || !got["SCHED-FAKE-2"] {
		t.Errorf("Cancelled %+v; want both held sends", e.fakes["n"].Cancelled)
	}
}

// expiredHeld has a held page of an incident that has since expired (clock 13:50, held until 14:00).
func expiredHeld(t *testing.T) *env {
	t.Helper()
	e := quietN(t, true, true) // 13:30; both levels hold until 14:00
	e.cfg.IncidentTTL = 10 * time.Minute
	n := 0
	e.fakes["n"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		n++
		return channel.Receipt{Instance: "n", ProviderID: fmt.Sprintf("SCHED-FAKE-%d", n), Scheduled: true}, nil
	}
	e.say("page", "k", "t")
	e.clk.Advance(20 * time.Minute)
	return e
}

// A generation that expires while its sends are still held by a channel is replaced, Reached and
// all. Its held sends must be cancelled then, or one fires after the new generation is resolved.
func TestExpiredGenerationsHeldSendIsCancelled(t *testing.T) {
	e := expiredHeld(t)
	e.say("page", "k", "t")
	if inc := e.incident("k"); inc.Generation != 2 {
		t.Fatalf("generation %d, want 2", inc.Generation)
	}
	if got := cancelledIDs(e); got["SCHED-FAKE-1"] != 1 {
		t.Errorf("Cancelled %v; the expired generation's held page must be cancelled when it is replaced", got)
	}
	e.resolve("k", "")
	if got := cancelledIDs(e); got["SCHED-FAKE-1"] != 1 || got["SCHED-FAKE-2"] != 1 {
		t.Errorf("Cancelled %v; want each held page cancelled once", got)
	}
}

func TestExpiredGenerationsCancelFailureIsReported(t *testing.T) {
	e := expiredHeld(t)
	boom := errors.New("provider down")
	p := newWith(e, map[string]channel.Channel{"n": failing{Fake: e.fakes["n"], cancelErr: boom}})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if err != nil || len(r.Warnings) != 1 || !cancelFailure(r.Warnings[0], "n", boom) || r.Status != core.StatusHeld {
		t.Errorf("err %v warnings %v status %q; the new page still goes out, and the failed cancel is a warning", err, r.Warnings, r.Status)
	}
}

func TestExpiredGenerationsChannelGoneIsReported(t *testing.T) {
	e := expiredHeld(t)
	p := newWith(e, map[string]channel.Channel{"other": channeltest.NewFake(human)})
	if r, _ := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"}); len(r.Warnings) == 0 {
		t.Error("a held send on a channel no longer configured went unreported")
	}
}

func TestExpiredGenerationsEveryHeldSendIsCancelled(t *testing.T) {
	e := quietN(t, true, true)
	e.cfg.IncidentTTL = 10 * time.Minute
	n := 0
	e.fakes["n"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		n++
		return channel.Receipt{Instance: "n", ProviderID: fmt.Sprintf("SCHED-FAKE-%d", n), Scheduled: true}, nil
	}
	e.say("low", "k", "t")
	e.clk.Advance(time.Minute)
	e.say("page", "k", "t") // raised: a second held send on the same instance
	e.clk.Advance(20 * time.Minute)
	e.say("page", "k", "t")
	if got := cancelledIDs(e); got["SCHED-FAKE-1"] != 1 || got["SCHED-FAKE-2"] != 1 {
		t.Errorf("Cancelled %v; both held pages of the expired generation must be cancelled", got)
	}
}
