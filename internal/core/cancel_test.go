package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

var schedules = channel.Capabilities{ReachesHuman: true, Schedule: true}

func phoneAt(server string) config.ChannelInstance {
	return config.ChannelInstance{Type: "fake", Settings: map[string]any{"server": server}}
}

// scheduledEnv holds "page" in an always-on quiet window on a channel that schedules, so a notify
// leaves the phone with a send it will deliver later unless it is cancelled.
func scheduledEnv(t *testing.T) *env {
	t.Helper()
	lv := page(retry5, "phone")
	lv.HoldInQuietHours = true
	cfg := levelsCfg(time.Second, lv)
	cfg.QuietHours = config.QuietHours{Location: time.UTC, Windows: []config.Window{{From: 0, To: 0}}}
	cfg.Channels = map[string]config.ChannelInstance{"phone": phoneAt("https://one.example")}
	e := newEnv(t, cfg, map[string]channel.Capabilities{"phone": schedules})
	e.fakes["phone"].SendFunc = func(_ context.Context, _ channel.Message, sp channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: sp.Instance, ProviderID: "sched-1", Scheduled: !sp.DeliverAt.IsZero()}, nil
	}
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "disk", DedupKey: "disk"})
	if err != nil || r.Status != core.StatusHeld {
		t.Fatalf("Notify = %+v, %v; want held with the provider holding the send", r, err)
	}
	return e
}

// with is a pipeline over the environment's store and clock under cfg, with only chans built.
func (e *env) with(cfg *config.Config, chans map[string]channel.Channel) *core.Pipeline {
	caps := map[string]channel.Capabilities{"phone": schedules}
	return core.New(core.Deps{Config: cfg, Store: e.st, Channels: chans, Caps: caps, Now: e.clk.Now, NewID: newIDs()})
}

// idSeq numbers the ids of every pipeline newIDs makes, so two pipelines over one store never hand
// out the same queue key, as the binary's ULIDs never do.
var idSeq atomic.Int64

func newIDs() func() string {
	return func() string { return fmt.Sprintf("Z%03d", idSeq.Add(1)) }
}

func (e *env) configWith(inst config.ChannelInstance) *config.Config {
	cfg := *e.cfg
	cfg.Channels = map[string]config.ChannelInstance{"phone": inst}
	return &cfg
}

type storeIncident = store.Incident

var errStop = errors.New("stop")

func (e *env) notifyKey(level, key string) core.Result {
	e.t.Helper()
	r, err := e.p.Notify(context.Background(), channel.Message{Level: level, Title: "t", DedupKey: key})
	if err != nil {
		e.t.Fatalf("Notify: %v", err)
	}
	return r
}

func TestAnIncidentRecordsWhereEachChannelPointed(t *testing.T) {
	e := scheduledEnv(t)
	var got string
	_ = e.st.WithIncidents(func(m map[string]*storeIncident) error {
		if i := m["disk"]; i != nil && len(i.Reached) == 1 && len(i.Reached[0].Scheduled) == 1 {
			got = i.Reached[0].Scheduled[0].Fingerprint
		}
		return errStop
	})
	if want := core.FingerprintOf(phoneAt("https://one.example")); got != want {
		t.Errorf("reached fingerprint = %q, want %q", got, want)
	}
}

// Resolve past a channel that cannot be built cancels the incident's scheduled sends later, through
// the same destination, and never pages.
func TestResolveQueuesTheCancelOfAScheduledSendItCannotReach(t *testing.T) {
	e := scheduledEnv(t)
	gone := e.with(e.cfg, map[string]channel.Channel{}) // the phone cannot be built
	r, err := gone.Resolve(context.Background(), "disk", "")
	p, ok := problem.As(err)
	if !ok || p.Kind != problem.Config || !strings.Contains(p.What, "scheduled send on phone could not be cancelled yet") {
		t.Fatalf("Resolve error = %v; want a Config problem saying the scheduled send on phone could not be cancelled yet", err)
	}
	if r.Status != core.StatusResolved || len(e.fakes["phone"].Cancelled) != 0 {
		t.Fatalf("result %+v, cancelled %v; want resolved with nothing cancelled yet", r, e.fakes["phone"].Cancelled)
	}
	parkedAsNotBuilt(t, gone)
	built := e.with(e.cfg, map[string]channel.Channel{"phone": e.fakes["phone"]}) // the phone can be built again, unchanged
	if _, err := built.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkCancelledOnce(t, e, built)
}

