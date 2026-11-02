package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/core"
)

// callLog is the ordered sequence of calls a fake channel saw, one "send:<title>" or "resolve:<key>"
// per call, across all keys.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, s)
}

func (l *callLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// forKey is the calls for key. Sends are labelled "send:<key>:<title>" and resolves "resolve:<key>".
func (l *callLog) forKey(key string) []string {
	var out []string
	for _, c := range l.all() {
		_, rest, _ := strings.Cut(c, ":")
		k, _, _ := strings.Cut(rest, ":")
		if k == key {
			out = append(out, c)
		}
	}
	return out
}

// recordCalls makes f log every call, answering a send through send (nil: it succeeds) and a
// resolve through resolve (nil: it succeeds).
func recordCalls(f *channeltest.Fake, send func(m channel.Message) error, resolve func(key string) error) *callLog {
	l := &callLog{}
	f.SendFunc = func(_ context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
		l.add("send:" + m.DedupKey + ":" + m.Title)
		if send != nil {
			if err := send(m); err != nil {
				return channel.Receipt{}, err
			}
		}
		return channel.Receipt{Instance: s.Instance}, nil
	}
	f.ResolveFunc = func(_ context.Context, key string, _ channel.Message) error {
		l.add("resolve:" + key)
		if resolve != nil {
			return resolve(key)
		}
		return nil
	}
	return l
}

// noTriggerAfterResolve fails the test when calls (one key's, in order, no reopening in between)
// holds a send after a resolve with no further resolve behind it.
func noTriggerAfterResolve(t *testing.T, calls []string) {
	t.Helper()
	isResolve := func(c string) bool { return strings.HasPrefix(c, "resolve:") }
	for i, c := range calls {
		if strings.HasPrefix(c, "send:") && slices.ContainsFunc(calls[:i], isResolve) && !slices.ContainsFunc(calls[i+1:], isResolve) {
			t.Errorf("trigger %q came after a resolve with none behind it; calls = %v", c, calls)
			return
		}
	}
}

var temporary = func(error) error { return channel.Transient(errors.New("down")) }

// A trigger waiting for its retry is gone once its incident is resolved. The channel never reached
// the human (its one send failed), so the resolve has nothing to tell it: no resolve or note is due.
func TestRetryingTriggerIsNotSentAfterResolve(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	log := recordCalls(e.fakes["a"], func(channel.Message) error { return temporary(nil) }, nil)
	e.say("page", "k", "disk full")
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("setup: queued = %d, want the failed trigger", q)
	}
	e.resolve("k", "fixed")
	e.clk.Advance(2 * time.Minute)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := log.forKey("k")
	if want := []string{"send:k:disk full"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v; want only the failed trigger (the channel never reached the human, so there is nothing to resolve)", calls)
	}
	noTriggerAfterResolve(t, calls)
}

// A resolve waiting for its retry is gone once the key is raised again.
func TestRetryingResolveIsNotSentAfterReopen(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": {ReachesHuman: true, NativeDedup: true, Resolve: true}}
	e := newEnv(t, incCfg(0, "a"), caps)
	log := recordCalls(e.fakes["a"], nil, func(string) error { return temporary(nil) })
	e.say("page", "k", "first")
	_, _ = e.p.Resolve(context.Background(), "k", "fixed") // the note fails: a queued problem, not a test failure
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("setup: queued = %d, want the resolve waiting for its retry", q)
	}
	e.say("page", "k", "second")
	e.clk.Advance(2 * time.Minute)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := log.forKey("k")
	if n := len(calls); n == 0 || calls[n-1] != "send:k:second" {
		t.Errorf("calls = %v; the last must be the new trigger", calls)
	}
}

