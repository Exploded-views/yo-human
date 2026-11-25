package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fake is a listener that runs until it is shut down or closed, or, with runErr, fails at once.
type fake struct {
	name              string
	runErr            error
	failOnStop        error // run returns this, not http.ErrServerClosed, once stopped
	hang              bool  // shutdown waits for its deadline, as a drain that never ends
	stopped           chan struct{}
	once              sync.Once
	shutdowns, closes atomic.Int32
	deadline          time.Time
	sawDeadline       bool
}

func newFake(name string) *fake { return &fake{name: name, stopped: make(chan struct{})} }

func (f *fake) stop() { f.once.Do(func() { close(f.stopped) }) }

func (f *fake) listener() listener {
	return listener{name: f.name,
		run: func() error {
			if f.runErr != nil {
				return f.runErr
			}
			<-f.stopped
			if f.failOnStop != nil {
				return f.failOnStop
			}
			return http.ErrServerClosed
		},
		shutdown: func(ctx context.Context) error {
			f.shutdowns.Add(1)
			f.deadline, f.sawDeadline = ctx.Deadline()
			if f.hang {
				<-ctx.Done()
				return ctx.Err()
			}
			f.stop()
			return nil
		},
		close: func() error { f.closes.Add(1); f.stop(); return nil },
	}
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestServeFailureBeatsCancel(t *testing.T) {
	boom := errors.New("bind failed")
	bad, other := newFake("approval"), newFake("MCP")
	bad.runErr = boom
	// ctx is already done when serve starts: both events are ready together, and the failure wins.
	err := serve(cancelled(), []listener{bad.listener(), other.listener()}, time.Second)
	if !errors.Is(err, boom) || errors.Is(err, ErrForcedClose) {
		t.Fatalf("serve = %v, want the failure", err)
	}
	if other.shutdowns.Load() != 1 {
		t.Errorf("the other listener was shut down %d times, want 1", other.shutdowns.Load())
	}
}

// With ctx cancelled and nothing failed yet, the ctx arm is the only one that can fire; a listener
// that fails while it drains must still be what serve returns.
func TestServeFailureDuringDrainBeatsCancel(t *testing.T) {
	boom := errors.New("died while draining")
	dying, other := newFake("MCP"), newFake("approval")
	dying.failOnStop = boom
	err := serve(cancelled(), []listener{dying.listener(), other.listener()}, time.Second)
	if !errors.Is(err, boom) || errors.Is(err, ErrForcedClose) {
		t.Fatalf("serve = %v, want the failure from the drain", err)
	}
}

func TestServeFailureNamesItsListener(t *testing.T) {
	bad := newFake("approval")
	bad.runErr = errors.New("bind failed")
	err := serve(context.Background(), []listener{newFake("MCP").listener(), bad.listener()}, time.Second)
	if err == nil || !errors.Is(err, bad.runErr) || err.Error() == bad.runErr.Error() || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("serve = %v, want the failure wrapped with the listener's name", err)
	}
}

// A failure still beats the forced close: the other listener's drain running out of time is not
// what Serve reports.
func TestServeFailureBeatsForcedClose(t *testing.T) {
	bad, slow := newFake("approval"), newFake("MCP")
	bad.runErr, slow.hang = errors.New("bind failed"), true
	err := serve(context.Background(), []listener{bad.listener(), slow.listener()}, 50*time.Millisecond)
	if !errors.Is(err, bad.runErr) || errors.Is(err, ErrForcedClose) {
		t.Fatalf("serve = %v, want the failure", err)
	}
	if slow.closes.Load() != 1 {
		t.Errorf("the hung listener was closed %d times, want 1", slow.closes.Load())
	}
}

func TestServeClosedServersAreNotFailures(t *testing.T) {
	a, b := newFake("approval"), newFake("MCP")
	if err := serve(cancelled(), []listener{a.listener(), b.listener()}, time.Second); err != nil {
		t.Fatalf("serve = %v, want nil: http.ErrServerClosed from a server serve shut down is no failure", err)
	}
	if a.shutdowns.Load() != 1 || b.shutdowns.Load() != 1 || a.closes.Load()+b.closes.Load() != 0 {
		t.Errorf("shutdowns %d and %d, closes %d: want one shutdown each and no close",
			a.shutdowns.Load(), b.shutdowns.Load(), a.closes.Load()+b.closes.Load())
	}
}

func TestServeShutdownHasTheGraceDeadline(t *testing.T) {
	a, b := newFake("approval"), newFake("MCP")
	began := time.Now()
	if err := serve(cancelled(), []listener{a.listener(), b.listener()}, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*fake{a, b} {
		if !f.sawDeadline || f.deadline.Before(began.Add(2*time.Second)) || f.deadline.After(began.Add(4*time.Second)) {
			t.Errorf("%s: shutdown deadline %v (set %t), want about 3 s after the start", f.name, f.deadline.Sub(began), f.sawDeadline)
		}
	}
}

func TestServeForcedCloseClosesBoth(t *testing.T) {
	a, b := newFake("approval"), newFake("MCP")
	a.hang = true // only one drain hangs; both are still cut
	err := serve(cancelled(), []listener{a.listener(), b.listener()}, 50*time.Millisecond)
	if !errors.Is(err, ErrForcedClose) {
		t.Fatalf("serve = %v, want ErrForcedClose", err)
	}
	if a.closes.Load() != 1 || b.closes.Load() != 1 {
		t.Errorf("closes %d and %d, want 1 each", a.closes.Load(), b.closes.Load())
	}
}
