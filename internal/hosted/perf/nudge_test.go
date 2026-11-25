package perf

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func p95(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[(len(s)*95+99)/100-1]
}

func TestNudgeEventsIsTheBudgetLine(t *testing.T) {
	if NudgeOldEvents != 100000 {
		t.Errorf("NudgeOldEvents = %d: the budget line is 100,000 older events", NudgeOldEvents)
	}
}

// With 100,000 older events a tick's p95 stays within 2x of the empty database's: a tick reads
// only the events after its cursor. A millisecond of slack covers timer noise on a loaded host.
func TestNudgeTickDoesNotGrowWithOldEvents(t *testing.T) {
	empty, err := nudgeIn(filepath.Join(t.TempDir(), "a"), 100, 0, 40)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := nudgeIn(filepath.Join(t.TempDir(), "b"), 100, NudgeOldEvents, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 40 || len(loaded) != 40 {
		t.Fatalf("%d and %d samples, want 40 each", len(empty), len(loaded))
	}
	if got, base := p95(loaded), p95(empty); got > 2*base+time.Millisecond {
		t.Errorf("p95 with 100,000 older events = %v, empty = %v: a tick scans more than the events after its cursor", got, base)
	}
}

func TestNudgeSamplesAreEachATickWithAnEventToRead(t *testing.T) {
	got, err := nudgeIn(filepath.Join(t.TempDir(), "c"), 20, 50, 5)
	if err != nil || len(got) != 5 {
		t.Fatalf("samples = %v, %v, want 5", got, err)
	}
	for _, d := range got {
		if d <= 0 {
			t.Errorf("a sample of %v", d)
		}
	}
}