// A trigger parked because its channel was not built is gone once its incident is resolved.
func TestParkedTriggerIsNotSentAfterResolve(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	log := recordCalls(e.fakes["a"], func(channel.Message) error { return temporary(nil) }, nil)
	e.say("page", "k", "disk full")
	// A run that does not build the channel parks the entry.
	var n int
	other := core.New(core.Deps{Config: e.cfg, Store: e.st, Channels: map[string]channel.Channel{}, Caps: one, Now: e.clk.Now,
		NewID: func() string { n++; return fmt.Sprintf("P%03d", n) }})
	e.clk.Advance(2 * time.Minute)
	if _, err := other.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("setup: queued = %d, want the parked trigger", q)
	}
	e.resolve("k", "fixed")
	e.clk.Advance(2 * time.Minute)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := log.forKey("k")
	if want := []string{"send:k:disk full"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v; want only the first failed trigger", calls)
	}
	noTriggerAfterResolve(t, calls)
}

// A trigger in flight in one process when another resolves the key still ends with a resolve.
func TestTriggerInFlightElsewhereEndsWithResolve(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": {ReachesHuman: true, NativeDedup: true, Resolve: true}}
	e := newEnv(t, incCfg(0, "a"), caps)
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce sync.Once
	log := &callLog{}
	e.fakes["a"].SendFunc = func(_ context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
		log.add("send:" + m.DedupKey + ":" + m.Title)
		startedOnce.Do(func() { close(started) })
		<-release
		return channel.Receipt{Instance: s.Instance}, nil
	}
	e.fakes["a"].ResolveFunc = func(_ context.Context, key string, _ channel.Message) error {
		log.add("resolve:" + key)
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "disk full", DedupKey: "k"})
	}()
	<-started
	var n int
	other := core.New(core.Deps{Config: e.cfg, Store: e.st, Channels: map[string]channel.Channel{"a": e.fakes["a"]}, Caps: caps, Now: e.clk.Now,
		NewID: func() string { n++; return fmt.Sprintf("P%03d", n) }})
	if r, err := other.Resolve(context.Background(), "k", "fixed"); err != nil || r.Status != core.StatusResolved {
		t.Fatalf("Resolve = %q, %v", r.Status, err)
	}
	close(release)
	<-done
	e.clk.Advance(2 * time.Minute)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := log.forKey("k")
	if n := len(calls); n < 2 || calls[0] != "send:k:disk full" || calls[n-1] != "resolve:k" {
		t.Errorf("calls = %v; want the trigger, then a resolve last", calls)
	}
	noTriggerAfterResolve(t, calls)
}

// One flush over several keys with several workers keeps each key's calls in queue order. The store
// holds, for one key, a resolve note of the closed generation and a trigger of the next at once:
// both are due, and the stale note must not be sent at all: it is dropped when claimed, so each key's
// calls are the first trigger, the note that failed, the new trigger that failed, and its retry.
func TestFlushWorkersKeepPerKeyOrder(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": {ReachesHuman: true, NativeDedup: true, Resolve: true}}
	e := newEnvW(t, incCfg(0, "a"), caps, 4)
	var failing atomic.Bool
	fail := func() error {
		if failing.Load() {
			return temporary(nil)
		}
		return nil
	}
	log := recordCalls(e.fakes["a"], func(channel.Message) error { return fail() }, func(string) error { return fail() })
	keys := []string{"k", "m", "n", "o"}
	for _, k := range keys {
		failing.Store(false)
		e.say("page", k, k+" first")
		failing.Store(true)
		_, _ = e.p.Resolve(context.Background(), k, "fixed") // the note fails and waits
		e.say("page", k, k+" second")
	}
	if q, _, _ := e.counts(); q < 2*len(keys) {
		t.Fatalf("setup: queued = %d", q)
	}
	failing.Store(false)
	e.clk.Advance(2 * time.Minute)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		calls := log.forKey(k)
		want := []string{"send:" + k + ":" + k + " first", "resolve:" + k, "send:" + k + ":" + k + " second", "send:" + k + ":" + k + " second"}
		if !slices.Equal(calls, want) {
			t.Errorf("key %s: calls = %v; want %v", k, calls, want)
		}
	}
}