func parkedAsNotBuilt(t *testing.T, gone *core.Pipeline) {
	t.Helper()
	fr, ferr := gone.Flush(context.Background())
	if ferr != nil || len(fr.Channels) < 1 || !strings.Contains(joinErrs(fr), "not built") {
		t.Fatalf("flush while unbuildable = %+v, %v; want the cancel parked as not built", fr, ferr)
	}
}

func checkCancelledOnce(t *testing.T, e *env, built *core.Pipeline) {
	t.Helper()
	if c := e.fakes["phone"].Cancelled; len(c) != 1 || c[0].ProviderID != "sched-1" || c[0].Instance != "phone" || !c[0].Scheduled {
		t.Fatalf("cancelled = %+v; want the scheduled send cancelled exactly once", c)
	}
	if _, err := built.Flush(context.Background()); err != nil || len(e.fakes["phone"].Cancelled) != 1 {
		t.Errorf("a second flush cancelled again: %v, %d", err, len(e.fakes["phone"].Cancelled))
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d %d %d after the cancel; want empty", q, in, d)
	}
	if len(e.fakes["phone"].Sent) != 1 {
		t.Errorf("phone sent %d messages; only the scheduled one, never a page", len(e.fakes["phone"].Sent))
	}
}

func joinErrs(r core.Result) string {
	var b strings.Builder
	for _, c := range r.Channels {
		b.WriteString(c.Channel + ":" + c.Status + ":" + c.Error + ";")
	}
	return b.String()
}

// The channel removed and then restored identically is the same destination; restored pointing
// elsewhere it is not, and the cancel is never run through it.
func TestAQueuedCancelRunsOnlyThroughTheSameDestination(t *testing.T) {
	e := scheduledEnv(t)
	removed := e.with(&config.Config{Deadline: e.cfg.Deadline, Levels: e.cfg.Levels, QuietHours: e.cfg.QuietHours}, nil)
	if _, err := removed.Resolve(context.Background(), "disk", ""); err == nil {
		t.Fatal("Resolve with the phone removed returned no problem")
	}
	elsewhere := e.with(e.configWith(phoneAt("https://two.example")), map[string]channel.Channel{"phone": e.fakes["phone"]})
	fr, err := elsewhere.Flush(context.Background())
	if err != nil || len(e.fakes["phone"].Cancelled) != 0 || !strings.Contains(joinErrs(fr), "points somewhere else") {
		t.Fatalf("flush with the phone pointing elsewhere = %s, %v, cancelled %v; want it parked", joinErrs(fr), err, e.fakes["phone"].Cancelled)
	}
	same := e.with(e.configWith(phoneAt("https://one.example")), map[string]channel.Channel{"phone": e.fakes["phone"]})
	if _, err := same.Flush(context.Background()); err != nil || len(e.fakes["phone"].Cancelled) != 1 {
		t.Fatalf("flush with the phone restored: %v, cancelled %v; want exactly one cancel", err, e.fakes["phone"].Cancelled)
	}
}

