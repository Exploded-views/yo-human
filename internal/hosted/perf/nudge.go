package perf

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
	"github.com/Exploded-views/yo-human/internal/hosted/nudge"
)

// NudgeOldEvents is how many events the nudge scenario's database holds before its cursor.
const NudgeOldEvents = 100000

// NudgeIn times n nudge ticks against a database of items items and NudgeOldEvents older events.
func NudgeIn(dir string, items, n int) ([]time.Duration, error) {
	return nudgeIn(dir, items, NudgeOldEvents, n)
}

// quiet is a channel that accepts everything: the scenario times the tick, not the network.
type quiet struct{}

func (quiet) Name() string                               { return "quiet" }
func (quiet) Levels() []notify.Level                     { return []notify.Level{notify.LevelActive} }
func (quiet) Send(context.Context, notify.Message) error { return nil }

// nudgeIn is NudgeIn with the number of older events given. A sample is one Tick that has exactly
// one new event to read (a node's post, below the nudge's minimum severity, so nothing is sent),
// after an untimed first tick that moves the cursor past the older events. The window is one
// nanosecond so every tick looks at the events.
func nudgeIn(dir string, items, oldEvents, n int) ([]time.Duration, error) {
	b, closeDB, err := seeded(dir, items)
	if err != nil {
		return nil, err
	}
	defer closeDB()
	d, err := db.Open(filepath.Join(dir, "bus.db")) // a second handle on the seeded file, for the events and the worker
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	ctx := context.Background()
	err = d.Tx(ctx, func(x bus.Tx) error {
		for range oldEvents {
			if _, err := x.AppendEvent(bus.Event{Swarm: "crew", ItemID: "itm_old", Type: bus.EventStatusPushed, Created: time.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	w := nudge.NewWorker(b, d, quiet{}, nudge.Config{Window: time.Nanosecond, MinSeverity: bus.SeverityCritical, Level: notify.LevelActive}, time.Now)
	if err := w.Tick(ctx); err != nil {
		return nil, err
	}
	out := make([]time.Duration, 0, n)
	for i := range n {
		if _, err := b.Post(ctx, perfNode, perfDraft(fmt.Sprintf("tick-%d", i), "Tick")); err != nil {
			return nil, err
		}
		start := time.Now()
		err := w.Tick(ctx)
		out = append(out, max(time.Since(start), time.Nanosecond))
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
