package nudge

import "time"

// SetInterval changes how often Run ticks without a Kick.
func (w *Worker) SetInterval(d time.Duration) { w.interval = d }
