package core_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// resolveWhileClaimed flushes the waiting message of "k" and, once the flush has claimed it and
// before it checks the incident, moves the clock on a second and resolves "k".
func resolveWhileClaimed(t *testing.T, e *env) core.Result {
	t.Helper()
	core.BeforeAdmit(e.p, func() {
		e.clk.Advance(time.Second)
		e.resolve("k", "")
	})
	return e.flush()
}

// R1: a message waiting for its incident check, already claimed by a flush, must not page after a
// resolve of its key that came first: with no incident yet, and with an open one.
func TestAWaitingMessageClaimedBeforeAResolveDoesNotPage(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	_, release := deferred(t, e)
	release()
	fr := resolveWhileClaimed(t, e)
	if n := sentCount(e, "a"); n != 0 || statuses(fr)["_admit"] != core.ChannelCancelled {
		t.Errorf("flush = %+v, sent %d; want the waiting message dropped, nothing sent", fr, n)
	}
	if inc := e.incident("k"); inc.Open {
		t.Errorf("incident %+v; the resolve must leave it closed", inc)
	}
}

func TestAWaitingRepeatClaimedBeforeAResolveDoesNotReopen(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("low", "k", "t")
	e.cfg.Deadline = 200 * time.Millisecond
	release := holdLock(t, e)
	var r core.Result
	within(t, func() {
		r, _ = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "louder", DedupKey: "k"})
	})
	if r.Status != core.StatusQueued {
		t.Fatalf("louder repeat: %q; want it waiting", r.Status)
	}
	release()
	resolveWhileClaimed(t, e)
	if n := sentCount(e, "a"); n != 2 || titles(e, "a")[1] == "louder" {
		t.Errorf("sent %v; want the first page and the resolve note, never the louder repeat", titles(e, "a"))
	}
	if inc := e.incident("k"); inc.Open {
		t.Errorf("incident %+v; the late repeat reopened it", inc)
	}
}

// R2: a waiting repeat of an incident whose every send died is not "pending" because of itself: the
// flush that admits it sends it.
func TestAWaitingRepeatOfAStrandedIncidentIsSent(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	if r := e.say("page", "k", "t"); r.Status != core.StatusFailed {
		t.Fatalf("first: %q", r.Status)
	}
	e.fakes["a"].SendFunc = nil
	_, release := deferred(t, e)
	release()
	if fr := e.flush(); statuses(fr)["a"] != core.ChannelSent || sentCount(e, "a") != 2 {
		t.Errorf("flush = %+v; want the repeat sent: the incident reached no one", fr)
	}
}

// holdLockFor takes the incident lock through a second handle, as another process would, and lets
// it go after d. It returns once the lock is held.
func holdLockFor(t *testing.T, e *env, d time.Duration) {
	t.Helper()
	other, err := store.Open(e.dir)
	if err != nil {
		t.Error(err)
		return
	}
	held := make(chan struct{})
	go func() {
		_ = other.WithIncidents(func(map[string]*store.Incident) error {
			close(held)
			time.Sleep(d)
			return nil
		})
	}()
	<-held
}

// R3: a send that arrived is recorded on its incident even while another process holds the lock past
// the deadline, and a delivered page never exits 75 because of the lock.
func TestALandingIsRecordedBehindABusyLock(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.cfg.Deadline = 200 * time.Millisecond
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		holdLockFor(t, e, 400*time.Millisecond)
		return channel.Receipt{Instance: "a", ProviderID: "PD-FAKE-1"}, nil
	}
	var r core.Result
	var err error
	within(t, func() {
		r, err = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	})
	if r.Status != core.StatusDelivered || err != nil || r.Next != nil {
		t.Errorf("Notify = %q, %v, next %+v; want delivered, no error, no next", r.Status, err, r.Next)
	}
	if inc := e.incident("k"); len(inc.Reached) != 1 {
		t.Errorf("incident %+v; the landing must be recorded", inc)
	}
}

// expiredAndHeld has incident k expired (ttl 10m) while its page is still held: locally on "local",
// and scheduled at the provider on "n" (13:50, the window runs to 14:00).
func expiredAndHeld(t *testing.T) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"local": human, "n": schedules}
	cfg := quietCfg("local", "n")
	cfg.IncidentTTL = 10 * time.Minute
	e := newEnv(t, cfg, caps)
	e.clk.Advance(90 * time.Minute)
	n := 0
	e.fakes["n"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		n++
		return channel.Receipt{Instance: "n", ProviderID: fmt.Sprintf("SCHED-FAKE-%d", n), Scheduled: true}, nil
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("page: %q", r.Status)
	}
	e.clk.Advance(20 * time.Minute)
	return e
}

// R4: resolving an expired incident is a noop to the caller, but its held pages are still stopped:
// nothing fires when the window ends.
func TestResolvingAnExpiredIncidentStopsItsHeldPages(t *testing.T) {
	e := expiredAndHeld(t)
	if r := e.resolve("k", ""); r.Status != core.StatusNoop {
		t.Errorf("resolve: %q; want noop", r.Status)
	}
	if got := cancelledIDs(e); got["SCHED-FAKE-1"] != 1 {
		t.Errorf("cancelled %v; want the scheduled page cancelled", got)
	}
	if q, in, _ := e.counts(); q+in != 0 {
		t.Errorf("queued %d, in flight %d after the resolve; want the held copy removed", q, in)
	}
	e.clk.Advance(time.Hour)
	e.flush()
	if n := sentCount(e, "local"); n != 0 {
		t.Errorf("local got %d sends after the window; the resolved page must not fire", n)
	}
	if len(e.fakes["n"].Sent) != 1 {
		t.Errorf("n got %d sends; want only the scheduled page, and no note", len(e.fakes["n"].Sent))
	}
}

// n5: the next step of a queued cancel counts down from the incident level's retry.
func TestAQueuedCancelsNextUsesTheLevelsRetry(t *testing.T) {
	e := scheduledEnv(t)
	e.cfg.Levels[0].Retry.Times = 2
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Transient}})
	if r, _ := p.Resolve(context.Background(), "disk", ""); r.Next == nil || r.Next.AttemptsLeft != 2 {
		t.Errorf("next %+v; want 2 attempts left, from the level's retry (2)", r.Next)
	}
}

// The closed record a resolve leaves for a key it has never seen keeps the project and the key
// apart, as an incident opened by a message would (R1).
func TestAResolveOfAnUnknownKeyLeavesAClosedRecord(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	id := core.IncidentKey("proj", "k")
	if r := e.resolve(id, ""); r.Status != core.StatusNoop {
		t.Fatalf("resolve = %q; want noop", r.Status)
	}
	inc := e.incident(id)
	if inc.Open || inc.Project != "proj" || inc.Key != "k" || inc.Updated.IsZero() {
		t.Errorf("record = %+v; want closed, project proj, key k, stamped", inc)
	}
}

// A busy lock met in the backlog after the call's own message went out is a warning on a delivered
// result, never its error: a delivered message does not exit 75 for the lock (R3).
func TestABusyLockInTheBacklogIsAWarning(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	deferred(t, e) // the lock stays held, and a waiting message is due in the backlog
	var r core.Result
	var err error
	within(t, func() { r, err = e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "keyless"}) })
	busy := len(r.Warnings) > 0 && store.IsBusy(r.Warnings[0])
	if r.Status != core.StatusDelivered || err != nil || !busy {
		t.Errorf("Notify = %q, %v, warnings %v; want delivered, no error, the busy lock as a warning", r.Status, err, r.Warnings)
	}
}
