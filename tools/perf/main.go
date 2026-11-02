// Command perf runs each scenario several times and fails when its p95 wall
// time exceeds the budget in perf-budgets.json. CI=true applies the headroom.
//
// Each command scenario first runs once untimed, so cold caches do not count. A scenario whose p95
// is over budget is measured once more, from a fresh temp dir; it fails only if that is over
// budget too, so a one-off runner stall passes while a real regression fails both. Both p95s are
// printed, and appended to $GITHUB_STEP_SUMMARY when it is set.
//
// Exit codes: 0 within budget, 1 over budget, 2 usage or a scenario that could not run.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Scenario is a named measurement whose p95 is budgeted: a command whose wall time is taken, or,
// when Sample is set, a function that returns n timings itself (the hosted scenarios, which time
// one call against a seeded database rather than a process start).
type Scenario struct {
	Name   string
	Argv   []string
	Env    []string                             // extra environment for the timed command; $DIR becomes the temp dir
	Setup  func(dir string) error               // run once into the temp dir, before timing
	Exit   int                                  // the exit code the command is expected to return; zero unless it is a failure path
	Sample func(n int) ([]time.Duration, error) // when set, returns the n timings itself and Argv is not run
}

// fixtureEnv is a fixture home, so a run reads neither the developer's config nor their state.
var fixtureEnv = []string{"HOME=$DIR", "XDG_CONFIG_HOME=$DIR/config", "XDG_STATE_HOME=$DIR/state", "YO_HUMAN_CONFIG="}

// notifyArgv is a notify call that sends nothing over the network: it is held, suppressed or refused.
func notifyArgv(rest ...string) []string {
	return append([]string{"dist/yo-human", "notify"}, rest...)
}

var scenarios = []Scenario{
	{Name: "version-startup", Argv: []string{"dist/yo-human", "version"}},
	{Name: "levels", Argv: []string{"dist/yo-human", "levels"}, Setup: levelsFixture, Env: fixtureEnv},
	{Name: "status", Argv: []string{"dist/yo-human", "status"}, Setup: levelsFixture, Env: fixtureEnv},
	// A call with no title is refused before the config is read.
	{Name: "notify-usage-error", Argv: notifyArgv("--level", "note"), Env: fixtureEnv, Exit: 64},
	// Inside a quiet window that never ends, a level that holds waits in the queue, with no network.
	{Name: "notify-held-local", Argv: notifyArgv("--level", "night", "--title", "perf", "--ask", "look"), Setup: heldFixture, Env: fixtureEnv},
	// A quieter repeat of an open incident is recorded and sends nothing.
	{Name: "notify-suppressed", Argv: notifyArgv("--level", "note", "--title", "perf", "--dedup-key", perfKey), Setup: suppressedFixture, Env: fixtureEnv},
	{Name: "hosted-post", Sample: hostedSample("post")},
	{Name: "hosted-revise", Sample: hostedSample("revise")},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, scenarios, os.Getenv("CI") == "true"))
}

func run(args []string, stdout, stderr io.Writer, scs []Scenario, ci bool) int {
	return runWith(args, stdout, stderr, scs, ci, measure)
}

// measureFunc takes n timings of a scenario. runWith takes it so a test can stand in for real processes.
type measureFunc func(s Scenario, n int) ([]time.Duration, error)

func runWith(args []string, stdout, stderr io.Writer, scs []Scenario, ci bool, measure measureFunc) int {
	fs := flag.NewFlagSet("perf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("budgets", "perf-budgets.json", "budgets file")
	binPath := fs.String("binary", "dist/yo-human", "the built binary whose size is budgeted")
	basePath := fs.String("base-budgets", "", "budgets file from the merge base; raising a budget or headroom, lowering runs, or removing a budget fails")
	if err := fs.Parse(args); err != nil {
		_, _ = fmt.Fprintln(stderr, "usage: perf [--budgets F] [--base-budgets F] [--binary F]")
		return 2
	}
	b, err := ReadBudgets(*path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "perf:", err)
		return 2
	}
	var vs []Violation
	if *basePath != "" {
		base, err := ReadBudgets(*basePath)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "perf:", err)
			return 2
		}
		vs = CheckBase(b, base)
	}
	sized, err := weigh(stdout, *binPath, b.BinaryBytes)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "perf: binary:", err)
		return 2
	}
	vs = append(vs, sized...)
	results, firstMiss, err := measureAll(stdout, stderr, scs, b, ci, measure)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "perf:", err)
		return 2
	}
	vs = append(vs, WithFirstMiss(Check(results, b, ci), firstMiss)...)
	for _, v := range vs {
		_, _ = fmt.Fprintln(stdout, v)
	}
	if len(vs) > 0 {
		return 1
	}
	return 0
}

