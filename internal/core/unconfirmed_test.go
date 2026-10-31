package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

func unconfirmedSend(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
	return channel.Receipt{}, channel.Unconfirmed(errors.New("x request timed out"))
}

// unconfirmedCfg has a level "note" (no critical send) and a level "alarm" (one critical send, any
// channel), whatever the levels are named.
func unconfirmedCfg() *config.Config {
	return levelsCfg(time.Second,
		config.Level{Name: "note", Retry: retry5, Send: []config.Send{{Channel: "a", Interruption: channel.TimeSensitive}}},
		config.Level{Name: "alarm", Retry: retry5, Send: []config.Send{
			{Channel: "a", Interruption: channel.TimeSensitive}, {Channel: "b", Interruption: channel.Critical}}},
	)
}

func TestUnconfirmedBelowAPagingLevelIsSentAndNotRetried(t *testing.T) {
	e := newEnv(t, unconfirmedCfg(), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = unconfirmedSend
	r := e.notify("note")
	if len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelSent || !r.Channels[0].Unconfirmed || r.Status != core.StatusDelivered {
		t.Fatalf("result = %+v; want sent, unconfirmed, delivered", r)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("counts = %d %d %d; want the entry completed, nothing queued", q, in, d)
	}
	if len(e.fakes["a"].Sent) != 1 {
		t.Errorf("sends = %d; want one", len(e.fakes["a"].Sent))
	}
}

func TestUnconfirmedAtAPagingLevelIsRetried(t *testing.T) {
	e := newEnv(t, unconfirmedCfg(), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = unconfirmedSend // a level whose critical send is on another channel still pages
	r := e.notify("alarm")
	if got := statuses(r)["a"]; got != core.ChannelQueued {
		t.Errorf("a = %q; want queued", got)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d; want 1", q)
	}
}

func TestUnconfirmedOnADedupChannelIsRetried(t *testing.T) {
	e := newEnv(t, unconfirmedCfg(), map[string]channel.Capabilities{"a": {ReachesHuman: true, NativeDedup: true}, "b": human})
	e.fakes["a"].SendFunc = unconfirmedSend
	r := e.notify("note")
	if r.Channels[0].Status != core.ChannelQueued || r.Channels[0].Unconfirmed {
		t.Errorf("result = %+v; want queued", r.Channels[0])
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d; want 1", q)
	}
}

func TestPlainTransientBelowAPagingLevelIsRetried(t *testing.T) {
	e := newEnv(t, unconfirmedCfg(), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	r := e.notify("note")
	if r.Channels[0].Status != core.ChannelQueued || r.Channels[0].Unconfirmed {
		t.Errorf("result = %+v; want queued", r.Channels[0])
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d; want 1", q)
	}
}

// A keyed send that went out and got no answer still counts as reached: the incident records it, so
// a repeat of the same message is suppressed rather than sent again.
func TestUnconfirmedKeyedSendIsRecordedOnItsIncident(t *testing.T) {
	cfg := unconfirmedCfg()
	cfg.IncidentTTL = 24 * time.Hour
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = unconfirmedSend
	r := e.say("note", "k", "disk")
	if len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelSent || !r.Channels[0].Unconfirmed {
		t.Fatalf("result = %+v; want sent, unconfirmed", r)
	}
	if inc := e.incident("k"); len(inc.Reached) != 1 || inc.Reached[0].Instance != "a" {
		t.Fatalf("reached = %+v; want the instance recorded", inc.Reached)
	}
	e.say("note", "k", "disk")
	if n := len(e.fakes["a"].Sent); n != 1 {
		t.Errorf("sends = %d after a repeat; want the repeat suppressed (1)", n)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("counts = %d %d %d; want nothing queued", q, in, d)
	}
}

// A level the config no longer has is taken for a paging one: the entry is retried, not completed.
func TestUnconfirmedForARemovedLevelIsRetried(t *testing.T) {
	e := newEnv(t, unconfirmedCfg(), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	if r := e.notify("note"); r.Channels[0].Status != core.ChannelQueued {
		t.Fatalf("setup: %+v", r)
	}
	e.fakes["a"].SendFunc = unconfirmedSend
	cfg := *e.cfg
	cfg.Levels = cfg.Levels[1:] // "note" is gone
	p := core.New(core.Deps{Config: &cfg, Store: e.st, Channels: map[string]channel.Channel{"a": e.fakes["a"]},
		Caps: map[string]channel.Capabilities{"a": human}, Now: e.clk.Now, NewID: newIDs()})
	e.clk.Advance(2 * time.Minute)
	r, err := p.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelQueued || r.Channels[0].Unconfirmed {
		t.Fatalf("flush = %+v; want queued again", r.Channels)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d; want 1", q)
	}
}

// A resolve note follows the same rule: below a paging level, unconfirmed is sent and not retried.
func TestUnconfirmedResolveNoteBelowPagingIsNotRetried(t *testing.T) {
	cfg := unconfirmedCfg()
	cfg.IncidentTTL = 24 * time.Hour
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human, "b": human})
	if r := e.say("note", "k", "disk"); r.Status != core.StatusDelivered {
		t.Fatalf("setup: %+v", r)
	}
	e.fakes["a"].SendFunc = unconfirmedSend
	r := e.resolve("k", "fixed")
	n := len(e.fakes["a"].Sent)
	if len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelSent || !r.Channels[0].Unconfirmed || !e.fakes["a"].Sent[n-1].Resolved {
		t.Fatalf("resolve = %+v; want the note sent, unconfirmed", r.Channels)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("counts = %d %d %d; want nothing queued", q, in, d)
	}
}

// log.jsonl keeps the mark, so a send that may not have arrived can be told from one that did.
func TestUnconfirmedSendIsMarkedInTheLog(t *testing.T) {
	e := newEnv(t, unconfirmedCfg(), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = unconfirmedSend
	e.notify("note")
	b, err := os.ReadFile(filepath.Join(e.dir, "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var line struct {
		Status      string
		Unconfirmed bool
	}
	if err := json.Unmarshal(bytes.TrimSpace(b), &line); err != nil || line.Status != "sent" || !line.Unconfirmed {
		t.Errorf("log line = %q (%v); want sent with unconfirmed true", b, err)
	}
	e.fakes["a"].SendFunc = nil
	e.notify("note")
	b, _ = os.ReadFile(filepath.Join(e.dir, "log.jsonl"))
	if lines := bytes.Split(bytes.TrimSpace(b), []byte("\n")); bytes.Contains(lines[len(lines)-1], []byte("unconfirmed")) {
		t.Errorf("a confirmed send logged %q; want no unconfirmed field", lines[len(lines)-1])
	}
}
