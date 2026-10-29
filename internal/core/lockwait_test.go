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
	"github.com/Exploded-views/yo-human/internal/store"
)

// holdLock takes the incident lock through a second handle, as another process would, until the
// returned release runs (or the test ends).
func holdLock(t *testing.T, e *env) (release func()) {
	t.Helper()
	other, err := store.Open(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	held, rel, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = other.WithIncidents(func(map[string]*store.Incident) error {
			close(held)
			<-rel
			return nil
		})
	}()
	<-held
	var once bool
	release = func() {
		if !once {
			once = true
			close(rel)
			<-done
		}
	}
	t.Cleanup(release)
	return release
}

// within runs f, failing the test if it is still running long after the deadline.
func within(t *testing.T, f func()) {
	t.Helper()
	ok := make(chan struct{})
	go func() { f(); close(ok) }()
	select {
	case <-ok:
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting for the incident lock 5s after the deadline")
	}
}

// deferred notifies "page" with key k while another process holds the lock: the message is left in
// the queue for its incident check. It returns the result and the lock's release.
func deferred(t *testing.T, e *env) (core.Result, func()) {
	t.Helper()
	e.cfg.Deadline = 200 * time.Millisecond
	release := holdLock(t, e)
	var r core.Result
	var err error
	within(t, func() {
		r, err = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	return r, release
}

// A keyed notify cannot check its incident while another process holds the lock past the deadline.
// It sends nothing (that could page twice), keeps the message in the queue for the incident check,
// and says to flush (exit 75); the flush admits it once the lock is free.
func TestAKeyedNotifyBehindAHeldLockIsQueuedForAFlush(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	r, release := deferred(t, e)
	if r.Status != core.StatusQueued || r.Next == nil || r.Next.Do != "yo-human flush" || sentCount(e, "a") != 0 {
		t.Fatalf("Notify = %+v, sent %d; want queued with a flush as the next step, and nothing sent", r, sentCount(e, "a"))
	}
	release()
	admittedOnce(t, e)
}

// admittedOnce flushes and checks that the waiting message was admitted and sent once, and is an
// incident like any other.
func admittedOnce(t *testing.T, e *env) {
	t.Helper()
	fr := e.flush()
	if n := sentCount(e, "a"); n != 1 || fr.Status != core.StatusOK || statuses(fr)["a"] != core.ChannelSent {
		t.Fatalf("flush = %+v, sent %d; want the message admitted and sent once", fr, n)
	}
	if inc := e.incident("k"); !inc.Open || len(inc.Reached) != 1 {
		t.Errorf("incident %+v; want it open and reached", inc)
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Errorf("repeat: %q; the admitted message is an incident like any other", r.Status)
	}
}

// A message whose incident cannot be read at the flush stays in flight for recovery; the store
// failure is reported.
func TestADeferredMessageWhoseIncidentCannotBeReadIsKept(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	_, release := deferred(t, e)
	release()
	incidents := filepath.Join(e.dir, "incidents.json")
	if err := os.Remove(incidents); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(incidents, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := e.p.Flush(context.Background())
	if pr, ok := problem.As(err); !ok || pr.Kind != problem.Store || sentCount(e, "a") != 0 {
		t.Errorf("Flush error %v, sent %d; want a store problem and nothing sent", err, sentCount(e, "a"))
	}
	if _, in, _ := e.counts(); in != 1 {
		t.Errorf("in flight %d; want the message kept for recovery", in)
	}
}

// A resolve of a key with no incident looks for a waiting message to stop; a queue that cannot be
// read is reported.
func TestAResolveThatCannotLookForAWaitingMessageSaysSo(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if err := os.WriteFile(filepath.Join(e.dir, "queue", "junk.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	dead := filepath.Join(e.dir, "dead")
	if err := os.Chmod(dead, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dead, 0o700) })
	r, err := e.p.Resolve(context.Background(), "nothing", "")
	if pr, ok := problem.As(err); !ok || pr.Kind != problem.Store || r.Status != core.StatusNoop {
		t.Errorf("Resolve = %+v, %v; want noop, with the store problem", r, err)
	}
}

// While the lock is still held, the flush leaves the message waiting, no attempt spent, and says to
// flush again.
func TestADeferredMessageWaitsWhileTheLockIsStillHeld(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	deferred(t, e)
	var r core.Result
	var err error
	within(t, func() { r, err = e.p.Flush(context.Background()) })
	if !store.IsBusy(err) || r.Status != core.StatusQueued || r.Next == nil || statuses(r)["_admit"] != core.ChannelQueued || sentCount(e, "a") != 0 {
		t.Fatalf("Flush = %+v, %v; want it still waiting, queued, with a next step", r, err)
	}
	if due, _ := e.st.Due(e.clk.Now()); len(due) != 1 || due[0].Attempts != 0 {
		t.Errorf("queue %+v; want the message waiting, no attempt spent", due)
	}
}

// A resolve that comes before the flush stops the message: the human is not paged for an incident
// the agent already closed.
func TestAResolveStopsAMessageWaitingForItsIncidentCheck(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	_, release := deferred(t, e)
	release()
	if r := e.resolve("k", ""); r.Status != core.StatusResolved || statuses(r)["_admit"] != core.ChannelCancelled {
		t.Fatalf("resolve = %+v; want resolved, the waiting message cancelled", r)
	}
	e.flush()
	if n := sentCount(e, "a"); n != 0 {
		t.Errorf("sent %d; the resolved message must not go out", n)
	}
}

// A flush builds the channels a waiting message's level sends to, not a channel named by its key.
func TestEntryInstancesOfAWaitingMessageAreItsLevels(t *testing.T) {
	e := newEnv(t, incCfg(0, "a", "b"), map[string]channel.Capabilities{"a": human, "b": human})
	deferred(t, e)
	due, err := e.st.Due(e.clk.Now())
	if err != nil || len(due) != 1 {
		t.Fatalf("queue %+v, %v", due, err)
	}
	if got := core.EntryInstances(due[0]); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("EntryInstances = %v; want the level's a and b", got)
	}
	if got := core.EntryInstances(store.Pending{Key: "X1.a", Data: []byte(`{"instance":"a"}`)}); len(got) != 1 || got[0] != "a" {
		t.Errorf("EntryInstances of a send = %v; want its own instance", got)
	}
}

// Recovering stale entries needs the lock. Behind a held lock it waits for a later call; a notify
// that needs no incident check still goes out.
func TestRecoveryBehindAHeldLockIsPostponed(t *testing.T) {
	e := newEnv(t, levelsCfg(200*time.Millisecond, page(retry5, "a")), one)
	if err := e.st.Enqueue(store.Pending{Key: "OLD.a", Data: []byte(`{"instance":"a"}`)}); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(e.dir, "inflight", "OLD.a.999999.json")
	if err := os.Rename(filepath.Join(e.dir, "queue", "OLD.a.json"), stale); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	holdLock(t, e)
	var r core.Result
	var err error
	within(t, func() { r, err = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t"}) })
	if err != nil || r.Status != core.StatusDelivered {
		t.Errorf("Notify = %+v, %v; want delivered, the recovery postponed", r, err)
	}
	if _, in, _ := e.counts(); in != 1 {
		t.Errorf("in flight %d; want the stale entry left for a later call", in)
	}
}

// A message that cannot be kept for its incident check is a store failure.
func TestADeferredMessageThatCannotBeKeptIsAStoreError(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if err := os.Mkdir(filepath.Join(e.dir, "queue", "M001._admit.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.cfg.Deadline = 200 * time.Millisecond
	holdLock(t, e)
	var err error
	within(t, func() {
		_, err = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	})
	if pr, ok := problem.As(err); !ok || pr.Kind != problem.Store {
		t.Errorf("Notify error %v; want a store problem", err)
	}
}

// A flush behind a held lock cannot check an entry's incident: the entry waits, and the flush says
// to run again (exit 75).
func TestAFlushBehindAHeldLockSaysToFlushAgain(t *testing.T) {
	e := queuedOnce(t)
	e.cfg.Deadline = 200 * time.Millisecond
	holdLock(t, e)
	var r core.Result
	var err error
	within(t, func() { r, err = e.p.Flush(context.Background()) })
	if !store.IsBusy(err) || r.Next == nil || r.Next.Do != "yo-human flush" {
		t.Errorf("Flush = %+v, %v; want the busy lock reported, with a flush as the next step", r, err)
	}
}
