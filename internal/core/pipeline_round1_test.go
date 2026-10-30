package core_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// attempts reads the Attempts of every queued entry (due at any time) by key.
func attempts(t *testing.T, e *env) map[string]int {
	t.Helper()
	due, err := e.st.Due(t0.Add(1000 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, d := range due {
		out[d.Key] = d.Attempts
	}
	return out
}

// A second process that sees every entry as old steals claims from Notify; Notify must still
// report the message as on its way, never as failed.
func TestNotifyNeverFailsBecauseAnotherProcessTookTheClaim(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a", "b")), map[string]channel.Capabilities{"a": human, "b": human})
	thief := channeltest.NewFake(human)
	thief2 := channeltest.NewFake(human)
	late := &clock{t: t0.Add(24 * time.Hour)} // to the thief every entry is old, so it takes them
	tp := core.New(core.Deps{Config: e.cfg, Store: e.st, Channels: map[string]channel.Channel{"a": thief, "b": thief2},
		Caps: map[string]channel.Capabilities{"a": human, "b": human}, Now: late.Now,
		NewID: func() string { return "T" }})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = tp.Flush(context.Background())
			}
		}
	}()
	for i := range 100 {
		r, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status == core.StatusFailed {
			t.Fatalf("run %d: failed with %+v while another process was delivering it", i, r.Channels)
		}
	}
	close(stop)
	wg.Wait()
	if _, _, d := e.counts(); d != 0 {
		t.Errorf("dead = %d; nothing failed, so nothing may be dead", d)
	}
}

// raw queues an entry as the pipeline would have: enqueued at the given time, whatever the message
// says about its own creation.
func raw(t *testing.T, e *env, key string, attempts int, created, enqueued time.Time) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"instance": "a", "fingerprint": core.FingerprintOf(e.cfg.Channels["a"]), "enqueued": enqueued,
		"message": channel.Message{ID: "R", Level: "page", Created: created}})
	if err := e.st.Enqueue(store.Pending{Key: key, Data: data, Attempts: attempts}); err != nil {
		t.Fatal(err)
	}
}

func TestBacklogLeavesFreshFirstAttemptsToTheirOwnCaller(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	raw(t, e, "R1.a", 0, e.clk.Now(), e.clk.Now()) // a message another caller is sending right now
	raw(t, e, "R2.a", 1, e.clk.Now(), e.clk.Now()) // young, but already tried: ours to retry
	e.clk.Advance(0)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent := e.fakes["a"].Sent
	if len(sent) != 1 || attempts(t, e)["R1.a"] != 0 {
		t.Fatalf("sent %d, queue %v; want only the already-tried entry sent, the fresh one left", len(sent), attempts(t, e))
	}
	if _, ok := attempts(t, e)["R1.a"]; !ok {
		t.Fatal("the fresh entry left the queue")
	}
	e.clk.Advance(3 * time.Second) // older than 2 x deadline: its caller is gone
	if _, err := e.p.Flush(context.Background()); err != nil || len(e.fakes["a"].Sent) != 2 {
		t.Errorf("flush after 2 x deadline: %v, sent %d; want the old entry sent", err, len(e.fakes["a"].Sent))
	}
}

