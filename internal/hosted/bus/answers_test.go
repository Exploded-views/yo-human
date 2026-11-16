package bus_test

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

var cinode = node("ci-1", "crew")

// pageAll reads every page after 0 with no wait and returns how often each seq came back and how
// many pages had events. It fails the test on a page over 100 events or a Next that is not the
// last seq returned.
func pageAll(t *testing.T, e *env, p bus.Principal) (seen map[int64]int, pages int) {
	t.Helper()
	seen = map[int64]int{}
	var since int64
	for {
		u := e.answers(t, p, since, 0)
		if len(u.Events) == 0 {
			if u.Next != since {
				t.Errorf("an empty page moved Next from %d to %d", since, u.Next)
			}
			return seen, pages
		}
		if len(u.Events) > 100 {
			t.Fatalf("a page of %d events, want at most 100", len(u.Events))
		}
		for _, ev := range u.Events {
			seen[ev.Seq]++
		}
		if last := u.Events[len(u.Events)-1].Seq; u.Next != last {
			t.Errorf("Next = %d, want the last seq returned, %d", u.Next, last)
		}
		since = u.Next
		pages++
	}
}

func TestAnswersSinceCursorNoLoss(t *testing.T) {
	e := newEnv(t)
	for i := range 250 {
		e.post(t, duet, toCrew("h"+string(rune('a'+i/26))+string(rune('a'+i%26))))
	}
	seen, pages := pageAll(t, e, cinode)
	if len(seen) != 250 || pages != 3 {
		t.Errorf("saw %d distinct events in %d pages, want 250 in 3", len(seen), pages)
	}
	for seq, n := range seen {
		if n != 1 {
			t.Errorf("event %d returned %d times", seq, n)
		}
	}
}

// nodeActivity makes the node's own events: a post, a reply, a revision and a status push.
func nodeActivity(t *testing.T, e *env) bus.Item {
	t.Helper()
	ctx := context.Background()
	mine := e.post(t, cinode, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	if _, err := e.b.Reply(ctx, cinode, mine.ID, "from me", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	rev := draft(bus.KindQuestion, "q", bus.UrgencyDigest)
	rev.Title = "changed"
	e.post(t, cinode, rev)
	if err := e.b.PushStatus(ctx, cinode, validStatus("crew")); err != nil {
		t.Fatal(err)
	}
	return mine
}

func TestAnswersExcludesWhatTheNodeCaused(t *testing.T) {
	e := newEnv(t)
	nodeActivity(t, e)
	u := e.answers(t, cinode, 0, 0)
	if len(u.Events) != 0 {
		t.Fatalf("the node's own events came back: %+v", u.Events)
	}
	if u.Next == 0 {
		t.Error("Next stayed at 0: a poller would read its own events again forever")
	}
}

func TestAnswersReturnsWhatOthersDid(t *testing.T) {
	e := newEnv(t)
	mine := nodeActivity(t, e)
	next := e.answers(t, cinode, 0, 0).Next
	if _, err := e.b.Reply(context.Background(), duet, mine.ID, "from duet", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	other := e.post(t, node("ci-2", "crew"), draft(bus.KindQuestion, "q2", bus.UrgencyDigest)).Item
	u := e.answers(t, cinode, next, 0)
	if len(u.Events) != 2 || u.Events[0].Type != bus.EventMessageAdded || u.Events[1].ItemID != other.ID {
		t.Fatalf("events = %+v, want duet's message and the other node's post", u.Events)
	}
	wantMessageUpdate(t, u.Events[0], mine.ID)
}

func wantMessageUpdate(t *testing.T, m bus.Update, itemID string) {
	t.Helper()
	if m.Message == nil || m.Message.Body != "from duet" || m.ItemID != itemID || m.Handle != "q" || m.Revision != 2 || m.Record != nil {
		t.Errorf("message update = %+v", m)
	}
}

func TestAnswersCarryTheRecord(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, cinode, draft(bus.KindQuestion, "q", bus.UrgencyDigest)).Item
	cur, _ := e.stored(t, it.ID)
	e.putAnswer(t, cur, "go ahead")
	u := e.answers(t, cinode, 0, 0)
	var got *bus.Update
	for i := range u.Events {
		if u.Events[i].Type == bus.EventAnswerRecorded {
			got = &u.Events[i]
		}
	}
	if got == nil || got.Record == nil || got.Record.Text != "go ahead" || got.Record.Kind != bus.RecordAnswer || got.Handle != "q" || got.Revision != 1 {
		t.Fatalf("answer update = %+v", got)
	}
}

func TestAnswersScopedToSwarm(t *testing.T) {
	e := newEnv(t)
	e.post(t, duet, toCrew("for-crew"))
	e.post(t, duet, func() bus.Draft { d := toCrew("for-client"); d.To = "client"; return d }())
	crew := e.answers(t, cinode, 0, 0)
	if len(crew.Events) != 1 || crew.Events[0].Handle != "for-crew" {
		t.Fatalf("crew sees %+v", crew.Events)
	}
	client := e.answers(t, node("ci-9", "client"), 0, 0)
	if len(client.Events) != 1 || client.Events[0].Handle != "for-client" {
		t.Fatalf("client sees %+v", client.Events)
	}
}

func TestAnswersOnlyForNodes(t *testing.T) {
	e := newEnv(t)
	for _, p := range []bus.Principal{duet, {Kind: bus.PrincipalDesk}, {Kind: bus.PrincipalPasskey}, {Kind: bus.PrincipalExecutor},
		{Kind: bus.PrincipalNode, Name: "x", Swarm: "duet"}, {}} {
		_, err := e.b.Answers(context.Background(), p, 0, 0)
		wantCode(t, err, bus.CodeForbidden)
	}
}

// fakeTimer is a long-poll timer a test fires by hand.
type fakeTimer struct {
	started chan time.Duration
	fire    chan time.Time
	stopped atomic.Int32
}

func newFakeTimer(t *testing.T, b *bus.Bus) *fakeTimer {
	ft := &fakeTimer{started: make(chan time.Duration, 8), fire: make(chan time.Time, 1)}
	b.SetTimer(func(d time.Duration) (<-chan time.Time, func()) {
		ft.started <- d
		return ft.fire, func() { ft.stopped.Add(1) }
	})
	return ft
}

// waitStarted blocks until the waiter has started its timer, which it does only after reading the
// store and finding nothing.
func (f *fakeTimer) waitStarted(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-f.started:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never started its timer")
		return 0
	}
}

type pollResult struct {
	u   bus.Updates
	err error
}

func poll(b *bus.Bus, ctx context.Context, p bus.Principal, since int64, wait time.Duration) <-chan pollResult {
	ch := make(chan pollResult, 1)
	go func() {
		u, err := b.Answers(ctx, p, since, wait)
		ch <- pollResult{u, err}
	}()
	return ch
}

func within(t *testing.T, ch <-chan pollResult, d time.Duration, what string) pollResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(d):
		t.Fatalf("%s: no return within %v", what, d)
		return pollResult{}
	}
}

