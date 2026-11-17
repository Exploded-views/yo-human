package bus

import (
	"sync"
	"time"
)

// wakes is the in-process broadcast behind Answers' long-poll. It holds no goroutine and no
// per-waiter state: one channel per swarm that someone is waiting on, closed (and dropped) by
// the next commit for that swarm. A waiter that goes away leaves nothing behind. The data a
// woken waiter returns always comes from the store; the wake only says "look again".
type wakes struct {
	mu sync.Mutex
	ch map[string]chan struct{}
}

func newWakes() *wakes { return &wakes{ch: map[string]chan struct{}{}} }

// subscribe returns a channel that is closed by the next fire for swarm. Subscribe before
// reading the store: a commit that lands between the read and the wait then still wakes you.
func (w *wakes) subscribe(swarm string) <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.ch[swarm]
	if !ok {
		c = make(chan struct{})
		w.ch[swarm] = c
	}
	return c
}

func (w *wakes) fire(swarm string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.ch[swarm]; ok {
		close(c)
		delete(w.ch, swarm)
	}
}

// committed is the one call every write path makes after a transaction that appended an event
// to swarm's log has committed (not before: a waiter would then read the old state). Post,
// Reply and PushStatus call it; so must any new write that appends an event, such as an
// answer, an approval or a relay. A write that appends no event does not call it.
func (b *Bus) committed(swarm string) { b.wakes.fire(swarm) }

// timerFunc starts a timer: the channel fires once after d, and stop releases it early.
type timerFunc func(d time.Duration) (fired <-chan time.Time, stop func())

func realTimer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}
