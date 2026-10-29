// Command perf runs each scenario several times and fails when its p95 wall
// time exceeds the budget in perf-budgets.json. CI=true applies the headroom.
//
// Exit codes: 0 within budget, 1 over budget, 2 usage or a scenario that could not run.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Scenario is a named measurement whose p95 is budgeted: a command whose wall time is taken, or,
// when Sample is set, a function that returns n timings itself (the hosted scenarios, which time
// one call against a seeded database rather than a process start).
type Scenario struct {
	Name   string
	Argv   []string
	Sample func(n int) ([]time.Duration, error)
}

var scenarios = []Scenario{
	{Name: "version-startup", Argv: []string{"dist/yo-human", "version"}},
	{Name: "hosted-post", Sample: hostedSample("post")},
	{Name: "hosted-revise", Sample: hostedSample("revise")},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, scenarios, os.Getenv("CI") == "true"))
}

func run(args []string, stdout, stderr io.Writer, scs []Scenario, ci bool) int {
	fs := flag.NewFlagSet("perf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("budgets", "perf-budgets.json", "budgets file")
	basePath := fs.String("base-budgets", "", "budgets file from the merge base; raising a budget or headroom, lowering runs, or removing a budget fails")
	if err := fs.Parse(args); err != nil {
		_, _ = fmt.Fprintln(stderr, "usage: perf [--budgets F] [--base-budgets F]")
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

// measure runs the scenario n times and returns each run's wall time, start to Wait.
func measure(s Scenario, n int) ([]time.Duration, error) {
	if s.Sample != nil {
		return s.Sample(n)
	}
	samples := make([]time.Duration, 0, n)
	for range n {
		cmd := exec.Command(s.Argv[0], s.Argv[1:]...) //nolint:gosec // argv comes from the scenarios slice
		start := time.Now()
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		if err := cmd.Wait(); err != nil {
			return nil, err
		}
		samples = append(samples, time.Since(start))
	}
	return samples, nil
}