func TestAnswersLongPollWakesOnCommit(t *testing.T) {
	e := newEnv(t)
	ft := newFakeTimer(t, e.b)
	res := poll(e.b, context.Background(), cinode, 0, 20*time.Second)
	ft.waitStarted(t) // the waiter has read the store, found nothing and is waiting
	start := time.Now()
	e.post(t, duet, toCrew("wake-up"))
	r := within(t, res, 5*time.Second, "waiter")
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("waiter returned %v after the commit, want within 200ms", took)
	}
	if r.err != nil || len(r.u.Events) != 1 || r.u.Events[0].Handle != "wake-up" {
		t.Errorf("result = %+v: the wake must bring what the store holds", r)
	}
	if ft.stopped.Load() != 1 {
		t.Error("the timeout timer was not stopped")
	}
}

// countingStore counts EventsSince calls, one per read a waiter makes.
type countingStore struct {
	bus.Store
	reads *atomic.Int32
}

func (c countingStore) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return c.Store.Tx(ctx, func(x bus.Tx) error { return fn(countingTx{x, c.reads}) })
}

type countingTx struct {
	bus.Tx
	reads *atomic.Int32
}

func (c countingTx) EventsSince(s string, since int64, limit int) ([]bus.Event, error) {
	c.reads.Add(1)
	return c.Tx.EventsSince(s, since, limit)
}

func TestAnswersLongPollIgnoresOtherSwarms(t *testing.T) {
	e := newEnv(t)
	var reads atomic.Int32
	b := bus.New(countingStore{e.d, &reads}, e.now, e.l)
	ft := newFakeTimer(t, b)
	res := poll(b, context.Background(), cinode, 0, 20*time.Second)
	ft.waitStarted(t)
	if got := reads.Load(); got != 1 {
		t.Fatalf("reads before any commit = %d, want 1", got)
	}
	if _, err := b.Post(context.Background(), duet, func() bus.Draft { d := toCrew("elsewhere"); d.To = "client"; return d }()); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-res:
		t.Fatalf("a post to another swarm woke the waiter: %+v", r)
	case <-time.After(100 * time.Millisecond):
	}
	if got := reads.Load(); got != 1 {
		t.Errorf("reads after another swarm's commit = %d, want 1: the waiter must not wake", got)
	}
	ft.fire <- time.Now()
	r := within(t, res, 5*time.Second, "waiter after its timeout")
	if r.err != nil || len(r.u.Events) != 0 || r.u.Next != 0 {
		t.Errorf("timed-out result = %+v, want empty with Next == since", r)
	}
}

