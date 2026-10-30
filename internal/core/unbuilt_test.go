package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// A command may build only some of the configured channels, and the config in force may not be the
// one an entry was queued under. In both cases the entry stays queued, its attempts untouched, with
// the reason kept: it is never buried and never sent to a destination it was not meant for.

func unbuiltEnv(t *testing.T) (*env, *channeltest.Fake) {
	t.Helper()
	cfg := levelsCfg(time.Second, page(retry5, "b"))
	cfg.Channels = map[string]config.ChannelInstance{"a": {Type: "fake"}, "b": bAt("https://one.example")}
	caps := map[string]channel.Capabilities{"a": human, "b": human}
	e := newEnv(t, cfg, caps)
	e.fakes["b"].SendFunc = failWith(channel.Transient, "down")
	if r := e.notify("page"); statuses(r)["b"] != core.ChannelQueued {
		t.Fatalf("setup: result %+v; want b queued", r)
	}
	e.clk.Advance(2 * time.Minute)
	return e, e.fakes["a"]
}

// flushWith flushes the environment's store under cfg with only the given channels built.
func flushWith(t *testing.T, e *env, cfg *config.Config, chans map[string]channel.Channel) core.Result {
	t.Helper()
	caps := map[string]channel.Capabilities{"a": human, "b": human}
	p := core.New(core.Deps{Config: cfg, Store: e.st, Channels: chans, Caps: caps, Now: e.clk.Now, NewID: func() string { return "X1" }})
	r, err := p.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return r
}

// parked checks that the queue holds exactly one entry for b, still on its one attempt, with a
// reason containing want, and that nothing is dead.
func parked(t *testing.T, e *env, r core.Result, want string) {
	t.Helper()
	if len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelQueued || !strings.Contains(r.Channels[0].Error, want) {
		t.Fatalf("result %+v; want one queued entry whose reason contains %q", r.Channels, want)
	}
	if q, in, dead := e.counts(); q != 1 || in != 0 || dead != 0 {
		t.Errorf("queued %d, inflight %d, dead %d; want 1, 0, 0", q, in, dead)
	}
	keptWithReason(t, e, want)
}

func keptWithReason(t *testing.T, e *env, want string) {
	t.Helper()
	due, err := e.st.Due(e.clk.Now().Add(time.Hour))
	if err != nil || len(due) != 1 || due[0].Attempts != 1 || !strings.Contains(due[0].Reason, want) {
		t.Errorf("queue = %+v, %v; want one entry on attempt 1 with the reason kept", due, err)
	}
}

func TestAnEntryForAChannelNotBuiltStaysQueued(t *testing.T) {
	e, a := unbuiltEnv(t)
	parked(t, e, flushWith(t, e, e.cfg, map[string]channel.Channel{"a": a}), "not built")
}

func TestAnEntryForAChannelNotInTheConfigInForceStaysQueued(t *testing.T) {
	e, a := unbuiltEnv(t)
	cfg := *e.cfg
	cfg.Channels = map[string]config.ChannelInstance{"a": {Type: "fake"}} // another config, with no b
	parked(t, e, flushWith(t, e, &cfg, map[string]channel.Channel{"a": a}), "not configured")
}

// bAt is channel b pointing at a server, with its topic held in a secret.
func bAt(server string) config.ChannelInstance {
	return config.ChannelInstance{Type: "fake", Settings: map[string]any{"server": server, "topic": map[string]any{"secret": "t1"}}}
}

// The same name is not the same destination when its type, its settings or the secrets it names
// differ: another config's b must not receive this config's message.
func TestAnEntryIsNeverSentWhereTheChannelNowPointsElsewhere(t *testing.T) {
	for name, changed := range map[string]config.ChannelInstance{
		"another server": bAt("https://two.example"),
		"another type":   {Type: "other", Settings: bAt("https://one.example").Settings},
		"another secret": {Type: "fake", Settings: map[string]any{"server": "https://one.example", "topic": map[string]any{"secret": "t2"}}},
		"no settings":    {Type: "fake"},
	} {
		e, a := unbuiltEnv(t)
		cfg := *e.cfg
		cfg.Channels = map[string]config.ChannelInstance{"a": {Type: "fake"}, "b": changed}
		e.fakes["b"].SendFunc = nil
		n := len(e.fakes["b"].Sent)
		r := flushWith(t, e, &cfg, map[string]channel.Channel{"a": a, "b": e.fakes["b"]})
		if len(e.fakes["b"].Sent) != n {
			t.Errorf("%s: the entry was sent to a channel that points elsewhere", name)
		}
		parked(t, e, r, "now points somewhere else than when this message was queued")
	}
}

