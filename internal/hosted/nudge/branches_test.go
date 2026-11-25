package nudge_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/nudge"
)

// noMessages is a store whose threads read as empty: an event whose message cannot be found.
type noMessages struct{ bus.Store }

func (s noMessages) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return s.Store.Tx(ctx, func(x bus.Tx) error { return fn(emptyThread{x}) })
}

type emptyThread struct{ bus.Tx }

func (emptyThread) Messages(string) ([]bus.Message, error) { return nil, nil }

// syncBuf is a log sink that Run's goroutine and the test may both touch.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestAFailedWaitingCountRestoresTheNudge(t *testing.T) {
	r := newRig(t)
	fs := &faultStore{Store: r.d, method: "ListInboxItems", nth: 1}
	r.b = bus.New(fs, r.clk.now, bus.Limits{LinkHosts: []string{"github.com"}})
	r.w = nudge.NewWorker(r.b, r.d, r.ch, r.cfg, r.clk.now)
	r.post(crewNode, question("a"))
	fs.armed.Store(true)
	if err := r.w.Tick(context.Background()); err == nil {
		t.Fatal("Tick returned nil though the waiting count failed")
	}
	if n := len(r.ch.all()); n != 0 {
		t.Fatalf("%d sends, want none", n)
	}
	r.advance(time.Second)
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
}

func TestAMessageWithoutItsEventTimeIsNotCounted(t *testing.T) {
	r := newRig(t)
	r.w = nudge.NewWorker(r.b, noMessages{r.d}, r.ch, r.cfg, r.clk.now)
	th := r.duetThread("t1")
	r.reply(crewNode, th.ID, "hello", bus.SeverityNormal, bus.UrgencyDigest)
	r.advance(time.Minute)
	r.tick()
	if n := len(r.ch.all()); n != 0 {
		t.Errorf("%d sends, want none for a message that cannot be found", n)
	}
}

func TestAFailedRestoreOfADeferredRowIsReported(t *testing.T) {
	r := newRig(t)
	fs := &faultStore{Store: r.d, method: "PutDeferred", nth: 1}
	r.w = nudge.NewWorker(r.b, fs, r.ch, r.cfg, r.clk.now)
	th := r.duetThread("t1")
	r.pushed(th.ID, t0.Add(-20*time.Minute))
	r.reply(crewNode, th.ID, "held", bus.SeverityNormal, bus.UrgencyDigest)
	r.tick() // the message is held: its row is deferred to the end of the hour
	r.advance(2 * time.Hour)
	r.ch.setDefault(statusErr(500))
	fs.armed.Store(true)
	if err := r.w.Tick(context.Background()); err == nil || !fs.hit.Load() {
		t.Fatalf("Tick = %v, hit = %v, want the failed restore reported", err, fs.hit.Load())
	}
}

func TestAFailedClearOfTheStuckRecordIsReported(t *testing.T) {
	r := newRig(t)
	fs := &faultStore{Store: r.d, method: "PutNudgeState", nth: 2}
	r.w = nudge.NewWorker(r.b, fs, r.ch, r.cfg, r.clk.now)
	r.post(crewNode, question("a"))
	r.ch.setDefault(statusErr(500))
	r.tick() // a transient failure: the nudge is stuck
	r.advance(time.Second)
	r.ch.setDefault(nil)
	fs.armed.Store(true)
	if err := r.w.Tick(context.Background()); err == nil || !fs.hit.Load() {
		t.Fatalf("Tick = %v, hit = %v, want the failed clear reported", err, fs.hit.Load())
	}
}

func TestAWorkerWithoutALoggerStillNudges(t *testing.T) {
	r := newRig(t)
	cfg := r.cfg
	cfg.Log = nil
	r.w = nudge.NewWorker(r.b, r.d, r.ch, cfg, r.clk.now)
	r.post(crewNode, question("a"))
	r.tick()
	wantBodies(t, r.ch.delivered(), "1 new, 1 waiting for Claude")
}

func TestRunLogsATickThatFailed(t *testing.T) {
	r := newRig(t)
	logs := &syncBuf{}
	cfg := r.cfg
	cfg.Window = 0 // a Config the worker refuses: every Tick fails
	cfg.Log = slog.New(slog.NewTextHandler(logs, nil))
	w := nudge.NewWorker(r.b, r.d, r.ch, cfg, r.clk.now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "tick failed") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(logs.String(), "tick failed") {
		t.Errorf("logs = %q, want the failed tick", logs.String())
	}
}

func TestPagesOfOneSwarmWithoutAnItemGoOutAsOnePush(t *testing.T) {
	r := newRig(t)
	for i, created := range []time.Duration{time.Minute, 0} { // the lower id is the later row
		r.do(func(x bus.Tx) error {
			_, err := x.InsertOutbox(bus.OutboxRow{Kind: bus.OutboxPage, Swarm: "crew", Level: "active", Title: "page", Body: "b" + string(rune('0'+i)),
				State: bus.OutboxPending, NextAt: t0, Created: t0.Add(created)})
			return err
		})
	}
	r.advance(time.Hour)
	r.tick()
	got := r.pushes()
	if len(got) != 1 || got[0].ID != "yh-1" || got[0].Body != "2 pages for Claude" {
		t.Fatalf("pushes = %+v, want one push yh-1 for 2 pages", got)
	}
}
