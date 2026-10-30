package store

import (
	"fmt"
	"testing"
)

// mover moves the claimed entry of "inc" from inflight/ to queue/ after the first directory
// listing, as the unlocked Release of a message whose incident check found the lock busy does.
func mover(t *testing.T, s *Store) {
	t.Helper()
	mustEnqueue(t, s, pend("01A.ntfy", "inc"))
	c := mustClaim(t, s, "01A.ntfy")
	inUseGap = func() {
		inUseGap = nil
		if err := s.Release(c, t0); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { inUseGap = nil })
}

// An entry moved between the listings, by a Release that cannot wait for the busy lock, is still
// seen: a record it needs is never pruned.
func TestIncidentsInUseSeesAnEntryMovedBetweenItsScans(t *testing.T) {
	s := open(t)
	mover(t, s)
	got, err := s.IncidentsInUse()
	if err != nil || !got["inc"] {
		t.Errorf("IncidentsInUse = %v, %v; want inc, its entry was queued or in flight throughout", got, err)
	}
}

// A queue that keeps changing gives no two passes that agree: IncidentsInUse says so, and nothing
// is pruned on it.
func TestIncidentsInUseWithoutAStableAnswerFails(t *testing.T) {
	s := open(t)
	n := 0
	inUseGap = func() { // another process keeps queueing messages of new incidents
		n++
		mustEnqueue(t, s, pend(fmt.Sprintf("01%03d.ntfy", n), fmt.Sprintf("inc%d", n)))
	}
	t.Cleanup(func() { inUseGap = nil })
	if got, err := s.IncidentsInUse(); err == nil {
		t.Errorf("IncidentsInUse = %v, nil; want an error when no two passes agree", got)
	}
}

// One pass alone sees an entry that moves once during it: inflight/, queue/, then inflight/ again.
func TestOneInUsePassSeesAnEntryMovedOnce(t *testing.T) {
	s := open(t)
	mover(t, s)
	got, err := s.inUsePass()
	if err != nil || !got["inc"] {
		t.Errorf("inUsePass = %v, %v; want inc", got, err)
	}
}