// A cancel entry is valid for its closed generation: a reopened key does not drop it.
func TestAQueuedCancelSurvivesTheIncidentReopening(t *testing.T) {
	e := scheduledEnv(t)
	gone := e.with(e.cfg, map[string]channel.Channel{})
	if _, err := gone.Resolve(context.Background(), "disk", ""); err == nil {
		t.Fatal("no problem")
	}
	e.fakes["phone"].SendFunc = nil
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "disk again", DedupKey: "disk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.with(e.cfg, map[string]channel.Channel{"phone": e.fakes["phone"]}).Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.fakes["phone"].Cancelled) != 1 {
		t.Errorf("cancelled %d; want the old generation's scheduled send cancelled", len(e.fakes["phone"].Cancelled))
	}
}

// The resolve note of a channel that was removed is queued for the destination the incident reached.
func TestAResolveNoteWaitsForTheSameDestination(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "phone"))
	cfg.Channels = map[string]config.ChannelInstance{"phone": phoneAt("https://one.example")}
	e := newEnv(t, cfg, map[string]channel.Capabilities{"phone": human})
	if r := e.notifyKey("page", "disk"); r.Status != core.StatusDelivered {
		t.Fatalf("setup: %+v", r)
	}
	removed := *cfg
	removed.Channels = map[string]config.ChannelInstance{}
	if _, err := e.with(&removed, nil).Resolve(context.Background(), "disk", ""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	n := len(e.fakes["phone"].Sent)
	e.clk.Advance(time.Minute)
	elsewhere := e.configWith(phoneAt("https://two.example"))
	fr, _ := e.with(elsewhere, map[string]channel.Channel{"phone": e.fakes["phone"]}).Flush(context.Background())
	if len(e.fakes["phone"].Sent) != n || !strings.Contains(joinErrs(fr), "points somewhere else") {
		t.Fatalf("the note went to a channel pointing elsewhere: %s", joinErrs(fr))
	}
	e.clk.Advance(time.Minute)
	fr, _ = e.with(e.configWith(phoneAt("https://one.example")), map[string]channel.Channel{"phone": e.fakes["phone"]}).Flush(context.Background())
	sent := e.fakes["phone"].Sent
	if len(sent) != n+1 || !sent[n].Resolved {
		t.Fatalf("after restoring the channel: %s; sent %d, want the resolve note", joinErrs(fr), len(sent)-n)
	}
}

// An entry with no fingerprint is never sent, built or not.
func TestAnEntryForAnUnconfiguredButBuiltChannelIsParked(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "a"))
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human})
	other := *cfg
	other.Channels = map[string]config.ChannelInstance{} // the config in force names no channel
	enqueueRaw(t, e, "X1.a", `{"instance":"a","message":{"level":"page"}}`)
	p := core.New(core.Deps{Config: &other, Store: e.st, Channels: map[string]channel.Channel{"a": e.fakes["a"]},
		Caps: map[string]channel.Capabilities{"a": human}, Now: e.clk.Now, NewID: newIDs()})
	r, err := p.Flush(context.Background())
	if err != nil || len(e.fakes["a"].Sent) != 0 || !strings.Contains(joinErrs(r), "not configured") {
		t.Fatalf("flush = %s, %v, sent %d; want the entry parked, not sent", joinErrs(r), err, len(e.fakes["a"].Sent))
	}
}

// failingCancel is a channel whose cancellations fail.
type failingCancel struct {
	*channeltest.Fake
	kind func(error) error
}

func (f failingCancel) Cancel(context.Context, channel.Receipt) error {
	return f.kind(errors.New("provider refused"))
}

func queuedCancel(t *testing.T) *env {
	t.Helper()
	e := scheduledEnv(t)
	if _, err := e.with(e.cfg, map[string]channel.Channel{}).Resolve(context.Background(), "disk", ""); err == nil {
		t.Fatal("no problem")
	}
	return e
}

func TestAQueuedCancelThatFailsPermanentlyGoesToTheDeadLetter(t *testing.T) {
	e := queuedCancel(t)
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Permanent}})
	fr, err := p.Flush(context.Background())
	if err != nil || !strings.Contains(joinErrs(fr), "dead:provider refused") {
		t.Fatalf("flush = %s, %v; want the cancel dead-lettered", joinErrs(fr), err)
	}
	if q, _, d := e.counts(); q != 0 || d != 1 {
		t.Errorf("queued %d, dead %d; want the cancel kept in the dead letter", q, d)
	}
}

