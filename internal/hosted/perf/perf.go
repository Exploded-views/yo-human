// Package perf measures hosted mode's hot paths for tools/perf: one timing per Bus.Post against a
// seeded database. It lives under internal/hosted because it imports the bus and the store,
// which the CLI side (and so tools/) may not.
package perf

import (
	"io"
	"time"
)

// SeedItems is the size of the database the scenarios run against (the plan's budget line:
// p95 against a 10,000-item database on local disk).
const SeedItems = 10000

// Post times n calls of Bus.Post that each create an item. (stub)
func Post(n int) ([]time.Duration, error) { return nil, nil }

// Revise times n calls of Bus.Post that each revise one item. (stub)
func Revise(n int) ([]time.Duration, error) { return nil, nil }

// PostIn is Post against a database of items items in dir. (stub)
func PostIn(dir string, items, n int) ([]time.Duration, error) { return nil, nil }

// ReviseIn is Revise against a database of items items in dir. (stub)
func ReviseIn(dir string, items, n int) ([]time.Duration, error) { return nil, nil }

// Run is the helper's main: perf post|revise N prints N timings in nanoseconds, one per line. (stub)
func Run(args []string, stdout, stderr io.Writer) int { return 2 }

var seedItems = SeedItems
