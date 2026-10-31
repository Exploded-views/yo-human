package core_test

import (
	"context"
	"errors"
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