// measureAll measures every scenario, re-measuring a budget miss once, and returns the samples
// each scenario is judged by plus the first p95 of each re-measured one.
func measureAll(stdout, stderr io.Writer, scs []Scenario, b Budgets, ci bool, measure measureFunc) (map[string][]time.Duration, map[string]time.Duration, error) {
	results := map[string][]time.Duration{}
	firstMiss := map[string]time.Duration{}
	for _, s := range scs {
		samples, first, err := measureOne(stdout, stderr, s, b, ci, measure)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		if first > 0 {
			firstMiss[s.Name] = first
		}
		results[s.Name] = samples
	}
	return results, firstMiss, nil
}

// measureOne measures s and, when it is over budget, once more, fresh: a miss must reproduce. It
// returns the samples s is judged by, and the first p95 when it was re-measured (zero otherwise).
func measureOne(stdout, stderr io.Writer, s Scenario, b Budgets, ci bool, measure measureFunc) ([]time.Duration, time.Duration, error) {
	samples, err := measure(s, b.Runs)
	if err != nil {
		return nil, 0, err
	}
	budgetMs := b.Budgets[s.Name].P95Ms
	_, _ = fmt.Fprintf(stdout, "%s: p95 %s, budget %dms (ci=%t)\n", s.Name, ms(P95(samples)), budgetMs, ci)
	if !OverBudget(s.Name, samples, b, ci) {
		return samples, 0, nil
	}
	first := P95(samples)
	if samples, err = measure(s, b.Runs); err != nil {
		return nil, 0, err
	}
	again := OverBudget(s.Name, samples, b, ci)
	_, _ = fmt.Fprintf(stdout, "%s: over budget, re-measured: p95 %s (%s)\n", s.Name, ms(P95(samples)), remeasureVerdict(again))
	if err := appendSummary(os.Getenv("GITHUB_STEP_SUMMARY"), s.Name, first, P95(samples), budgetMs, again); err != nil {
		_, _ = fmt.Fprintln(stderr, "perf: step summary:", err)
	}
	return samples, first, nil
}

func remeasureVerdict(stillOver bool) string {
	if stillOver {
		return "still over budget"
	}
	return "passes; the first miss was probably a runner stall"
}

// appendSummary adds one Markdown line about a re-measured scenario to the file at path, which is
// $GITHUB_STEP_SUMMARY. An empty path writes nothing.
func appendSummary(path, name string, first, second time.Duration, budgetMs int, stillOver bool) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	outcome := "passed on the re-measure; the first miss was probably a runner stall"
	if stillOver {
		outcome = "FAILED: over budget on the re-measure too"
	}
	_, werr := fmt.Fprintf(f, "- perf `%s`: first p95 %s, re-measured p95 %s, budget %dms: %s\n", name, ms(first), ms(second), budgetMs, outcome)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// weigh checks the binary at path against a size budget of max bytes, printing its size; with no
// budget (zero) it checks nothing.
func weigh(stdout io.Writer, path string, max int64) ([]Violation, error) {
	if max == 0 {
		return nil, nil
	}
	vs, size, err := CheckSize(path, max)
	if err == nil {
		_, _ = fmt.Fprintf(stdout, "binary: %d bytes, budget %d bytes\n", size, max)
	}
	return vs, err
}

// exitedAs reports whether a finished command ended with the exit code want (zero: no error).
func exitedAs(err error, want int) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == want
	}
	return err == nil && want == 0
}

// measure runs the scenario n times and returns each run's wall time, start to Wait. Setup runs
// once, into a fresh temp dir that is removed afterwards, then one untimed warm-up run (which must
// exit as expected), then the timed runs; the dir stands in for $DIR in Env.
func measure(s Scenario, n int) (samples []time.Duration, err error) {
	if s.Sample != nil {
		return s.Sample(n)
	}
	dir, err := os.MkdirTemp("", "perf-"+s.Name+"-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if s.Setup != nil {
		if err := s.Setup(dir); err != nil {
			return nil, fmt.Errorf("setup: %w", err)
		}
	}
	env := os.Environ()
	for _, e := range s.Env {
		env = append(env, strings.ReplaceAll(e, "$DIR", dir))
	}
	// Run 0 is the warm-up: it must exit as expected, but is not kept.
	samples = make([]time.Duration, 0, n)
	for i := 0; i <= n; i++ {
		d, err := timeOnce(s, env)
		if err != nil {
			if i == 0 {
				err = fmt.Errorf("warm-up: %w", err)
			}
			return nil, err
		}
		if i > 0 {
			samples = append(samples, d)
		}
	}
	return samples, nil
}

// timeOnce runs the scenario's command once and returns its wall time, start to Wait.
func timeOnce(s Scenario, env []string) (time.Duration, error) {
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...) //nolint:gosec // argv comes from the scenarios slice
	cmd.Env = env
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	if err := cmd.Wait(); !exitedAs(err, s.Exit) {
		if err == nil {
			err = fmt.Errorf("exited 0, want exit code %d", s.Exit)
		}
		return 0, err
	}
	return time.Since(start), nil
}
