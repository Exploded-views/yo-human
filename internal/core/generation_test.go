package core_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

var procs atomic.Int64

// proc is a second "process": its own pipeline and store handle over e's directory, sharing the
// channels of e.
func proc(t *testing.T, e *env) *core.Pipeline {
	t.Helper()
	st, err := store.Open(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	chans := map[string]channel.Channel{}
	caps := map[string]channel.Capabilities{}
	for n, f := range e.fakes {
		chans[n], caps[n] = f, f.Caps
	}
	id, n := procs.Add(1), atomic.Int64{}
	return core.New(core.Deps{Config: e.cfg, Store: st, Channels: chans, Caps: caps, Now: e.clk.Now,
		NewID: func() string { return fmt.Sprintf("P%d-%03d", id, n.Add(1)) }})
}

// wait blocks until ch is closed, failing the test rather than hanging it.
func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: a hook or goroutine the test waits for never ran")
	}
}

func titles(e *env, name string) []string {
	var out []string
	for _, m := range e.fakes[name].Sent {
		out = append(out, m.Title)
	}
	return out
}

func logHas(e *env, want string) bool {
	b, _ := os.ReadFile(filepath.Join(e.dir, "log.jsonl"))
	return strings.Contains(string(b), want)
}

func TestGenerationBumpsOnEveryOpen(t *testing.T) {
	e := newEnv(t, incCfg(30*time.Minute, "a"), one)
	gen := func() int { return e.incident("k").Generation }
	e.say("low", "k", "t")
	if gen() != 1 {
		t.Fatalf("first open = %d, want 1", gen())
	}
	e.say("page", "k", "t") // raised
	e.clk.Advance(time.Hour)
	e.say("page", "k", "t") // reminded
	e.say("page", "k", "changed")
	if gen() != 1 {
		t.Errorf("a raise, a reminder and an update are the same incident; generation = %d", gen())
	}
	e.resolve("k", "")
	e.say("page", "k", "t")
	if gen() != 2 {
		t.Errorf("reopened after a resolve = %d, want 2", gen())
	}
	e.clk.Advance(25 * time.Hour)
	e.say("page", "k", "t")
	if gen() != 3 {
		t.Errorf("reopened after expiry = %d, want 3", gen())
	}
}

// 20 processes, each with its own store handle, repeat at once: the flock, not a shared handle,
// makes it one page.
func TestRepeatsFromSeparateStoreHandlesPageOnce(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	var wg sync.WaitGroup
	for range 20 {
		p := proc(t, e)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := sentCount(e, "a"); n != 1 {
		t.Errorf("sent %d times, want once", n)
	}
}

// Resolve closes the incident and cancels its queue in one hold of the lock. Another process's
// notify that reopens the key while the resolve runs gets in either wholly before or wholly after.
func TestReopenDuringResolveKeepsTheNewMessage(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	p2 := proc(t, e)
	core.AfterEnqueue(p2, func() { time.Sleep(150 * time.Millisecond) }) // its entry stays queued while the resolve finishes
	var nr core.Result
	done := make(chan struct{})
	core.AfterClose(e.p, func() {
		go func() {
			nr, _ = p2.Notify(context.Background(), channel.Message{Level: "page", Title: "again", DedupKey: "k"})
			close(done)
		}()
		time.Sleep(100 * time.Millisecond) // p2 is now waiting for the lock, or past it
	})
	e.resolve("k", "")
	wait(t, done)
	inc := e.incident("k")
	if !inc.Open || inc.Generation != 2 || nr.Status != core.StatusDelivered {
		t.Errorf("incident open=%v gen=%d, notify %q %+v; want the reopened incident paged", inc.Open, inc.Generation, nr.Status, nr.Channels)
	}
	// The resolve's note for the first generation goes out only if it wins the lock before the
	// reopen; either way the new message goes out, once, after it.
	got := titles(e, "a")
	withNote := len(got) == 3 && got[1] == "Resolved: t"
	if got[len(got)-1] != "again" || (len(got) != 2 && !withNote) {
		t.Errorf("sent %v; the new message must go out, once, last", got)
	}
}

// A flush in another process can claim a due entry after the incident is marked closed and before
// the resolve is through. It must drop the entry, not page.
func TestFlushClaimingDuringResolveDoesNotPage(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "t") // queued for a retry
	e.clk.Advance(time.Hour)
	var closedAtSend atomic.Bool
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		closedAtSend.Store(!e.incident("k").Open)
		return channel.Receipt{}, nil
	}
	p2 := proc(t, e)
	done := make(chan struct{})
	core.AfterClose(e.p, func() {
		go func() { _, _ = p2.Flush(context.Background()); close(done) }()
		time.Sleep(100 * time.Millisecond) // p2 has claimed the entry and waits to check its incident
	})
	e.resolve("k", "")
	wait(t, done)
	if sentCount(e, "a") != 1 { // only the first, failed, attempt
		t.Errorf("sent %d; a resolved incident must not be paged by a flush", sentCount(e, "a"))
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d; the dropped entry must be gone", q, in, d)
	}
	if !logHas(e, `"status":"cancelled"`) || !logHas(e, `incident k was resolved"`) {
		t.Error("no log line naming the cause (resolved) for the dropped entry")
	}
}

