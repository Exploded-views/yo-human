package perf

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func init() { nudgeOldEvents = 50 } // plumbing tests need no 100,000 events; the budget test says so itself

var samplers = map[string]sampler{"post": PostIn, "revise": ReviseIn, "wake": WakeIn, "nudge": NudgeIn}

func TestSeedItemsIsTheBudgetLine(t *testing.T) {
	if SeedItems != 10000 {
		t.Errorf("SeedItems = %d: the budget line is a 10,000-item database", SeedItems)
	}
}

func TestSamplersTimeEachCall(t *testing.T) {
	for name, fn := range samplers {
		got, err := fn(filepath.Join(t.TempDir(), "data"), 25, 4) // db.Open makes the missing directory 0700
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 4 {
			t.Fatalf("%s: %d samples, want 4", name, len(got))
		}
		for _, d := range got {
			if d <= 0 {
				t.Errorf("%s: a sample of %v", name, d)
			}
		}
	}
}

func TestSamplersReportSetupErrors(t *testing.T) {
	// A directory under a regular file can't be made.
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, fn := range samplers {
		if _, err := fn(filepath.Join(file, "data"), 5, 1); err == nil {
			t.Errorf("%s: no error for an unusable directory", name)
		}
	}
}

func TestRunPrintsOneTimingPerLine(t *testing.T) {
	old := seedItems
	seedItems = 20
	t.Cleanup(func() { seedItems = old })
	for _, kind := range []string{"post", "revise", "wake", "nudge"} {
		var out, errOut bytes.Buffer
		if code := Run([]string{kind, "3"}, &out, &errOut); code != 0 {
			t.Fatalf("%s: code %d, stderr %q", kind, code, errOut.String())
		}
		lines := strings.Fields(out.String())
		if len(lines) != 3 {
			t.Fatalf("%s: %d lines, want 3: %q", kind, len(lines), out.String())
		}
		for _, l := range lines {
			if ns, err := strconv.ParseInt(l, 10, 64); err != nil || ns <= 0 {
				t.Errorf("%s: line %q is not a positive integer", kind, l)
			}
		}
	}
}

func TestRunReportsAMeasurementFailure(t *testing.T) {
	old := tmpRoot
	tmpRoot = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() { tmpRoot = old })
	var out, errOut bytes.Buffer
	if code := Run([]string{"post", "1"}, &out, &errOut); code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "perf:") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

func TestRunRefusesBadArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"post"}, {"post", "0"}, {"post", "x"}, {"sleep", "3"}, {"post", "3", "4"}} {
		var out, errOut bytes.Buffer
		if code := Run(args, &out, &errOut); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "usage") {
			t.Errorf("Run(%v) = %d, stdout %q, stderr %q", args, code, out.String(), errOut.String())
		}
	}
}

func TestSamplersOnTheRealSize(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds 10,000 items")
	}
	for name, fn := range map[string]func(int) ([]time.Duration, error){"post": Post, "revise": Revise, "wake": Wake} {
		got, err := fn(2)
		if err != nil || len(got) != 2 {
			t.Errorf("%s: %v, %d samples", name, err, len(got))
		}
	}
}

// A scenario that times the wrong operation measures nothing: every timed Post must have done
// what the scenario says (created an item, or revised it).
func TestTimePostsRefusesTheWrongOutcome(t *testing.T) {
	b, closeDB, err := seeded(filepath.Join(t.TempDir(), "data"), 5)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	same := func(int) bus.Draft { return perfDraft("one-handle", "same content") }
	if _, err := timePosts(b, 1, same, bus.OutcomeCreated); err != nil {
		t.Fatalf("first post: %v", err)
	}
	if _, err := timePosts(b, 1, same, bus.OutcomeCreated); err == nil {
		t.Error("an unchanged repost was timed as a creation")
	}
	if _, err := timePosts(b, 1, same, bus.OutcomeRevised); err == nil {
		t.Error("an unchanged repost was timed as a revision")
	}
}

// seeded makes the one-off post that follows the bulk seed itself, so the first Post a scenario
// times is already a steady-state one: the warm-up draft is already there (unchanged).
func TestSeededWarmsUpBeforeReturning(t *testing.T) {
	b, closeDB, err := seeded(filepath.Join(t.TempDir(), "data"), 5)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	res, err := b.Post(context.Background(), perfNode, perfDraft("warm-up", "Warm-up"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != bus.OutcomeUnchanged {
		t.Errorf("first Post of the warm-up draft = %s, want %s: seeded must make the one-off post itself", res.Outcome, bus.OutcomeUnchanged)
	}
}

// A warm-up that fails is an error from the seed, not a bus that silently skipped it.
func TestSeededReportsAWarmUpFailure(t *testing.T) {
	b, closeDB, err := seededWarm(filepath.Join(t.TempDir(), "data"), 5, perfDraft("warm-up", ""))
	if err == nil {
		closeDB()
		t.Fatalf("seededWarm with an invalid warm-up draft succeeded (bus %v), want its error", b)
	}
}

// The warm-up has to come after the seed: its event takes the sequence number after all the seed
// events, since a warm-up before the seed would not absorb the post-seed cost.
func TestWarmUpFollowsTheSeed(t *testing.T) {
	const items = 5
	b, closeDB, err := seeded(filepath.Join(t.TempDir(), "data"), items)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	res, err := b.Post(context.Background(), perfNode, perfDraft("warm-up", "Warm-up"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.LastSeq != items+1 {
		t.Errorf("warm-up event seq = %d, want %d: the warm-up must follow all %d seed events", res.Item.LastSeq, items+1, items)
	}
}

// The wake sampler times a waiter, not the post: each sample is how long a blocked Answers took
// to return after the commit that woke it, and it must have returned what the commit made.
func TestWakeSamplesAreTheWaitersLatency(t *testing.T) {
	got, err := WakeIn(filepath.Join(t.TempDir(), "data"), 25, 5)
	if err != nil || len(got) != 5 {
		t.Fatalf("%v, %d samples", err, len(got))
	}
	for _, d := range got {
		if d <= 0 || d > 5*time.Second {
			t.Errorf("a sample of %v: a waiter that waited out its timeout measures nothing", d)
		}
	}
}

func TestUsageNamesWake(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"sleep", "3"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "wake") {
		t.Errorf("code %d, stderr %q: the usage line must name wake", code, errOut.String())
	}
}

// A waiter that returns something other than the one post it was waiting for measures the wrong
// operation, and a post that fails is an error, not a sample.
func TestWakeOnceRefusesTheWrongOperation(t *testing.T) {
	b, closeDB, err := seeded(filepath.Join(t.TempDir(), "data"), 5)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	// From cursor 0 the waiter reads the seed's events as well, so it returns more than the post.
	if _, _, err := wakeOnce(context.Background(), b, 0, "wake-x"); err == nil || !strings.Contains(err.Error(), "wrong operation") {
		t.Errorf("err = %v, want the wrong-operation refusal", err)
	}
	cursor, err := drain(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := wakeOnce(context.Background(), b, cursor, strings.Repeat("h", bus.MaxHandle+1)); err == nil {
		t.Error("a post the bus refuses was timed")
	}
}

func TestDrainReturnsTheEndOfTheSwarm(t *testing.T) {
	b, closeDB, err := seeded(filepath.Join(t.TempDir(), "data"), 250)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	cursor, err := drain(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	u, err := b.Answers(context.Background(), perfNode, cursor, 0)
	if err != nil || len(u.Events) != 0 {
		t.Errorf("after drain: %+v, %v", u, err)
	}
	if cursor == 0 {
		t.Error("drain stayed at 0 over a seeded swarm")
	}
}
