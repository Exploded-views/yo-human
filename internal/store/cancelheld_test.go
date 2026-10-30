package store

import (
	"testing"
	"time"
)

// CancelHeld removes only that incident's entries held for later: one due now, or another
// incident's held one, stays.
func TestCancelHeldRemovesOnlyThatIncidentsHeldEntries(t *testing.T) {
	s := open(t)
	held := pend("01A.ntfy", "inc")
	held.DeliverAt = t0.Add(time.Hour)
	other := pend("01B.ntfy", "other")
	other.DeliverAt = t0.Add(time.Hour)
	for _, p := range []Pending{held, pend("01C.ntfy", "inc"), other} {
		mustEnqueue(t, s, p)
	}
	gone, err := s.CancelHeld("inc", t0)
	if err != nil || len(gone) != 1 || gone[0].Key != "01A.ntfy" {
		t.Fatalf("CancelHeld = %+v, %v; want only the held entry of inc", gone, err)
	}
	due, _ := s.Due(t0.Add(2 * time.Hour))
	if len(due) != 2 {
		t.Errorf("left %+v; want the due entry of inc and the other incident's held one", due)
	}
	if gone, err := s.CancelHeld("inc", t0.Add(2*time.Hour)); err != nil || len(gone) != 0 {
		t.Errorf("again = %+v, %v; nothing held is left", gone, err)
	}
}
