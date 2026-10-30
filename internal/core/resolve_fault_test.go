package core_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
)

// newWith is a pipeline over e's store and clock with the given channels.
func newWith(e *env, chans map[string]channel.Channel) *core.Pipeline {
	n := 0
	caps := map[string]channel.Capabilities{}
	for n, c := range chans {
		caps[n] = c.Capabilities()
	}
	return core.New(core.Deps{Config: e.cfg, Store: e.st, Channels: chans, Caps: caps, Now: e.clk.Now,
		NewID: func() string { n++; return fmt.Sprintf("N%03d", n) }}) // distinct ids, as the binary's are: queue keys are built from them
}

func TestResolveStoreFailure(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if err := os.Mkdir(filepath.Join(e.dir, "incidents.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Resolve(context.Background(), "k", ""); err == nil {
		t.Error("an unreadable incident store went unreported")
	}
	if err := os.RemoveAll(filepath.Join(e.dir, "inflight")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Resolve(context.Background(), "k", ""); err == nil {
		t.Error("a failing recovery went unreported")
	}
}

func TestResolveReportsACancelThatFailed(t *testing.T) {
	caps := map[string]channel.Capabilities{"sched": {ReachesHuman: true, Schedule: true}}
	e := newEnv(t, quietCfg("sched"), caps)
	e.clk.Advance(90 * time.Minute)
	e.fakes["sched"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true}, nil
	}
	e.say("page", "k", "t")
	boom := errors.New("provider down")
	p := newWith(e, map[string]channel.Channel{"sched": failing{Fake: e.fakes["sched"], cancelErr: boom}})
	r, err := p.Resolve(context.Background(), "k", "")
	if !cancelFailure(err, "sched", boom) || r.Status != core.StatusResolved {
		t.Fatalf("err = %v status %q; a scheduled page we could not cancel is the human's to know about", err, r.Status)
	}
	if st := statuses(r); st["sched"] == core.ChannelCancelled {
		t.Errorf("channels = %v; a failed cancel is not cancelled", st)
	}
	// A channel that is no longer configured cannot be cancelled either.
	e.say("page", "k2", "t")
	if _, err := newWith(e, map[string]channel.Channel{}).Resolve(context.Background(), "k2", ""); err == nil {
		t.Error("a channel missing from the config went unreported")
	}
}

func TestResolveQueuesANoteThatFailed(t *testing.T) {
	caps := map[string]channel.Capabilities{"pd": {ReachesHuman: true, Resolve: true}}
	e := newEnv(t, incCfg(0, "pd"), caps)
	e.say("page", "k", "t")
	boom := errors.New("provider down")
	p := newWith(e, map[string]channel.Channel{"pd": failing{Fake: e.fakes["pd"], resolveErr: boom}})
	r, err := p.Resolve(context.Background(), "k", "")
	if !isQueued(err) || r.Status != core.StatusResolved || statuses(r)["pd"] != core.ChannelQueued || r.Next == nil {
		t.Fatalf("err = %v, %+v; want resolved with the close queued for a retry, and a next step", err, r)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("queued = %d; the resolve must not be lost", q)
	}
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err != nil || len(e.fakes["pd"].Resolved) != 1 {
		t.Errorf("flush: %v, resolved %v; the retry must close the alert", err, e.fakes["pd"].Resolved)
	}
}

func TestResolveReportsACancelIncidentFailure(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": human, "b": human}
	e := newEnv(t, quietCfg("a", "b"), caps)
	e.clk.Advance(90 * time.Minute)
	e.say("page", "k", "t") // held locally on both
	lock(t, filepath.Join(e.dir, "queue"))
	r, err := e.p.Resolve(context.Background(), "k", "")
	if err == nil || r.Status != core.StatusResolved {
		t.Errorf("err = %v status %q; a queue we could not clean is reported, but the incident is closed", err, r.Status)
	}
}

func TestRequeueFailuresAreReported(t *testing.T) {
	for _, closeIncident := range []bool{false, true} {
		e, dir := faultEnv(t)
		e.cfg.IncidentTTL = time.Hour
		e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
			if closeIncident {
				e.resolve("k", "")
			}
			lock(t, filepath.Join(dir, "queue"))    // Retry cannot write
			lock(t, filepath.Join(dir, "inflight")) // Complete cannot remove
			return channel.Receipt{}, channel.Transient(errors.New("down"))
		}
		_, err := e.p.Notify(context.Background(), channel.Message{Level: "page", DedupKey: "k"})
		if err == nil {
			t.Errorf("closed=%v: a failed requeue went unreported", closeIncident)
		}
	}
}

func TestResolveReportsANoteThatCouldNotBeQueued(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	lock(t, filepath.Join(e.dir, "queue"))
	r, err := e.p.Resolve(context.Background(), "k", "")
	if err == nil || r.Status != core.StatusResolved || sentCount(e, "a") != 1 {
		t.Errorf("err = %v status %q sent %d; want the queue failure reported and no unqueued note sent", err, r.Status, sentCount(e, "a"))
	}
}
