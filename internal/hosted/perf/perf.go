// Package perf measures hosted mode's hot paths for tools/perf: one timing per Bus.Post against a
// seeded database. It lives under internal/hosted because it imports the bus and the store,
// which the CLI side (and so tools/) may not.
package perf

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// SeedItems is the size of the database the scenarios run against (the plan's budget line:
// p95 against a 10,000-item database on local disk).
const SeedItems = 10000

var (
	seedItems = SeedItems // a variable so the tests can run Run on a small database
	tmpRoot   = ""        // where the temp databases go; empty is the system temp directory
)

type sampler func(dir string, items, n int) ([]time.Duration, error)

// Post times n calls of Bus.Post that each create an item.
func Post(n int) ([]time.Duration, error) { return inTempDir(SeedItems, n, PostIn) }

// Revise times n calls of Bus.Post that each revise one item, through the store's UpdateItem and
// its event appends.
func Revise(n int) ([]time.Duration, error) { return inTempDir(SeedItems, n, ReviseIn) }

// inTempDir runs fn in a fresh 0700 temp directory and removes it afterwards.
func inTempDir(items, n int, fn sampler) ([]time.Duration, error) {
	dir, err := os.MkdirTemp(tmpRoot, "yh-perf-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	return fn(dir, items, n)
}

// Run is the helper's main: `post N` or `revise N` prints N timings in nanoseconds, one per
// line, against a database of seedItems items. Exit 0 ok, 1 a measurement failed, 2 usage.
func Run(args []string, stdout, stderr io.Writer) int {
	var fn sampler
	n := 0
	if len(args) == 2 {
		n, _ = strconv.Atoi(args[1])
		fn = map[string]sampler{"post": PostIn, "revise": ReviseIn}[args[0]]
	}
	if fn == nil || n < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: perf post|revise N")
		return 2
	}
	samples, err := inTempDir(seedItems, n, fn)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "perf:", err)
		return 1
	}
	for _, d := range samples {
		_, _ = fmt.Fprintln(stdout, d.Nanoseconds())
	}
	return 0
}

var perfNode = bus.Principal{Kind: bus.PrincipalNode, Name: "perf", Swarm: "crew"}

func perfDraft(handle, title string) bus.Draft {
	return bus.Draft{Kind: bus.KindNotice, Severity: bus.SeverityNormal, To: "human", Handle: handle, Title: title,
		Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
}

// PostIn is Post against a database of items items in dir.
func PostIn(dir string, items, n int) ([]time.Duration, error) {
	b, closeDB, err := seeded(dir, items)
	if err != nil {
		return nil, err
	}
	defer closeDB()
	return timePosts(b, n, func(i int) bus.Draft { return perfDraft(fmt.Sprintf("perf-%d", i), "Build done") }, bus.OutcomeCreated)
}

// ReviseIn is Revise against a database of items items in dir.
func ReviseIn(dir string, items, n int) ([]time.Duration, error) {
	b, closeDB, err := seeded(dir, items)
	if err != nil {
		return nil, err
	}
	defer closeDB()
	if _, err := b.Post(context.Background(), perfNode, perfDraft("revise-me", "Build 0")); err != nil {
		return nil, err
	}
	return timePosts(b, n, func(i int) bus.Draft { return perfDraft("revise-me", fmt.Sprintf("Build %d", i+1)) }, bus.OutcomeRevised)
}

// timePosts posts n drafts and returns how long each Post took. Every post must have the wanted
// outcome, or the timing is of some other operation and the run fails.
func timePosts(b *bus.Bus, n int, draft func(i int) bus.Draft, want bus.Outcome) ([]time.Duration, error) {
	out := make([]time.Duration, 0, n)
	for i := range n {
		d := draft(i)
		start := time.Now()
		res, err := b.Post(context.Background(), perfNode, d)
		out = append(out, time.Since(start))
		if err != nil {
			return nil, err
		}
		if res.Outcome != want {
			return nil, fmt.Errorf("a timed post was %s, want %s: the scenario measures the wrong operation", res.Outcome, want)
		}
	}
	return out, nil
}

// seeded opens a database in dir holding items items (each with its posted event) spread over
// four swarms, and returns a Bus over it with the function that closes the database.
func seeded(dir string, items int) (*bus.Bus, func(), error) {
	return seededWarm(dir, items, perfDraft("warm-up", "Warm-up"))
}

// seededWarm is seeded with the warm-up draft given, so a failing warm-up can be tested.
func seededWarm(dir string, items int, warm bus.Draft) (*bus.Bus, func(), error) {
	d, err := db.Open(filepath.Join(dir, "bus.db"))
	if err != nil {
		return nil, nil, err
	}
	closeDB := func() { _ = d.Close() }
	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	swarms := []string{"crew", "client", "other", "extra"}
	err = d.Tx(context.Background(), func(x bus.Tx) error {
		for i := range items {
			it := bus.Item{
				Draft: perfDraft(fmt.Sprintf("seed-%d", i), "Seeded item"),
				ID:    fmt.Sprintf("itm_seed%06d", i), Swarm: swarms[i%len(swarms)], From: "seed", FromKind: bus.PrincipalNode,
				Revision: 1, State: bus.StateOpen, ContentHash: "v1:seed", Created: at, Updated: at,
			}
			seq, err := x.AppendEvent(bus.Event{Swarm: it.Swarm, ItemID: it.ID, Type: bus.EventItemPosted, Created: at})
			if err != nil {
				return err
			}
			it.LastSeq = seq
			if err := x.InsertItem(it); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		closeDB()
		return nil, nil, err
	}
	b := bus.New(d, time.Now, bus.Limits{})
	// Measured locally: the first commit after the 10,000-item bulk seed took 8.6-10.3 ms, the
	// rest 0.16-0.37 ms. The seed creates that one-off, so the scenarios must not time it.
	if _, err := b.Post(context.Background(), perfNode, warm); err != nil {
		closeDB()
		return nil, nil, err
	}
	return b, closeDB, nil
}
