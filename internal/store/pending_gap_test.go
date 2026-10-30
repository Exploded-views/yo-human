package store

import (
	"testing"
	"time"
)

// gapRequeue checks HasPending, called holding the incident lock as the pipeline does, against an
// entry that something else tries to move from inflight/ back to queue/ between its two scans. The
// move must wait for the lock, so the entry is seen in inflight/.
func gapRequeue(t *testing.T, s *Store, incident string, requeue func() error) {
	t.Helper()
	done := make(chan error, 1)
	hasPendingGap = func() {
		hasPendingGap = nil
		go func() { done <- requeue() }()
		time.Sleep(150 * time.Millisecond) // long enough for an unlocked move to finish
	}
	t.Cleanup(func() { hasPendingGap = nil })
	var got bool
	if err := s.WithIncidents(func(map[string]*Incident) error {
		got = hasPending(t, s, incident)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("HasPending = false for an entry that was queued or in flight throughout")
	}
	if err := <-done; err != nil {
		t.Fatalf("requeue: %v", err)
	}
}

func TestHasPendingSeesAnEntryParkedBetweenItsScans(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A.ntfy", "inc"))
	c := mustClaim(t, s, "01A.ntfy")
	gapRequeue(t, s, "inc", func() error { return s.Park(c, t0, "not in this config") })
	wantCounts(t, s, 1, 0, 0)
}

func TestHasPendingSeesAnEntryRecoveredBetweenItsScans(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "01B.ntfy", deadPID, time.Hour) // a dead owner's stale entry of incident "i"
	gapRequeue(t, s, "i", func() error { _, err := s.Recover(t0, time.Second, 0); return err })
	wantCounts(t, s, 1, 0, 0)
}