func TestBacklogDoesNotStartWithAQuarterOfTheDeadlineLeft(t *testing.T) {
	e := newEnv(t, levelsCfg(400*time.Millisecond, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	for _, k := range []string{"B1.a", "B2.a", "B3.a"} {
		raw(t, e, k, 1, t0.Add(-time.Hour), t0.Add(-time.Hour))
	}
	e.fakes["a"].SendFunc = func(ctx context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		time.Sleep(330 * time.Millisecond) // leaves about 70ms, under a quarter (100ms)
		return channel.Receipt{}, nil
	}
	e.notify("page")
	if n := len(e.fakes["a"].Sent); n != 1 {
		t.Errorf("%d sends; want only the new message, no backlog send begun with so little time", n)
	}
	if q, _, _ := e.counts(); q != 3 {
		t.Errorf("queued = %d; want the 3 backlog entries untouched", q)
	}
}

func TestSendCutShortByTheCallerIsReleasedWithoutSpendingAnAttempt(t *testing.T) {
	e := newEnv(t, levelsCfg(2*time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	ctx, cancel := context.WithCancel(context.Background())
	e.fakes["a"].SendFunc = func(c context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		cancel() // the call is cut short early in the send
		<-c.Done()
		return channel.Receipt{}, c.Err()
	}
	r, err := e.p.Notify(ctx, channel.Message{Level: "page"})
	if err != nil {
		t.Fatal(err)
	}
	need(t, r, e.fakes["a"], 1, 1)
	if r.Status != core.StatusQueued || r.Channels[0].Status != "queued" {
		t.Errorf("got %q %+v; want queued", r.Status, r.Channels)
	}
	if got := attempts(t, e)["M001.a"]; got != 0 {
		t.Errorf("Attempts = %d; want 0: our own cut-off is not the channel's failure", got)
	}
}

func TestSendThatRanHalfTheDeadlineCountsAsAFailure(t *testing.T) {
	e := newEnv(t, levelsCfg(200*time.Millisecond, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = func(c context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		<-c.Done() // a provider that always hangs
		return channel.Receipt{}, c.Err()
	}
	e.notify("page")
	if got, ok := attempts(t, e)["M001.a"]; !ok || got != 1 {
		t.Errorf("Attempts = %d (queued %v); want 1, so a provider that always hangs still reaches the dead letter", got, ok)
	}
}

func TestFailedScheduledSendWithAFutureDeliverAtIsHeld(t *testing.T) {
	e := newEnv(t, holdCfg(), map[string]channel.Capabilities{"phone": {ReachesHuman: true, Schedule: true}})
	e.clk.Advance(11 * time.Hour)
	e.fakes["phone"].SendFunc = failWith(channel.Transient, "down")
	r := e.notify("page")
	need(t, r, e.fakes["phone"], 1, 1)
	if r.Status != core.StatusHeld || r.Channels[0].Status != "held" || r.Next != nil {
		t.Errorf("got %q %+v next=%+v; want held: it is due after the window anyway", r.Status, r.Channels, r.Next)
	}
}

func TestLogRecordsAttemptsAfterTheIncrement(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.notify("page")
	f, err := os.Open(filepath.Join(e.dir, "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("empty log")
	}
	var line struct{ Attempts int }
	if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line.Attempts != 1 {
		t.Errorf("first failed send logged attempts %d (%v); want 1", line.Attempts, err)
	}
}

func TestRecoverJudgesFileAgeByTheRealClock(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	strandedEntry(t, e, 0) // claimed just now, by a process that has since died
	e.clk.Advance(100 * time.Hour)
	before := len(e.fakes["a"].Sent)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.fakes["a"].Sent) != before {
		t.Error("a file written a moment ago was recovered because the fake clock ran ahead")
	}
	if _, in, _ := e.counts(); in != 1 {
		t.Errorf("inflight = %d; want the young file left alone", in)
	}
}

func TestNewMessageGoesBeforeBacklogWhenItIsFast(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "a"))
	e := newEnv(t, cfg, map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	for range 5 {
		e.notify("page")
	}
	e.clk.Advance(time.Minute)
	e.fakes["a"].SendFunc = nil // everything succeeds at once
	before := len(e.fakes["a"].Sent)
	e.notify("page")
	sent := e.fakes["a"].Sent[before:]
	if len(sent) != 6 || sent[0].ID != "M006" {
		t.Fatalf("sends after the backlog built up: %d, first %q; want 6 with the new M006 first", len(sent), sent[0].ID)
	}
	for _, m := range sent[1:] {
		if m.ID == "M006" {
			t.Error("the new message was sent twice")
		}
	}
}

func TestFreshnessIgnoresTheCallersCreatedTime(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	ancient := t0.Add(-1000 * time.Hour)
	future := t0.Add(1000 * time.Hour)
	raw(t, e, "OLD.a", 0, ancient, e.clk.Now())       // enqueued just now, but the caller says it is old
	raw(t, e, "NEW.a", 0, future, t0.Add(-time.Hour)) // enqueued long ago, but the caller says it is from the future
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	left := attempts(t, e)
	if _, ok := left["OLD.a"]; !ok {
		t.Error("an old Created made a just-enqueued entry stealable")
	}
	if _, ok := left["NEW.a"]; ok {
		t.Error("a future Created kept an old entry from every backlog drain")
	}
}

func TestNotifyStampsTheEnqueueTime(t *testing.T) {
	cfg := holdCfg()
	cfg.Levels[0].HoldInQuietHours = true
	e := newEnv(t, cfg, map[string]channel.Capabilities{"phone": human})
	e.clk.Advance(18*time.Hour + 59*time.Minute + 59*time.Second) // 06:59:59, a second before the window ends
	m := channel.Message{Level: "page", Created: t0.Add(-1000 * time.Hour)}
	if _, err := e.p.Notify(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(time.Second) // 07:00:00: due, one second after it was enqueued
	if _, err := e.p.Flush(context.Background()); err != nil || len(e.fakes["phone"].Sent) != 0 {
		t.Fatalf("flush at the window end: %v, sent %d; want it left, its caller is still within 2 x deadline", err, len(e.fakes["phone"].Sent))
	}
	e.clk.Advance(2 * time.Second)
	if _, err := e.p.Flush(context.Background()); err != nil || len(e.fakes["phone"].Sent) != 1 {
		t.Errorf("flush 2 x deadline later: %v, sent %d; want it sent", err, len(e.fakes["phone"].Sent))
	}
}
