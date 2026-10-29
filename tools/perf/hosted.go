package main

import (
	"strconv"
	"time"
)

// hostedArgv is the command that measures a hosted scenario: the helper under internal/hosted
// (which tools/ may not import) prints one timing in nanoseconds per line. A variable so the
// tests can stand in a fake.
var hostedArgv = func(kind string, n int) []string {
	return []string{"go", "run", "./internal/hosted/perf/cmd", kind, strconv.Itoa(n)}
}

// hostedSample returns the Sample function for a hosted scenario. (stub)
func hostedSample(kind string) func(n int) ([]time.Duration, error) {
	return func(n int) ([]time.Duration, error) { return nil, nil }
}