func TestAnswersLongPollTimesOutEmpty(t *testing.T) {
	e := newEnv(t)
	ft := newFakeTimer(t, e.b)
	e.post(t, cinode, draft(bus.KindQuestion, "mine", bus.UrgencyDigest)) // the node's own event is not news
	res := poll(e.b, context.Background(), cinode, 0, 10*time.Second)
	if d := ft.waitStarted(t); d != 10*time.Second {
		t.Errorf("timer = %v, want the wait asked for", d)
	}
	ft.fire <- time.Now()
	r := within(t, res, 5*time.Second, "waiter")
	if r.err != nil || len(r.u.Events) != 0 {
		t.Errorf("result = %+v, want empty", r)
	}
	if r.u.Next == 0 {
		t.Error("Next = 0: it must pass the node's own event")
	}
}

func TestAnswersLongPollKeepsWaitingThroughOwnEvents(t *testing.T) {
	e := newEnv(t)
	ft := newFakeTimer(t, e.b)
	res := poll(e.b, context.Background(), cinode, 0, 10*time.Second)
	ft.waitStarted(t)
	e.post(t, cinode, draft(bus.KindQuestion, "mine", bus.UrgencyDigest)) // wakes the swarm, but is not news to this node
	select {
	case r := <-res:
		t.Fatalf("returned on the node's own event: %+v", r)
	case <-time.After(100 * time.Millisecond):
	}
	e.post(t, duet, toCrew("news"))
	r := within(t, res, 5*time.Second, "waiter")
	if len(r.u.Events) != 1 || r.u.Events[0].Handle != "news" {
		t.Errorf("result = %+v", r)
	}
}

func TestAnswersWaitIsCappedAt25s(t *testing.T) {
	e := newEnv(t)
	ft := newFakeTimer(t, e.b)
	res := poll(e.b, context.Background(), cinode, 0, time.Hour)
	if d := ft.waitStarted(t); d != 25*time.Second {
		t.Errorf("timer = %v, want 25s", d)
	}
	ft.fire <- time.Now()
	within(t, res, 5*time.Second, "waiter")
}

func TestAnswersZeroOrNegativeWaitDoesNotBlock(t *testing.T) {
	e := newEnv(t)
	ft := newFakeTimer(t, e.b)
	for _, w := range []time.Duration{0, -time.Second} {
		within(t, poll(e.b, context.Background(), cinode, 0, w), 5*time.Second, "no-wait read")
	}
	select {
	case <-ft.started:
		t.Error("a timer was started for a read that does not wait")
	default:
	}
}

func TestAnswersLongPollCancelReturnsAtOnce(t *testing.T) {
	e := newEnv(t)
	ft := newFakeTimer(t, e.b)
	before := runtime.NumGoroutine()
	var results []<-chan pollResult
	var cancels []context.CancelFunc
	for range 20 {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		results = append(results, poll(e.b, ctx, cinode, 0, 20*time.Second))
		ft.waitStarted(t)
	}
	for _, c := range cancels {
		c()
	}
	for _, r := range results {
		got := within(t, r, 5*time.Second, "cancelled waiter")
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", got.err)
		}
	}
	if ft.stopped.Load() != 20 {
		t.Errorf("%d timers stopped, want 20", ft.stopped.Load())
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Errorf("%d goroutines after the waiters left, %d before", n, before)
	}
}

func TestAnswersLongPollSeesACommitBetweenReadAndWait(t *testing.T) {
	// The waiter subscribes before it reads, so a commit that lands at any point is seen: here,
	// posts race with a stream of polls and none is lost.
	e := newEnv(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 30 {
			d := toCrew("p" + string(rune('a'+i)))
			if _, err := e.b.Post(context.Background(), duet, d); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	var since int64
	got := 0
	deadline := time.Now().Add(20 * time.Second)
	for got < 30 && time.Now().Before(deadline) {
		u, err := e.b.Answers(context.Background(), cinode, since, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		got += len(u.Events)
		since = u.Next
	}
	<-done
	if got != 30 {
		t.Errorf("saw %d of 30 posts", got)
	}
}