func TestAQueuedCancelThatFailsTransientlyIsRetried(t *testing.T) {
	e := queuedCancel(t)
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Transient}})
	if _, err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q, _, d := e.counts(); q != 1 || d != 0 {
		t.Errorf("queued %d, dead %d; want the cancel kept for a retry", q, d)
	}
	e.clk.Advance(2 * time.Minute)
	if _, err := e.with(e.cfg, map[string]channel.Channel{"phone": e.fakes["phone"]}).Flush(context.Background()); err != nil || len(e.fakes["phone"].Cancelled) != 1 {
		t.Errorf("retry: %v, cancelled %d; want one cancel", err, len(e.fakes["phone"].Cancelled))
	}
}

func TestResolveSaysSoWhenTheCancelCannotBeQueued(t *testing.T) {
	e := scheduledEnv(t)
	lock(t, filepath.Join(e.dir, "queue"))
	r, err := e.with(e.cfg, map[string]channel.Channel{}).Resolve(context.Background(), "disk", "")
	if err == nil || !strings.Contains(joinErrs(r), "phone:dead:its scheduled send could not be queued for cancellation") {
		t.Errorf("result %s, error %v; want the failure reported on the channel", joinErrs(r), err)
	}
}

// clearReachedFingerprints makes the incident look as one recorded before destinations were.
func clearReachedFingerprints(t *testing.T, e *env) {
	t.Helper()
	err := e.st.WithIncidents(func(m map[string]*storeIncident) error {
		for i := range m["disk"].Reached {
			for j := range m["disk"].Reached[i].Scheduled {
				m["disk"].Reached[i].Scheduled[j].Fingerprint = ""
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A send with no recorded destination, or one the channel no longer matches, is never cancelled
// through whatever channel is built now: its cancellation is queued.
func TestAScheduledSendIsNotCancelledThroughAnotherDestination(t *testing.T) {
	cases := map[string]func(e *env) *config.Config{
		"no recorded destination": func(e *env) *config.Config {
			clearReachedFingerprints(t, e)
			cfg := *e.cfg
			cfg.Channels = map[string]config.ChannelInstance{} // built but unconfigured: "" would equal ""
			return &cfg
		},
		"pointing elsewhere": func(e *env) *config.Config { return e.configWith(phoneAt("https://two.example")) },
	}
	for name, mk := range cases {
		e := scheduledEnv(t)
		cfg := mk(e)
		if _, err := e.with(cfg, map[string]channel.Channel{"phone": e.fakes["phone"]}).Resolve(context.Background(), "disk", ""); err == nil {
			t.Errorf("%s: Resolve returned no problem", name)
		}
		if len(e.fakes["phone"].Cancelled) != 0 {
			t.Errorf("%s: cancelled through a channel that is not where the send went", name)
		}
		if q, _, _ := e.counts(); q != 1 {
			t.Errorf("%s: queued %d, want the cancellation queued", name, q)
		}
	}
}

func TestADeliveryAddsItsDestinationToTheRecord(t *testing.T) {
	rs := core.AddReached(nil, core.ChannelResult{Channel: "phone", Status: core.ChannelSent}, "one")
	rs = core.AddReached(rs, core.ChannelResult{Channel: "phone", Status: core.ChannelSent}, "two")
	rs = core.AddReached(rs, core.ChannelResult{Channel: "phone", Status: core.ChannelSent}, "one") // again: kept once
	if len(rs) != 1 || len(rs[0].Delivered) != 2 || rs[0].Delivered[0] != "one" || rs[0].Delivered[1] != "two" {
		t.Errorf("reached = %+v; want one record, delivered through both destinations", rs)
	}
}
