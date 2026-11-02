package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scripted stands in for measure: each call for a scenario returns its next scripted p95 as every sample.
type scripted struct {
	p95   map[string][]int // ms, one per call
	calls map[string]int
}

func (s *scripted) measure(sc Scenario, n int) ([]time.Duration, error) {
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	i := s.calls[sc.Name]
	s.calls[sc.Name]++
	out := make([]time.Duration, n)
	for j := range out {
		out[j] = ms100(s.p95[sc.Name][i])
	}
	return out, nil
}

// 30ms budget, 2.0 headroom: 60ms in CI.
const heldBudgets = `{"runs": 3, "ci_headroom": 2.0, "budgets": {"x": {"p95_ms": 30}}}`

func runScripted(t *testing.T, sc *scripted, ci bool) (code int, stdout string) {
	t.Helper()
	var out, errb strings.Builder
	code = runWith([]string{"--budgets", writeBudgets(t, heldBudgets)}, &out, &errb, []Scenario{{Name: "x"}}, ci, sc.measure)
	return code, out.String()
}

func TestConsistentlySlowScenarioStillFailsNamingBothP95s(t *testing.T) {
	sc := &scripted{p95: map[string][]int{"x": {215, 90}}}
	code, out := runScripted(t, sc, true)
	if code != 1 || sc.calls["x"] != 2 {
		t.Fatalf("exit %d after %d measurements, output %q; want exit 1 after 2", code, sc.calls["x"], out)
	}
	if !strings.Contains(out, "x: p95 90.0ms > budget 60.0ms") || !strings.Contains(out, "215.0ms") {
		t.Errorf("output %q; want a violation naming the re-measured p95 90.0ms and the first, 215.0ms", out)
	}
}

func TestOneOffSpikePassesAndBothMeasurementsAreShown(t *testing.T) {
	sc := &scripted{p95: map[string][]int{"x": {215, 7}}}
	code, out := runScripted(t, sc, true)
	if code != 0 || sc.calls["x"] != 2 {
		t.Fatalf("exit %d after %d measurements, output %q; want exit 0 after 2", code, sc.calls["x"], out)
	}
	for _, want := range []string{
		"x: p95 215.0ms, budget 30ms (ci=true)",
		"x: over budget, re-measured: p95 7.0ms (passes; the first miss was probably a runner stall)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q lacks %q", out, want)
		}
	}
}

func TestWithinBudgetIsMeasuredOnce(t *testing.T) {
	sc := &scripted{p95: map[string][]int{"x": {60}}}
	if code, out := runScripted(t, sc, true); code != 0 || sc.calls["x"] != 1 {
		t.Errorf("exit %d after %d measurements, output %q; want exit 0 after 1", code, sc.calls["x"], out)
	}
}

func TestOverBudgetWithoutHeadroomIsAlsoRemeasuredOnce(t *testing.T) {
	sc := &scripted{p95: map[string][]int{"x": {31, 31, 31}}}
	if code, _ := runScripted(t, sc, false); code != 1 || sc.calls["x"] != 2 {
		t.Errorf("exit %d after %d measurements; want exit 1 after exactly 2", code, sc.calls["x"])
	}
}

func TestStepSummaryGetsOneLineForARemeasuredScenario(t *testing.T) {
	file := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", file)
	sc := &scripted{p95: map[string][]int{"x": {215, 7}}}
	runScripted(t, sc, true)
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "215.0ms") || !strings.Contains(lines[0], "7.0ms") || !strings.Contains(lines[0], "x") {
		t.Errorf("summary %q; want one line naming x and both p95s", b)
	}
}

func TestStepSummaryIsUntouchedWithoutARemeasureOrWithoutTheVariable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", file)
	runScripted(t, &scripted{p95: map[string][]int{"x": {10}}}, true)
	if _, err := os.Stat(file); err == nil {
		t.Error("summary written although nothing was re-measured")
	}
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	dir := t.TempDir()
	t.Chdir(dir)
	runScripted(t, &scripted{p95: map[string][]int{"x": {215, 7}}}, true)
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("files written with GITHUB_STEP_SUMMARY unset: %v", ents)
	}
}

func TestEachScenarioWarmsUpOnceUntimed(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	sc := Scenario{Name: "w", Argv: []string{"sh", "-c", "echo x >> " + log}}
	samples, err := measure(sc, 3)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if got := strings.Count(string(b), "\n"); got != 4 || len(samples) != 3 {
		t.Errorf("%d runs logged, %d samples; want 4 runs (1 warm-up + 3 timed) and 3 samples", got, len(samples))
	}
}

func TestAWarmUpThatExitsWrongIsAnError(t *testing.T) {
	sc := Scenario{Name: "w", Argv: []string{"sh", "-c", "exit 3"}, Exit: 64}
	if _, err := measure(sc, 1); err == nil {
		t.Error("measure succeeded although the command exits 3, want 64")
	}
}

func TestAFailedRemeasureIsAnErrorNamingTheScenario(t *testing.T) {
	calls := 0
	m := func(Scenario, int) ([]time.Duration, error) {
		calls++
		if calls == 2 {
			return nil, os.ErrInvalid
		}
		return []time.Duration{ms100(215)}, nil
	}
	var out, errb strings.Builder
	code := runWith([]string{"--budgets", writeBudgets(t, heldBudgets)}, &out, &errb, []Scenario{{Name: "x"}}, true, m)
	if code != 2 || !strings.Contains(errb.String(), "x") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming x", code, errb.String())
	}
}

func TestAnUnwritableStepSummaryIsReportedButDoesNotChangeTheVerdict(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", t.TempDir()) // a directory, so it cannot be opened for append
	sc := &scripted{p95: map[string][]int{"x": {215, 7}}}
	var out, errb strings.Builder
	code := runWith([]string{"--budgets", writeBudgets(t, heldBudgets)}, &out, &errb, []Scenario{{Name: "x"}}, true, sc.measure)
	if code != 0 || !strings.Contains(errb.String(), "step summary") {
		t.Errorf("exit %d, stderr %q; want exit 0 with the summary failure on stderr", code, errb.String())
	}
}
