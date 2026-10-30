package core_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

// quietCfg holds both levels from 13:00 to 14:00 UTC; t0 is 12:00.
func quietCfg(names ...string) *config.Config {
	c := incCfg(0, names...)
	for i := range c.Levels {
		c.Levels[i].HoldInQuietHours = true
	}
	c.QuietHours = config.QuietHours{Location: time.UTC, Windows: []config.Window{{From: 13 * 60, To: 14 * 60}}}
	return c
}

// pendingEnv has one incident "k" with a locally held send ("local") and one the channel holds for
// later ("sched"), inside quiet hours.
func pendingEnv(t *testing.T) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"local": human, "sched": {ReachesHuman: true, Schedule: true}}
	e := newEnv(t, quietCfg("local", "sched"), caps)
	e.clk.Advance(90 * time.Minute) // 13:30, inside the window
	e.fakes["sched"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true}, nil
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("setup: %q", r.Status)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("setup: queued = %d, want the locally held send", q)
	}
	return e
}

func TestResolveCancelsPending(t *testing.T) {
	e := pendingEnv(t)
	r := e.resolve("k", "fixed")
	if r.Status != core.StatusResolved || r.ID == "" {
		t.Fatalf("status %q id %q", r.Status, r.ID)
	}
	if q, _, _ := e.counts(); q != 0 {
		t.Errorf("queued = %d; the held send must be removed", q)
	}
	if st := statuses(r); st["local"] != core.ChannelCancelled || st["sched"] != core.ChannelCancelled || len(st) != 2 {
		t.Errorf("channels = %v; want both cancelled", st)
	}
	if inc := e.incident("k"); !e.hasIncident("k") || inc.Open {
		t.Errorf("incident = %+v; want it kept and closed", inc)
	}
}

func TestResolveCancelsWhatAChannelHolds(t *testing.T) {
	e := pendingEnv(t)
	e.resolve("k", "fixed")
	want := channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true}
	if c := e.fakes["sched"].Cancelled; len(c) != 1 || c[0] != want {
		t.Errorf("Cancelled = %+v, want %+v", c, want)
	}
	if sentCount(e, "local") != 0 || sentCount(e, "sched") != 1 {
		t.Errorf("a cancelled send must not be followed by a note: local %d, sched %d", sentCount(e, "local"), sentCount(e, "sched"))
	}
}

func TestResolveNativeAndNotes(t *testing.T) {
	caps := map[string]channel.Capabilities{
		"pd":    {ReachesHuman: true, NativeDedup: true, Resolve: true},
		"ntfy":  human,
		"log":   {},
		"late":  human,
		"mute":  {ReachesHuman: true, Interruptions: map[channel.Interruption]bool{}},
		"quiet": {ReachesHuman: true, Interruptions: map[channel.Interruption]bool{channel.Active: true, channel.Critical: true}},
	}
	e := newEnv(t, quietCfg("pd", "ntfy", "log", "late", "mute", "quiet"), caps)
	e.fakes["late"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "disk full")
	e.clk.Advance(90 * time.Minute) // into the quiet window
	r := e.resolve("k", "freed 40GB")
	if r.Status != core.StatusResolved {
		t.Fatalf("status %q", r.Status)
	}
	if got := e.fakes["pd"].Resolved; len(got) != 1 || got[0] != "k" || sentCount(e, "pd") != 1 {
		t.Errorf("pd: Resolved %v, sent %d; want Resolve(k) and no note message", got, sentCount(e, "pd"))
	}
	checkNote(t, e.fakes["ntfy"], channel.Passive)
	checkNote(t, e.fakes["log"], channel.Passive)
	checkNote(t, e.fakes["mute"], channel.Passive) // lists no interruption at all: still passive
	checkNote(t, e.fakes["quiet"], channel.Active)
	if sentCount(e, "late") != 1 || len(e.fakes["late"].Resolved) != 0 {
		t.Errorf("late (never reached) got %d sends, %v resolves; want only the failed original", sentCount(e, "late"), e.fakes["late"].Resolved)
	}
	if st := statuses(r); st["late"] != core.ChannelCancelled || st["ntfy"] != core.ChannelSent {
		t.Errorf("channels = %v", st)
	}
}

// checkNote wants f to have sent the incident and then one resolve note at the given interruption,
// now, whatever the quiet hours.
func checkNote(t *testing.T, f *channeltest.Fake, want channel.Interruption) {
	t.Helper()
	if len(f.Sent) != 2 {
		t.Errorf("sent %d; want the incident and one note", len(f.Sent))
		return
	}
	note, spec := f.Sent[1], f.Specs[1]
	if !note.Resolved || note.DedupKey != "k" || note.Body != "freed 40GB" || !strings.Contains(note.Title, "disk full") {
		t.Errorf("note = %+v", note)
	}
	if spec.Interruption != want || !spec.DeliverAt.IsZero() {
		t.Errorf("spec = %+v; want %s, now, quiet hours ignored", spec, want)
	}
}

func TestResolveUnknownIsNoop(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if r := e.resolve("nope", ""); r.Status != core.StatusNoop || r.ID == "" || len(r.Channels) != 0 {
		t.Errorf("unknown = %+v", r)
	}
	e.say("page", "k", "t")
	e.resolve("k", "")
	if r := e.resolve("k", ""); r.Status != core.StatusNoop {
		t.Errorf("resolved twice = %q, want noop", r.Status)
	}
	if sentCount(e, "a") != 2 {
		t.Errorf("sent %d; the second resolve must send nothing", sentCount(e, "a"))
	}
}

func TestResolveThenNotifyOpensAFreshIncident(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	e.resolve("k", "")
	if r := e.say("low", "k", "t"); r.Status != core.StatusDelivered {
		t.Errorf("after a resolve = %q, want a fresh send even at a lower level", r.Status)
	}
	if inc := e.incident("k"); !inc.Open || inc.Level != "low" || len(inc.Reached) != 1 {
		t.Errorf("incident = %+v", inc)
	}
}

func TestResolveSendsWhatIsDue(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "other", "u")
	e.fakes["a"].SendFunc = nil
	e.say("page", "k", "t")
	e.clk.Advance(time.Hour)
	e.resolve("k", "")
	if q, _, _ := e.counts(); q != 0 {
		t.Errorf("queued = %d; resolve sends the backlog too", q)
	}
}

// failing wraps a fake so Resolve or Cancel can fail.
type failing struct {
	*channeltest.Fake
	resolveErr, cancelErr error
}

func (f failing) Resolve(ctx context.Context, k string, n channel.Message) error {
	if f.resolveErr != nil {
		return f.resolveErr
	}
	return f.Fake.Resolve(ctx, k, n)
}

func (f failing) Cancel(ctx context.Context, r channel.Receipt) error {
	if f.cancelErr != nil {
		return f.cancelErr
	}
	return f.Fake.Cancel(ctx, r)
}
