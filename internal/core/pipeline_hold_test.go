package core_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

// holdCfg holds "page" from 22:00 to 07:00 UTC; t0 is moved inside the window by the tests.
func holdCfg() *config.Config {
	lv := page(retry5, "phone")
	lv.HoldInQuietHours = true
	c := levelsCfg(time.Second, lv)
	c.QuietHours = config.QuietHours{Location: time.UTC, Windows: []config.Window{{From: 22 * 60, To: 7 * 60}}}
	return c
}

func TestHeldLocallyIsNotSentUntilDue(t *testing.T) {
	e := newEnv(t, holdCfg(), map[string]channel.Capabilities{"phone": human})
	e.clk.Advance(11 * time.Hour) // 23:00
	r := e.notify("page")
	need(t, r, e.fakes["phone"], 1, 0)
	if r.Status != core.StatusHeld || r.Channels[0].Status != "held" || r.Next != nil {
		t.Fatalf("got %q %+v next=%+v; want held", r.Status, r.Channels, r.Next)
	}
	if len(e.fakes["phone"].Sent) != 0 {
		t.Fatal("a held send went out")
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("queued = %d; want the held send kept", q)
	}
	e.clk.Advance(7 * time.Hour) // 06:00, still inside
	if _, err := e.p.Flush(context.Background()); err != nil || len(e.fakes["phone"].Sent) != 0 {
		t.Fatalf("flush inside the window: %v, sent %d", err, len(e.fakes["phone"].Sent))
	}
	e.clk.Advance(time.Hour + time.Minute) // 07:01
	fr, err := e.p.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkFlushedHeld(t, e, fr)
}

func checkFlushedHeld(t *testing.T, e *env, fr core.Result) {
	t.Helper()
	need(t, fr, e.fakes["phone"], 1, 1)
	if fr.Status != core.StatusOK || len(fr.Channels) != 1 || fr.Channels[0].Status != "sent" || fr.Channels[0].Channel != "phone" {
		t.Errorf("flush result = %+v", fr)
	}
	if !e.fakes["phone"].Specs[0].DeliverAt.IsZero() {
		t.Error("a locally held send was given a DeliverAt")
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("counts %d %d %d; want empty", q, in, d)
	}
}

func TestScheduledChannelGetsDeliverAt(t *testing.T) {
	e := newEnv(t, holdCfg(), map[string]channel.Capabilities{"phone": {ReachesHuman: true, Schedule: true}})
	e.clk.Advance(11 * time.Hour)
	e.fakes["phone"].SendFunc = func(_ context.Context, _ channel.Message, s channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: s.Instance, ProviderID: "p1", Scheduled: true}, nil
	}
	r := e.notify("page")
	need(t, r, e.fakes["phone"], 1, 1)
	want := time.Date(2026, 10, 31, 7, 0, 0, 0, time.UTC)
	if got := e.fakes["phone"].Specs[0].DeliverAt; !got.Equal(want) {
		t.Errorf("DeliverAt = %v; want the window end %v", got, want)
	}
	if r.Status != core.StatusHeld || r.Channels[0].Status != "held" {
		t.Errorf("got %q %+v; want held", r.Status, r.Channels)
	}
	if c := r.Channels[0]; c.ProviderID != "p1" || !c.Scheduled {
		t.Errorf("channel result %+v; want the receipt's provider id and Scheduled for Task 6", c)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("counts %d %d %d; the provider holds it, so the queue must not", q, in, d)
	}
}

func TestFlushRetriesWithBackoffThenDeadLetters(t *testing.T) {
	cfg := levelsCfg(time.Second, page(config.Retry{Every: 60 * time.Second, Times: 2}, "a"))
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.notify("page")
	// Each step advances the clock, flushes, and expects this many sends in total so far.
	steps := []struct {
		advance time.Duration
		sent    int
		why     string
	}{
		{59 * time.Second, 1, "no retry before +60s"},
		{time.Second, 2, "first retry at +60s"},
		{119 * time.Second, 2, "no retry before +180s (the backoff doubled to 120s)"},
		{time.Second, 3, "second retry at +180s"},
		{24 * time.Hour, 3, "a dead send is never tried again"},
	}
	for _, s := range steps {
		e.clk.Advance(s.advance)
		if _, err := e.p.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := len(e.fakes["a"].Sent); got != s.sent {
			t.Fatalf("after +%v: %d sends; want %d (%s)", s.advance, got, s.sent, s.why)
		}
	}
	if q, _, d := e.counts(); q != 0 || d != 1 {
		t.Errorf("queued=%d dead=%d; want it dead-lettered after two retries", q, d)
	}
	if dead, _ := e.st.DeadEntries(); len(dead) != 1 || dead[0].Reason == "" {
		t.Errorf("dead = %+v; want one with a reason", dead)
	}
}

func TestNewMessageBeforeBacklog(t *testing.T) {
	cfg := levelsCfg(200*time.Millisecond, page(retry5, "a"))
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	for range 50 {
		e.notify("page")
	}
	e.clk.Advance(time.Minute) // all 50 are due
	e.fakes["a"].SendFunc = func(ctx context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		<-ctx.Done()
		return channel.Receipt{}, ctx.Err()
	}
	before := len(e.fakes["a"].Sent)
	start := time.Now()
	e.notify("page")
	if el := time.Since(start); el > 400*time.Millisecond {
		t.Errorf("Notify with a backlog took %v; want it bounded by the deadline", el)
	}
	sent := e.fakes["a"].Sent[before:]
	if len(sent) == 0 || sent[0].ID != "M051" {
		ids := fmt.Sprint(len(sent))
		if len(sent) > 0 {
			ids = sent[0].ID
		}
		t.Errorf("first send after the backlog built up was %s; want the new message M051", ids)
	}
}

func TestBacklogEntryTakenByAnotherProcessIsSkipped(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "a"))
	cfg.IncidentTTL, cfg.Levels[0].Reminder = time.Hour, time.Second     // the second notify is a reminder: the same incident
	e := newEnvW(t, cfg, map[string]channel.Capabilities{"a": human}, 1) // one at a time, so the second entry is still queued when the first is sent
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	for range 2 {
		if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page", DedupKey: "k"}); err != nil {
			t.Fatal(err)
		}
		e.clk.Advance(time.Second)
	}
	e.clk.Advance(time.Minute)
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		// Another process resolves the incident while this one is mid-flush: the entries
		// not yet claimed are gone from the queue.
		if _, err := e.st.CancelIncident("k"); err != nil {
			t.Error(err)
		}
		return channel.Receipt{}, nil
	}
	r, err := e.p.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(e.fakes["a"].Sent) != 3 { // 2 failures, then only the entry already claimed
		t.Errorf("sent %d; want the cancelled entry not sent", len(e.fakes["a"].Sent))
	}
	var skipped int
	for _, c := range r.Channels {
		if c.Status == "skipped" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("results %+v; want one skipped", r.Channels)
	}
}

func TestConcurrentFlushesNeverSendTwice(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	for range 20 {
		e.notify("page")
	}
	e.fakes["a"].SendFunc = nil
	e.clk.Advance(time.Minute)
	before := len(e.fakes["a"].Sent)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.p.Flush(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := len(e.fakes["a"].Sent) - before; got != 20 {
		t.Errorf("%d sends for 20 entries; want each sent exactly once", got)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("counts %d %d %d; want empty", q, in, d)
	}
}
