package store

import "testing"

func hasPending(t *testing.T, s *Store, inc string) bool {
	t.Helper()
	ok, err := s.HasPending(inc)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestHasPendingSeesQueuedAndInflightEntriesOfThatIncident(t *testing.T) {
	s := open(t)
	if hasPending(t, s, "inc") {
		t.Fatal("an empty store has nothing pending")
	}
	mustEnqueue(t, s, pend("01A.ntfy", "inc"))
	mustEnqueue(t, s, pend("01B.ntfy", "other"))
	if !hasPending(t, s, "inc") || hasPending(t, s, "none") {
		t.Fatal("queued: want pending for inc only")
	}
	c := mustClaim(t, s, "01A.ntfy")
	if !hasPending(t, s, "inc") {
		t.Fatal("in flight still counts as pending")
	}
	if err := s.Dead(c, "revoked"); err != nil {
		t.Fatal(err)
	}
	if hasPending(t, s, "inc") {
		t.Fatal("a dead entry is not pending")
	}
	mustEnqueue(t, s, pend("01C.ntfy", "inc"))
	c = mustClaim(t, s, "01C.ntfy")
	if err := s.Complete(c); err != nil {
		t.Fatal(err)
	}
	if hasPending(t, s, "inc") {
		t.Fatal("a sent entry is not pending")
	}
}
