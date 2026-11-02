package core_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

// lateCfg has a paging level ("page", with a critical send) and a level with none ("low").
func lateCfg() *config.Config {
	return levelsCfg(5*time.Second,
		config.Level{Name: "page", Retry: retry5, Send: []config.Send{{Channel: "a", Interruption: channel.Critical}}},
		config.Level{Name: "low", Retry: retry5, Send: sends("a")})
}

// titleLog records the title of each message the fake sees, and fails the first n sends (a temporary
// error) when asked to.
type titleLog struct {
	mu   sync.Mutex
	seen []string
	fail int
}

func (ts *titleLog) install(e *env, fail int) {
	ts.fail = fail
	e.fakes["a"].SendFunc = func(_ context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.seen = append(ts.seen, m.Title)
		if len(ts.seen) <= ts.fail {
			return channel.Receipt{}, channel.Transient(context.DeadlineExceeded)
		}
		return channel.Receipt{Instance: s.Instance}, nil
	}
}

func (ts *titleLog) last() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.seen) == 0 {
		return "<none>"
	}
	return ts.seen[len(ts.seen)-1]
}

func (e *env) flushAt(d time.Duration) {
	e.t.Helper()
	e.clk.Advance(d)
	if _, err := e.p.Flush(context.Background()); err != nil {
		e.t.Fatalf("Flush: %v", err)
	}
}

func TestLatePageGetsQueuedSinceMarker(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	e.clk.Advance(2*time.Hour + 5*time.Minute + 59*time.Second) // 14:05:59Z
	e.say("page", "", "disk full")
	e.flushAt(6*time.Minute + time.Second) // 14:12:00Z
	if got, want := ts.last(), "[queued since 14:05 UTC] disk full"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestLatePageWithNoTitleGetsMarkerAlone(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	e.clk.Advance(2*time.Hour + 5*time.Minute + 59*time.Second)
	e.say("page", "", "")
	e.flushAt(6*time.Minute + time.Second)
	if got, want := ts.last(), "[queued since 14:05 UTC]"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestPageAtExactlyFiveMinutesHasNoMarker(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	e.say("page", "", "disk full")
	e.flushAt(5 * time.Minute)
	if got := ts.last(); got != "disk full" {
		t.Errorf("title = %q; exactly five minutes is not late", got)
	}
}

func TestLateNonPagingSendHasNoMarker(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	e.say("low", "", "disk full")
	e.flushAt(30 * time.Minute)
	if got := ts.last(); got != "disk full" {
		t.Errorf("title = %q; a level with no critical send gets no marker", got)
	}
}

func TestLateResolveHasNoMarker(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": {ReachesHuman: true, NativeDedup: true, Resolve: true}}
	for _, name := range []string{"note", "resolve"} {
		t.Run(name, func(t *testing.T) {
			c := caps
			if name == "note" {
				c = one
			}
			e := newEnv(t, lateCfg(), c)
			var ts titleLog
			ts.install(e, 0)
			var resolved []string
			var mu sync.Mutex
			e.fakes["a"].ResolveFunc = func(_ context.Context, _ string, m channel.Message) error {
				mu.Lock()
				defer mu.Unlock()
				resolved = append(resolved, m.Title)
				if len(resolved) == 1 {
					return channel.Transient(context.DeadlineExceeded)
				}
				return nil
			}
			e.say("page", "k", "disk full")
			// the note, on a channel that cannot resolve, goes through Send: fail its first try too
			if name == "note" {
				ts.mu.Lock()
				ts.fail = len(ts.seen) + 1
				ts.mu.Unlock()
			}
			_, _ = e.p.Resolve(context.Background(), "k", "fixed")
			e.flushAt(30 * time.Minute)
			want := "Resolved: disk full"
			if name == "note" {
				if got := ts.last(); got != want {
					t.Errorf("note title = %q, want %q", got, want)
				}
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if n := len(resolved); n == 0 || resolved[n-1] != want {
				t.Errorf("resolve titleLog = %v, want last %q", resolved, want)
			}
		})
	}
}

func TestMarkerIsNotStored(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 2) // the first send and the first late retry both fail
	e.clk.Advance(2*time.Hour + 5*time.Minute + 59*time.Second)
	e.say("page", "", "disk full")
	e.flushAt(6*time.Minute + time.Second)
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("setup: queued = %d, want the failed late retry back in the queue", q)
	}
	e.flushAt(2 * time.Minute)
	if got, want := ts.last(), "[queued since 14:05 UTC] disk full"; got != want {
		t.Errorf("second late retry title = %q, want exactly one marker: %q", got, want)
	}
}

func TestOnTimePageHasNoMarker(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 0)
	if r := e.say("page", "", "disk full"); r.Status != core.StatusDelivered {
		t.Fatalf("status = %q", r.Status)
	}
	if got := ts.last(); got != "disk full" {
		t.Errorf("title = %q", got)
	}
}

