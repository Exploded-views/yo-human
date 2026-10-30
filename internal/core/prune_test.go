package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// A cancel of an expired incident's scheduled send that is queued for another try says to flush,
// as every other queued cancel does: exit 75 always comes with a next step.
func TestAnExpiredResolveWithAQueuedCancelSaysToFlush(t *testing.T) {
	e := expiredAndHeld(t)
	p := e.with(e.cfg, map[string]channel.Channel{"n": failingCancel{e.fakes["n"], channel.Transient}})
	r, err := p.Resolve(context.Background(), "k", "")
	pr, ok := problem.As(err)
	if r.Status != core.StatusNoop || !ok || pr.Kind != problem.Queued {
		t.Fatalf("resolve = %q, %v; want a noop with the cancel queued", r.Status, err)
	}
	if r.Next == nil || r.Next.Do != "yo-human flush" {
		t.Errorf("next = %+v; want yo-human flush", r.Next)
	}
}

// A closed record is kept for incident_ttl, then pruned the next time the incidents are written, so
// resolves of keys never seen do not pile up in incidents.json.
func TestAClosedRecordIsPrunedAfterIncidentTTL(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.resolve("k", "")
	e.say("page", "other", "t")
	if !e.hasIncident("k") {
		t.Fatal("a closed record younger than incident_ttl was pruned")
	}
	e.clk.Advance(25 * time.Hour)
	e.say("page", "other2", "t")
	if e.hasIncident("k") {
		t.Error("a closed record older than incident_ttl was kept")
	}
	if !e.hasIncident("other") {
		t.Error("an open incident was pruned")
	}
}

// A closed record a waiting message still needs for its drop check (R1) is not pruned, however old:
// the message, claimed before the resolve, is dropped and pages nobody.
func TestAClosedRecordAWaitingMessageNeedsIsKept(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	_, release := deferred(t, e)
	release()
	fired := false
	core.BeforeAdmit(e.p, func() {
		if fired {
			return
		}
		fired = true
		e.clk.Advance(time.Second)
		e.resolve("k", "")
		e.clk.Advance(25 * time.Hour)
		e.say("page", "other", "t") // writes the incidents, past incident_ttl
	})
	fr := e.flush()
	if n := sentCount(e, "a"); n != 1 || statuses(fr)["_admit"] != core.ChannelCancelled {
		t.Errorf("flush = %+v, sent %d; want the waiting message dropped, only the other incident sent", fr, n)
	}
}

// Generations are never reused, so an entry written for an incident whose record was pruned never
// acts on the key's next incident. Here a resolve note, kept out of sight of the prune (as one in
// transit between queue/ and inflight/ could be), comes back after the key has reopened: it must
// not reach the human as a resolve of the live incident.
func TestAStaleNoteNeverResolvesTheKeysNextIncident(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.resolve("k", "")
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("queued %d after the resolve; want its note queued", q)
	}
	e.fakes["a"].SendFunc = nil
	hidden := hideQueue(t, e)
	e.clk.Advance(25 * time.Hour)
	e.say("page", "other", "t") // prunes the closed record of k
	if e.hasIncident("k") {
		t.Fatal("the closed record of k was not pruned")
	}
	e.say("page", "k", "again")
	sent := sentCount(e, "a")
	hidden()
	e.clk.Advance(time.Hour)
	e.flush()
	for _, m := range e.fakes["a"].Sent[sent:] {
		if m.Resolved {
			t.Errorf("a stale resolve note was sent for the reopened incident: %+v", m)
		}
	}
	if !e.incident("k").Open {
		t.Error("the reopened incident was closed")
	}
}

// hideQueue moves every queued entry out of the store, and returns a func that puts them back.
func hideQueue(t *testing.T, e *env) func() {
	t.Helper()
	q, aside := filepath.Join(e.dir, "queue"), t.TempDir()
	move := func(from, to string) {
		ents, err := os.ReadDir(from)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range ents {
			if err := os.Rename(filepath.Join(from, en.Name()), filepath.Join(to, en.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	move(q, aside)
	return func() { move(aside, q) }
}

// When the entries cannot be listed, nothing is pruned: a record kept longer is harmless, one
// pruned on a guess could let a stale entry act on a later incident.
func TestNothingIsPrunedWhenTheEntriesCannotBeListed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission faults need a non-root user")
	}
	e := newEnv(t, incCfg(0, "a"), one)
	e.resolve("old", "")
	e.clk.Advance(25 * time.Hour)
	_, release := deferred(t, e)
	release()
	inflight := filepath.Join(e.dir, "inflight")
	core.BeforeAdmit(e.p, func() { // the waiting message is claimed; its incident check lists the entries
		if err := os.Chmod(inflight, 0o300); err != nil {
			t.Fatal(err)
		}
	})
	t.Cleanup(func() { _ = os.Chmod(inflight, 0o700) })
	_, _ = e.p.Flush(context.Background()) // settling the claimed entry fails too; only the record matters here
	if err := os.Chmod(inflight, 0o700); err != nil {
		t.Fatal(err)
	}
	if !e.hasIncident("old") {
		t.Error("a closed record was pruned though the entries could not be listed")
	}
}
