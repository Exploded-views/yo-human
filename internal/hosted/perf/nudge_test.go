package perf

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
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
// only the events after its cursor. A millisecond of slack covers timer noise, and a loaded host
// gets three tries: a scan of the old events is slower every time, and noise is not.
func TestNudgeTickDoesNotGrowWithOldEvents(t *testing.T) {
	var got, base time.Duration
	for try := range 3 {
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
		if got, base = p95(loaded), p95(empty); got <= 2*base+time.Millisecond {
			return
		}
		t.Logf("try %d: p95 with 100,000 older events = %v, empty = %v", try+1, got, base)
	}
	t.Errorf("p95 with 100,000 older events = %v, empty = %v on every try: a tick scans more than the events after its cursor", got, base)
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

func TestQuietChannelAcceptsEverything(t *testing.T) {
	var q quiet
	if q.Name() != "quiet" || len(q.Levels()) == 0 || q.Send(context.Background(), notify.Message{}) != nil {
		t.Error("the scenario's channel must accept every message")
	}
}

// A step that fails is an error, never a sample: the database is closed before each step in turn.
func TestNudgeScenarioReportsEachStepThatFails(t *testing.T) {
	for _, at := range []string{"events", "first", "post", "tick"} {
		_, err := nudgeStaged(filepath.Join(t.TempDir(), at), 5, 3, 2, func(stage string, d *db.DB) {
			if stage == at {
				_ = d.Close()
			}
		})
		if err == nil {
			t.Errorf("closing the database before %q gave no error", at)
		}
	}
}
