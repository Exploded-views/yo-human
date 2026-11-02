package core_test

import (
	"context"
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
