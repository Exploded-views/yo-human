package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// hostedArgv is the command that measures a hosted scenario: the helper under internal/hosted
// (which tools/ may not import) prints one timing in nanoseconds per line. A variable so the
// tests can stand in a fake.
var hostedArgv = func(kind string, n int) []string {
	return []string{"go", "run", "./internal/hosted/perf/cmd", kind, strconv.Itoa(n)}
}

// hostedSample returns the Sample function for a hosted scenario ("post" or "revise"): it runs
// the helper and reads exactly n positive nanosecond timings from its output.
func hostedSample(kind string) func(n int) ([]time.Duration, error) {
	return func(n int) ([]time.Duration, error) {
		argv := hostedArgv(kind, n)
		out, err := exec.Command(argv[0], argv[1:]...).Output() //nolint:gosec // argv comes from hostedArgv, a fixed command
		if err != nil {
			return nil, fmt.Errorf("hosted-%s helper: %w", kind, err)
		}
		return parseTimings(kind, string(out), n)
	}
}

func parseTimings(kind, out string, n int) ([]time.Duration, error) {
	lines := strings.Fields(out)
	if len(lines) != n {
		return nil, fmt.Errorf("hosted-%s helper printed %d timings, want %d", kind, len(lines), n)
	}
	samples := make([]time.Duration, 0, n)
	for _, l := range lines {
		ns, err := strconv.ParseInt(l, 10, 64)
		if err != nil || ns <= 0 {
			return nil, fmt.Errorf("hosted-%s helper printed %q, want a positive number of nanoseconds", kind, l)
		}
		samples = append(samples, time.Duration(ns))
	}
	return samples, nil
}
