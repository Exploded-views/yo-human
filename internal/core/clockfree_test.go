package core_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/core"
)

// R1, whatever the clocks say: a waiting message a flush has claimed is dropped by a resolve of its
// key that came first, when the resolve stamps the same instant the message was queued at, and when
// the clock has stepped back between them. The resolve lists the message on the closed record.
func TestAWaitingMessageIsDroppedByAResolveWhateverTheClock(t *testing.T) {
	for name, step := range map[string]time.Duration{"same instant": 0, "clock stepped back": -time.Second} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, incCfg(0, "a"), one)
			_, release := deferred(t, e)
			release()
			core.BeforeAdmit(e.p, func() {
				e.clk.Advance(step)
				if r := e.resolve("k", ""); r.Status != core.StatusNoop {
					t.Errorf("resolve = %q; want noop", r.Status)
				}
			})
			fr := e.flush()
			if n := sentCount(e, "a"); n != 0 || statuses(fr)["_admit"] != core.ChannelCancelled {
				t.Errorf("flush = %+v, sent %d; want the waiting message dropped, nothing sent", fr, n)
			}
			if inc := e.incident("k"); inc.Open {
				t.Errorf("incident %+v; want it closed", inc)
			}
		})
	}
}

// A message that starts waiting after the resolve is not the resolve's to drop: it is checked and
// sent, at the same instant too.
func TestAMessageWaitingAfterAResolveIsSent(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.resolve("k", "")
	_, release := deferred(t, e)
	release()
	fr := e.flush()
	if n := sentCount(e, "a"); n != 1 {
		t.Errorf("flush = %+v, sent %d; want the message sent once", fr, n)
	}
	if inc := e.incident("k"); !inc.Open {
		t.Errorf("incident %+v; want it open", inc)
	}
}

// A waiting message the resolve could not list (written while the resolve held the lock, after its
// listing; the queue is hidden from the resolve here) is still dropped by the time rule: it was
// queued before the close.
func TestAWaitingMessageTheResolveCouldNotListIsDroppedByTime(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	_, release := deferred(t, e)
	release()
	hidden := hideQueue(t, e)
	e.clk.Advance(time.Second)
	e.resolve("k", "")
	hidden()
	fr := e.flush()
	if n := sentCount(e, "a"); n != 0 || statuses(fr)["_admit"] != core.ChannelCancelled {
		t.Errorf("flush = %+v, sent %d; want the waiting message dropped", fr, n)
	}
}

// The list survives a reopen: a message the resolve withdrew, checked only after the key opened
// again, is dropped rather than sent as a louder repeat of the new incident.
func TestAWithdrawnMessageIsDroppedAfterTheKeyReopens(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	_, release := deferred(t, e) // a page, waiting
	release()
	core.BeforeAdmit(e.p, func() {
		e.resolve("k", "")
		e.say("low", "k", "new") // reopens the key, quieter
	})
	fr := e.flush()
	if n := sentCount(e, "a"); n != 1 || statuses(fr)["_admit"] != core.ChannelCancelled {
		t.Errorf("flush = %+v, sent %d; want only the reopening message sent", fr, n)
	}
}
