package perf

import (
	"path/filepath"
	"time"
)

// NudgeOldEvents is how many events the nudge scenario's database holds before its cursor.
const NudgeOldEvents = 100000

// NudgeIn times n nudge ticks against a database of items items and NudgeOldEvents older events.
func NudgeIn(dir string, items, n int) ([]time.Duration, error) { return nil, nil }

func nudgeIn(dir string, items, oldEvents, n int) ([]time.Duration, error) {
	_ = filepath.Join
	return nil, nil
}
