package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t     *testing.T
	st    *store.Store
	dir   string
	cfg   *config.Config
	fakes map[string]*channeltest.Fake
	clk   *clock
	p     *core.Pipeline
}

var human = channel.Capabilities{ReachesHuman: true}

// newEnv wires a pipeline over a temp store, a fake clock at t0 and one fake per name in caps.
func newEnv(t *testing.T, cfg *config.Config, caps map[string]channel.Capabilities) *env {
	t.Helper()
	return newEnvW(t, cfg, caps, 0)
}

// newEnvW is newEnv with a set number of backlog workers (zero: the default).
func newEnvW(t *testing.T, cfg *config.Config, caps map[string]channel.Capabilities, workers int) *env {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, dir: dir, cfg: cfg, fakes: map[string]*channeltest.Fake{}, clk: &clock{t: t0}}
	chans := map[string]channel.Channel{}
	if cfg.Channels == nil {
		cfg.Channels = map[string]config.ChannelInstance{}
	}
	for name, c := range caps {
		if _, ok := cfg.Channels[name]; !ok { // every channel a test builds is configured, like the binary's
			cfg.Channels[name] = config.ChannelInstance{Type: "fake"}
		}
		e.fakes[name] = channeltest.NewFake(c)
		chans[name] = e.fakes[name]
	}
	var n atomic.Int64
	e.p = core.New(core.Deps{Config: cfg, Store: st, Channels: chans, Caps: caps, Now: e.clk.Now, BacklogWorkers: workers,
		NewID: func() string { return fmt.Sprintf("M%03d", n.Add(1)) }})
	return e
}

func levelsCfg(deadline time.Duration, lv ...config.Level) *config.Config {
	return &config.Config{Deadline: deadline, Levels: lv}
}

func sends(names ...string) []config.Send {
	var out []config.Send
	for _, n := range names {
		out = append(out, config.Send{Channel: n, Interruption: channel.Active})
	}
	return out
}

func page(retry config.Retry, names ...string) config.Level {
	return config.Level{Name: "page", Retry: retry, Send: sends(names...)}
}

var retry5 = config.Retry{Every: 60 * time.Second, Times: 5}

func failWith(kind func(error) error, msg string) func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
	return func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{}, kind(errors.New(msg))
	}
}

func (e *env) counts() (q, in, dead int) {
	e.t.Helper()
	q, in, dead, err := e.st.Counts()
	if err != nil {
		e.t.Fatal(err)
	}
	return q, in, dead
}

func (e *env) notify(level string) core.Result {
	e.t.Helper()
	r, err := e.p.Notify(context.Background(), channel.Message{Level: level, Title: "t"})
	if err != nil {
		e.t.Fatalf("Notify: %v", err)
	}
	return r
}

func statuses(r core.Result) map[string]string {
	out := map[string]string{}
	for _, c := range r.Channels {
		out[c.Channel] = c.Status
	}
	return out
}

// need stops a test cleanly, not with an index panic, when a stub or a bug produced too few results.
func need(t *testing.T, r core.Result, f *channeltest.Fake, channels, sent int) {
	t.Helper()
	if len(r.Channels) < channels || len(f.Sent) < sent || len(f.Specs) < sent {
		t.Fatalf("result has %d channels and %d sends; want at least %d and %d", len(r.Channels), len(f.Sent), channels, sent)
	}
}
