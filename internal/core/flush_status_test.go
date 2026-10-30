package core_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
)

// queuedOnce leaves one send in the queue after a failed first attempt, due again in a minute.
func queuedOnce(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	if r := e.say("page", "k", "t"); r.Status != core.StatusQueued {
		t.Fatalf("first = %q", r.Status)
	}
	e.clk.Advance(2 * time.Minute)
	return e
}

func (e *env) flush() core.Result {
	e.t.Helper()
	r, err := e.p.Flush(context.Background())
	if err != nil {
		e.t.Fatalf("Flush: %v", err)
	}
	return r
}

func TestFlushThatLeavesASendQueuedIsQueuedWithNext(t *testing.T) {
	e := queuedOnce(t)
	r := e.flush()
	if r.Status != core.StatusQueued || r.Next == nil || r.Next.AttemptsLeft != 4 || r.Next.AfterSeconds != 120 || r.Next.Do != "yo-human flush" {
		t.Fatalf("flush = %+v; want queued with next in 120s and 4 attempts left (2 of the 6 sends spent; the backoff doubles)", r)
	}
}

func TestFlushThatBuriesASendFails(t *testing.T) {
	e := queuedOnce(t)
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	if r := e.flush(); r.Status != core.StatusFailed || r.Next != nil {
		t.Fatalf("flush = %+v; want failed and no next", r)
	}
}

func TestFlushThatDeliversIsOK(t *testing.T) {
	e := queuedOnce(t)
	e.fakes["a"].SendFunc = nil
	if r := e.flush(); r.Status != core.StatusOK || r.Next != nil {
		t.Fatalf("flush = %+v; want ok", r)
	}
	if r := e.flush(); r.Status != core.StatusOK {
		t.Fatalf("empty flush = %+v; want ok", r)
	}
}

func TestADeadSendOutweighsAQueuedOne(t *testing.T) {
	e := newEnv(t, incCfg(0, "a", "b"), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.fakes["b"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "t")
	e.clk.Advance(2 * time.Minute)
	e.fakes["b"].SendFunc = failWith(channel.Permanent, "revoked")
	if r := e.flush(); r.Status != core.StatusFailed {
		t.Fatalf("flush = %+v; want failed (dead wins over queued)", r)
	}
}

func TestFlushNextUsesTheEntrysLevelRetry(t *testing.T) {
	cfg := incCfg(0, "a")
	cfg.Levels[1].Retry.Times = 4
	e := newEnv(t, cfg, one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "t")
	e.clk.Advance(2 * time.Minute)
	if r := e.flush(); r.Next == nil || r.Next.AttemptsLeft != 3 {
		t.Fatalf("flush = %+v; want next with 3 sends left of the page level's 5 (retry times 4, plus the first)", r)
	}
}

func TestParkedEntriesDoNotMakeAFlushQueued(t *testing.T) {
	e := queuedOnce(t)
	delete(e.cfg.Channels, "a") // the entry now belongs to another config
	if r := e.flush(); r.Status != core.StatusOK || r.Next != nil || len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelQueued {
		t.Fatalf("flush = %+v; want ok with the parked entry listed", r)
	}
}

// The countdown the agent is told never skips or repeats: notify says 5, each flush one fewer, and
// the flush after the one that says 1 buries the send (exit 69 in the CLI). There is no "0 left":
// a send with none left is dead-lettered, not queued.
func TestTheAttemptsLeftCountDownToTheDeadLetter(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	first := e.say("page", "k", "t")
	if first.Status != core.StatusQueued || first.Next == nil {
		t.Fatalf("notify = %+v", first)
	}
	got := []int{first.Next.AttemptsLeft}
	last := first
	for i := 0; i < 10 && last.Status == core.StatusQueued; i++ {
		e.clk.Advance(2 * time.Hour) // past any backoff
		last = e.flush()
		if last.Next != nil {
			got = append(got, last.Next.AttemptsLeft)
		}
	}
	if !slices.Equal(got, []int{5, 4, 3, 2, 1}) || last.Status != core.StatusFailed || last.Next != nil {
		t.Fatalf("countdown = %v ending %+v; want 5, 4, 3, 2, 1 then failed", got, last)
	}
	if _, _, dead := e.counts(); dead != 1 {
		t.Errorf("dead = %d; want the send buried after the last allowed flush", dead)
	}
}
