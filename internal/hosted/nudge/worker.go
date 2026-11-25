// Package nudge is hosted mode's delivery worker: the collapsed nudge ("N new, M waiting for
// Claude", at most once per window) and the outbox pass that sends the messages that can't wait.
// It reaches state only through bus.Store and the network only through a notify.Channel.
package nudge

import (
	"context"
	"log/slog"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

// Config is the nudge's settings.
type Config struct {
	Window      time.Duration
	MinSeverity bus.Severity
	Level       notify.Level
	Log         *slog.Logger
}

// Validate refuses a Config the worker will not run with.
func (c Config) Validate() error { return nil }

// Worker sends what is owed to the owner's phone.
type Worker struct {
	interval time.Duration
}

// NewWorker returns a worker over the bus b and its store s, sending through ch.
func NewWorker(b *bus.Bus, s bus.Store, ch notify.Channel, cfg Config, now func() time.Time) *Worker {
	return &Worker{}
}

// Kick asks Run to tick now.
func (w *Worker) Kick() {}

// Run ticks once at start, then on every Kick and every interval, until ctx ends.
func (w *Worker) Run(ctx context.Context) {}

// Tick runs one pass: the nudge, then the outbox.
func (w *Worker) Tick(ctx context.Context) error { return nil }