// A send in flight at a resolve fails after the key was reopened. It is the old incident's: it must
// not be requeued into the new one.
func TestStaleInflightSendIsNotRequeuedIntoTheReopenedIncident(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fakes["a"].SendFunc = func(_ context.Context, m channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		if m.Title == "old" {
			once.Do(func() { close(started) })
			<-release
			return channel.Receipt{}, channel.Transient(errors.New("down"))
		}
		return channel.Receipt{}, nil
	}
	done := make(chan struct{})
	go func() { e.say("page", "k", "old"); close(done) }()
	<-started
	e.resolve("k", "")
	e.say("page", "k", "new")
	close(release)
	wait(t, done)
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := titles(e, "a"); strings.Join(got, ",") != "old,new" {
		t.Errorf("sent %v; the old page must not be sent again into the new incident", got)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d", q, in, d)
	}
	if !logHas(e, `incident k was reopened"`) {
		t.Error("the dropped requeue's log line must say reopened")
	}
}

// A resolve note retried after the key was reopened closes the wrong incident.
func TestStaleResolveNoteDoesNotCloseTheReopenedIncident(t *testing.T) {
	caps := map[string]channel.Capabilities{"pd": {ReachesHuman: true, NativeDedup: true, Resolve: true}}
	e := newEnv(t, incCfg(0, "pd"), caps)
	e.say("page", "k", "t")
	p := newWith(e, map[string]channel.Channel{"pd": failing{Fake: e.fakes["pd"], resolveErr: errors.New("down")}})
	if r, err := p.Resolve(context.Background(), "k", ""); !isQueued(err) || r.Status != core.StatusResolved {
		t.Fatalf("%v %+v; want resolved, with the close queued", err, r)
	}
	e.clk.Advance(time.Minute)
	if r := e.say("page", "k", "it broke again"); r.Status != core.StatusDelivered {
		t.Fatalf("reopen: %q", r.Status)
	}
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.fakes["pd"].Resolved; len(got) != 0 {
		t.Errorf("Resolved %v; the old note must not close the reopened incident's alert", got)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d; the stale note is dropped", q, in, d)
	}
}

// A retry queued before an incident expired belongs to the generation that expired.
func TestExpiredIncidentsQueuedRetryIsDropped(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "old")
	e.fakes["a"].SendFunc = nil
	e.clk.Advance(25 * time.Hour)
	if r := e.say("page", "k", "new"); r.Status != core.StatusDelivered {
		t.Fatalf("status %q", r.Status)
	}
	if got := titles(e, "a"); strings.Join(got, ",") != "old,new" {
		t.Errorf("sent %v; the old queued retry must be dropped, not sent after the new page", got)
	}
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !logHas(e, `incident k was expired (superseded)"`) {
		t.Error("the dropped entry's log line must say expired (superseded)")
	}
}

// Claim fails when a resolve removed the entry. If the key was reopened by then, the call is still
// a resolve outcome, not a queued one.
func TestNotifyResolvedAndReopenedMidCallReportsResolved(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	core.AfterEnqueue(e.p, func() {
		core.AfterEnqueue(e.p, nil)
		e.resolve("k", "")
		e.say("page", "k", "other")
	})
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "first", DedupKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != core.StatusResolved || r.Next != nil {
		t.Errorf("got %q next=%+v; its incident was resolved under it", r.Status, r.Next)
	}
}

// An entry whose incident has no record (the file was lost) is sent, not dropped.
func TestEntryWithoutAnIncidentRecordIsStillSent(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "t")
	e.fakes["a"].SendFunc = nil
	if err := os.Remove(filepath.Join(e.dir, "incidents.json")); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sentCount(e, "a") != 2 {
		t.Errorf("sent %d; a message must not be lost for want of a record", sentCount(e, "a"))
	}
}

func TestClaimedEntryIsKeptWhenItsIncidentCannotBeChecked(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "t")
	e.fakes["a"].SendFunc = nil
	p := filepath.Join(e.dir, "incidents.json")
	_ = os.Remove(p)
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err == nil {
		t.Error("an unreadable incident store went unreported")
	}
	if sentCount(e, "a") != 1 {
		t.Errorf("sent %d; nothing is sent on a guess", sentCount(e, "a"))
	}
	if q, in, d := e.counts(); q+in != 1 || d != 0 {
		t.Errorf("queue %d/%d/%d; the entry must be kept", q, in, d)
	}
}
