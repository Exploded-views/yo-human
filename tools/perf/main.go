// Command perf runs each scenario several times and fails when its p95 wall
// time exceeds the budget in perf-budgets.json. CI=true applies the headroom.
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

// Scenario is a named command whose wall time is budgeted.
type Scenario struct {
	Name  string
	Argv  []string
	Env   []string               // extra environment for the timed command; $DIR becomes the temp dir
	Setup func(dir string) error // run once into the temp dir, before timing
	Exit  int                    // the exit code the command is expected to return; zero unless it is a failure path
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
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, scenarios, os.Getenv("CI") == "true"))
}

func run(args []string, stdout, stderr io.Writer, scs []Scenario, ci bool) int {
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
	results := map[string][]time.Duration{}
	for _, s := range scs {
		samples, err := measure(s, b.Runs)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "perf: %s: %v\n", s.Name, err)
			return 2
		}
		results[s.Name] = samples
		_, _ = fmt.Fprintf(stdout, "%s: p95 %s, budget %dms (ci=%t)\n", s.Name, ms(P95(samples)), b.Budgets[s.Name].P95Ms, ci)
	}
	vs = append(vs, Check(results, b, ci)...)
	for _, v := range vs {
		_, _ = fmt.Fprintln(stdout, v)
	}
	if len(vs) > 0 {
		return 1
	}
	return 0
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
// once, into a fresh temp dir that is removed afterwards, before the first timed run; the dir
// stands in for $DIR in Env.
func measure(s Scenario, n int) (samples []time.Duration, err error) {
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
	samples = make([]time.Duration, 0, n)
	for range n {
		cmd := exec.Command(s.Argv[0], s.Argv[1:]...) //nolint:gosec // argv comes from the scenarios slice
		cmd.Env = env
		start := time.Now()
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		if err := cmd.Wait(); !exitedAs(err, s.Exit) {
			if err == nil {
				err = fmt.Errorf("exited 0, want exit code %d", s.Exit)
			}
			return nil, err
		}
		samples = append(samples, time.Since(start))
	}
	return samples, nil
}
