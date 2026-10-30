package store

import (
	"strings"
	"testing"
)

// Enqueue never replaces an entry: a key already queued is refused, and the first entry stays as it
// was. Two sends that share a key would otherwise leave one of them lost.
func TestEnqueueRefusesAKeyAlreadyQueued(t *testing.T) {
	s := open(t)
	first := pend("01A.ntfy", "inc")
	mustEnqueue(t, s, first)
	second := pend("01A.ntfy", "other")
	err := s.Enqueue(second)
	if err == nil || !strings.Contains(err.Error(), "01A.ntfy") {
		t.Fatalf("Enqueue twice = %v; want a refusal naming the key", err)
	}
	due, _ := s.Due(t0)
	if len(due) != 1 || due[0].IncidentKey != "inc" {
		t.Errorf("queue %+v; want the first entry kept as it was", due)
	}
}

// A claimed entry goes back to the queue on Retry. If an entry was queued under its key since the
// claim, that one is newer: it is kept, and the claimed copy dropped, as Recover does.
func TestARequeueNeverReplacesANewerEntry(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A.ntfy", "inc"))
	c := mustClaim(t, s, "01A.ntfy")
	if err := s.Retry(c, t0); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if due, _ := s.Due(t0); len(due) != 1 || due[0].Attempts != 1 {
		t.Fatalf("queue %+v; want the entry back on attempt 1", due)
	}
	c = mustClaim(t, s, "01A.ntfy")
	mustEnqueue(t, s, pend("01A.ntfy", "newer"))
	if err := s.Release(c, t0); err != nil {
		t.Fatalf("Release: %v", err)
	}
	due, _ := s.Due(t0)
	if q, in, _ := counts(t, s); len(due) != 1 || due[0].IncidentKey != "newer" || q != 1 || in != 0 {
		t.Errorf("queue %+v, %d queued, %d in flight; want the newer entry kept and the claimed copy gone", due, q, in)
	}
}