func TestAnEntryIsSentWhenTheChannelIsUnchanged(t *testing.T) {
	e, a := unbuiltEnv(t)
	cfg := *e.cfg
	cfg.Channels = map[string]config.ChannelInstance{"a": {Type: "fake"}, "b": bAt("https://one.example")} // a copy, same settings
	e.fakes["b"].SendFunc = nil
	if r := flushWith(t, e, &cfg, map[string]channel.Channel{"a": a, "b": e.fakes["b"]}); statuses(r)["b"] != core.ChannelSent {
		t.Errorf("result %+v; want sent", r.Channels)
	}
}

// An entry queued before destinations were recorded is not trusted.
func TestAnEntryWithNoFingerprintIsParked(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "b"))
	cfg.Channels = map[string]config.ChannelInstance{"b": bAt("https://one.example")}
	e := newEnv(t, cfg, map[string]channel.Capabilities{"b": human})
	enqueueRaw(t, e, "X1.b", `{"instance":"b","message":{"level":"page"}}`)
	r, err := e.p.Flush(context.Background())
	if err != nil || len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelQueued || !strings.Contains(r.Channels[0].Error, "points somewhere else") {
		t.Fatalf("flush = %+v, %v; want the entry parked", r, err)
	}
	if len(e.fakes["b"].Sent) != 0 {
		t.Error("an entry with no fingerprint was sent")
	}
}

func TestAQueuedEntryRecordsItsDestinationAndConfigPath(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "b"))
	cfg.Channels = map[string]config.ChannelInstance{"b": {Type: "fake"}}
	caps := map[string]channel.Capabilities{"b": human}
	e := newEnv(t, cfg, caps)
	e.fakes["b"].SendFunc = failWith(channel.Transient, "down")
	p := core.New(core.Deps{Config: cfg, Store: e.st, Channels: map[string]channel.Channel{"b": e.fakes["b"]}, Caps: caps,
		Now: e.clk.Now, NewID: func() string { return "X1" }, ConfigPath: "/work/my dir/proj.yaml"})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t"})
	if err != nil || r.Next == nil {
		t.Fatalf("Notify = %+v, %v", r, err)
	}
	if want := "yo-human flush --config '/work/my dir/proj.yaml'"; r.Next.Do != want {
		t.Errorf("next.do = %q, want %q", r.Next.Do, want)
	}
	due, _ := e.st.Due(e.clk.Now().Add(time.Hour))
	var pl struct{ Fingerprint, Config string }
	if len(due) != 1 || json.Unmarshal(due[0].Data, &pl) != nil || len(pl.Fingerprint) != 64 || pl.Config != "/work/my dir/proj.yaml" {
		t.Errorf("queued payload = %s (%v); want a 64-digit destination fingerprint and the config path recorded", dueData(due), pl)
	}
	e2 := newEnv(t, cfg, caps)
	e2.fakes["b"].SendFunc = failWith(channel.Transient, "down")
	if r := e2.notify("page"); r.Next == nil || r.Next.Do != "yo-human flush" {
		t.Errorf("next = %+v without a config path; want plain flush", r.Next)
	}
}

func dueData(due []store.Pending) string {
	if len(due) == 0 {
		return "(nothing queued)"
	}
	return string(due[0].Data)
}

// A planned channel this call could not build is skipped: not sent, not queued (a page for it would
// go out long after the human fixed the config), and counted as not reached.
func TestAPlannedChannelThatIsNotBuiltIsSkipped(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "a", "b"))
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human, "b": human})
	p := core.New(core.Deps{Config: cfg, Store: e.st, Channels: map[string]channel.Channel{"a": e.fakes["a"]},
		Caps: map[string]channel.Capabilities{"a": human, "b": human}, Now: e.clk.Now, NewID: newIDs()})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t"})
	if err != nil || r.Status != core.StatusPartial || statuses(r)["a"] != core.ChannelSent || statuses(r)["b"] != core.ChannelSkipped {
		t.Fatalf("Notify = %+v, %v; want a sent, b skipped, partial", r, err)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d; a skipped channel is not queued", q, in, d)
	}
	p = core.New(core.Deps{Config: cfg, Store: e.st, Channels: map[string]channel.Channel{},
		Caps: map[string]channel.Capabilities{"a": human, "b": human}, Now: e.clk.Now, NewID: newIDs()})
	if r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t"}); err != nil || r.Status != core.StatusFailed || r.Next != nil {
		t.Errorf("none built: %+v, %v; want failed, no next", r, err)
	}
}