// lateHoldCfg is lateCfg with quiet hours from 13:00 to 14:00 UTC that "page" holds for.
func lateHoldCfg() *config.Config {
	c := lateCfg()
	c.Levels[0].HoldInQuietHours = true
	c.QuietHours = config.QuietHours{Location: time.UTC, Windows: []config.Window{{From: 13 * 60, To: 14 * 60}}}
	return c
}

// A page held for quiet hours is released on purpose at the end of the window: it is not late.
func TestPageHeldForQuietHoursIsNotLateWhenReleased(t *testing.T) {
	e := newEnv(t, lateHoldCfg(), one) // "a" cannot schedule: the queue holds the page
	var ts titleLog
	ts.install(e, 0)
	e.clk.Advance(90 * time.Minute) // 13:30
	if r := e.say("page", "", "disk full"); r.Status != core.StatusHeld {
		t.Fatalf("setup: status = %q, want held", r.Status)
	}
	e.flushAt(30 * time.Minute) // 14:00, the window's end
	if got := ts.last(); got != "disk full" {
		t.Errorf("title = %q; a page held on purpose and released at the end of the hold is not late", got)
	}
}

// Once released, a page that fails and is retried more than five minutes after the end of the hold is
// late, and says when it was queued, not when the hold ended.
func TestHeldPageRetriedLateIsMarkedWithItsQueueTime(t *testing.T) {
	e := newEnv(t, lateHoldCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	e.clk.Advance(90 * time.Minute) // 13:30
	e.say("page", "", "disk full")
	e.flushAt(30 * time.Minute) // 14:00: released, fails
	if got := ts.seen[0]; got != "disk full" {
		t.Errorf("first send at the end of the hold = %q, want no marker", got)
	}
	e.flushAt(7 * time.Minute) // 14:07
	if got, want := ts.last(), "[queued since 13:30 UTC] disk full"; got != want {
		t.Errorf("retry title = %q, want %q", got, want)
	}
}

// A page deferred behind the incident lock and admitted late is still the page queued earlier: its
// retries say when that was.
func TestAdmittedPageKeepsItsQueueTime(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	_, release := deferred(t, e) // queued at 12:00
	release()
	e.flushAt(20 * time.Minute) // admitted at 12:20: sent late, fails
	e.flushAt(2 * time.Minute)  // retried
	for i, got := range ts.seen {
		if want := "[queued since 12:00 UTC] t"; got != want {
			t.Errorf("send %d title = %q, want %q", i, got, want)
		}
	}
	if len(ts.seen) < 2 {
		t.Errorf("sends = %v; want the failed admission and its retry", ts.seen)
	}
}

// An entry with no queue time (hand-written or corrupt) cannot say when it was queued, so it is not marked.
func TestEntryWithNoEnqueueTimeHasNoMarker(t *testing.T) {
	e := newEnv(t, lateCfg(), one)
	var ts titleLog
	ts.install(e, 1)
	e.say("page", "", "disk full")
	due, err := e.st.Due(e.clk.Now().Add(time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("setup: %v %v", due, err)
	}
	c, ok, err := e.st.Claim(due[0].Key)
	if !ok || err != nil {
		t.Fatalf("claim: %v %v", ok, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(c.Data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "enqueued")
	c.Data, _ = json.Marshal(raw)
	if err := e.st.Release(c, e.clk.Now()); err != nil {
		t.Fatal(err)
	}
	e.flushAt(30 * time.Minute)
	if got := ts.last(); got != "disk full" {
		t.Errorf("title = %q; an entry with no queue time gets no marker", got)
	}
}
