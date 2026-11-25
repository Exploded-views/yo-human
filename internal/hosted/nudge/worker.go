// Package nudge is hosted mode's delivery worker: the collapsed nudge ("N new, M waiting for
// Claude", at most once per window) and the outbox pass that sends the messages that can't wait.
// It reaches state only through bus.Store and the network only through a notify.Channel.
//
// Assumptions, stated: one worker per database (the bus is one process on one machine), and
// ticks are serialised: Kick, the interval and direct Tick calls all pass through one lock, so
// two ticks never overlap.
package nudge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

// The clocks the plan sets: how often Run ticks on its own, the owner-push cap per item, the
// spacing of one swarm's pages, how often a broken channel is probed, how many outbox rows one
// tick takes, how young a refused row must be to get one more try after a restart, and the
// retry backoff (doubling from the base to the ceiling).
const (
	tickEvery   = 30 * time.Second
	pushWindow  = time.Hour
	pageSpacing = 10 * time.Minute
	probeEvery  = 5 * time.Minute
	outboxBatch = 10
	requeueAge  = 30 * time.Minute
	backoffBase = 30 * time.Second
	backoffMax  = 5 * time.Minute
)

// Config is the nudge's settings. Level is the nudge's level: H1 refuses anything louder than
// active.
type Config struct {
	Window      time.Duration
	MinSeverity bus.Severity
	Level       notify.Level
	Log         *slog.Logger
}

var severityRank = map[bus.Severity]int{bus.SeverityLow: 0, bus.SeverityNormal: 1, bus.SeverityHigh: 2, bus.SeverityCritical: 3}

// Validate refuses a Config the worker will not run with: a window that is not positive, a
// minimum severity that is not one of the four, and any level but passive or active.
func (c Config) Validate() error {
	if _, ok := severityRank[c.MinSeverity]; !ok {
		return errors.New("nudge: the minimum severity must be low, normal, high or critical")
	}
	if c.Window <= 0 {
		return errors.New("nudge: the window must be positive")
	}
	if c.Level != notify.LevelPassive && c.Level != notify.LevelActive {
		return errors.New("nudge: the nudge level must be passive or active in H1")
	}
	return nil
}

// Worker sends what is owed to the owner's phone.
type Worker struct {
	b      *bus.Bus
	s      bus.Store
	ch     notify.Channel
	cfg    Config
	cfgErr error
	now    func() time.Time
	log    *slog.Logger

	interval time.Duration
	kick     chan struct{}

	mu        sync.Mutex // one tick at a time
	started   bool       // the first tick has requeued the young refused rows
	broken    bool       // the delivery flag as this tick read it
	lastProbe time.Time  // in memory on purpose: a new worker probes at once
}

// NewWorker returns a worker over the bus b and its store s, sending through ch. A Config that
// fails Validate makes every Tick return that error and send nothing.
func NewWorker(b *bus.Bus, s bus.Store, ch notify.Channel, cfg Config, now func() time.Time) *Worker {
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Worker{b: b, s: s, ch: ch, cfg: cfg, cfgErr: cfg.Validate(), now: now, log: log, interval: tickEvery, kick: make(chan struct{}, 1)}
}

// Kick asks Run to tick now. It never blocks, and kicks that pile up while a tick runs become
// one more tick.
func (w *Worker) Kick() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Run ticks once at start, then on every Kick and every interval, until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	tk := time.NewTicker(w.interval)
	defer tk.Stop()
	for {
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			w.log.Error("tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-tk.C:
		}
	}
}

// Tick runs one pass: the nudge, then the outbox. While the channel is broken it sends only a
// probe. An error is a store failure: what a channel refused is handled, not returned.
func (w *Worker) Tick(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cfgErr != nil {
		return w.cfgErr
	}
	now := w.now().UTC()
	if !w.started {
		if err := w.requeueFailed(ctx, now); err != nil {
			return err
		}
		w.started = true
	}
	flag, err := w.readFlag(ctx)
	if err != nil {
		return err
	}
	if w.broken = flag.Raised; w.broken {
		if err := w.probe(ctx, now); err != nil || w.broken {
			return err
		}
	}
	if err := w.nudge(ctx, now); err != nil || w.broken {
		return err
	}
	_, err = w.outbox(ctx, now, false)
	return err
}

// probe is a broken channel's tick: at most every probeEvery, the oldest due outbox row the cap
// allows, or failing that a due nudge. The first 2xx from either clears the flag.
func (w *Worker) probe(ctx context.Context, now time.Time) error {
	if now.Sub(w.lastProbe) < probeEvery {
		return nil
	}
	w.lastProbe = now
	attempted, err := w.outbox(ctx, now, true)
	if err != nil || attempted {
		return err
	}
	return w.nudge(ctx, now)
}

func (w *Worker) readFlag(ctx context.Context) (f bus.BrokenFlag, err error) {
	err = w.s.Tx(ctx, func(x bus.Tx) (err error) { f, err = x.BrokenFlag(); return })
	return f, err
}

// raise sets the sticky flag, with the time and status of this refusal: a refused probe
// re-raises it with the new time.
func (w *Worker) raise(ctx context.Context, now time.Time, status int) error {
	w.broken, w.lastProbe = true, now // the next probe is probeEvery from this refusal
	return w.s.Tx(ctx, func(x bus.Tx) error {
		return x.PutBrokenFlag(bus.BrokenFlag{Raised: true, Since: now, Status: status})
	})
}

// clearIn clears the flag in x when this worker saw it raised. Only a 2xx calls it.
func (w *Worker) clearIn(x bus.Tx) error {
	if !w.broken {
		return nil
	}
	w.broken = false
	return x.PutBrokenFlag(bus.BrokenFlag{})
}

// requeueFailed gives each message refused less than requeueAge ago, and not yet retried, one
// more try: a refusal that was the channel's fault is usually fixed by the restart that follows.
func (w *Worker) requeueFailed(ctx context.Context, now time.Time) error {
	return w.s.Tx(ctx, func(x bus.Tx) error {
		rows, err := x.FailedSince(now.Add(-requeueAge))
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.Retried {
				continue
			}
			r.State, r.Retried, r.NextAt = bus.OutboxPending, true, now
			if err := x.UpdateOutbox(r); err != nil {
				return err
			}
		}
		return nil
	})
}
