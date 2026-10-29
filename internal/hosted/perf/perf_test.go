package perf

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var samplers = map[string]sampler{"post": PostIn, "revise": ReviseIn}

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
	for _, kind := range []string{"post", "revise"} {
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
	for name, fn := range map[string]func(int) ([]time.Duration, error){"post": Post, "revise": Revise} {
		got, err := fn(2)
		if err != nil || len(got) != 2 {
			t.Errorf("%s: %v, %d samples", name, err, len(got))
		}
	}
}
