package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// A message waiting for its incident check is not a pending send of that incident, queued or
// claimed: counting it would suppress its own repeat (R2).
func TestAWaitingMessageIsNotPending(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A"+admitSuffix, "inc"))
	if hasPending(t, s, "inc") {
		t.Fatal("a queued waiting message counts as pending")
	}
	mustClaim(t, s, "01A"+admitSuffix)
	if hasPending(t, s, "inc") {
		t.Fatal("a claimed waiting message counts as pending")
	}
}

// Unbounded waits for the lock past a deadline the store was given: a landing is recorded once the
// holder lets go, never dropped (R3).
func TestUnboundedWaitsPastTheDeadline(t *testing.T) {
	s := open(t)
	other, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = other.WithIncidents(func(map[string]*Incident) error { close(held); <-release; return nil })
	}()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	time.AfterFunc(200*time.Millisecond, func() { close(release) })
	if err := s.Within(ctx).Unbounded().WithIncidents(func(map[string]*Incident) error { return nil }); err != nil {
		t.Fatalf("Unbounded().WithIncidents = %v; want it to wait for the lock", err)
	}
}

// The incidents in use are those a queued or in-flight entry refers to, a waiting message included;
// a dead entry does not count, and a store that cannot be listed says so.
func TestIncidentsInUse(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A"+admitSuffix, "waiting"))
	mustEnqueue(t, s, pend("01B.ntfy", "claimed"))
	mustEnqueue(t, s, pend("01C.ntfy", "dead"))
	mustEnqueue(t, s, pend("01D.ntfy", ""))
	mustClaim(t, s, "01B.ntfy")
	if err := s.Dead(mustClaim(t, s, "01C.ntfy"), "revoked"); err != nil {
		t.Fatal(err)
	}
	got, err := s.IncidentsInUse()
	if err != nil || len(got) != 2 || !got["waiting"] || !got["claimed"] {
		t.Errorf("IncidentsInUse = %v, %v; want waiting and claimed only", got, err)
	}
	q := s.path("queue")
	if err := os.Chmod(q, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(q, 0o700) }()
	if _, err := s.IncidentsInUse(); err == nil && os.Geteuid() != 0 {
		t.Error("IncidentsInUse of an unreadable queue: no error")
	}
}

// WaitingOf lists the incident's waiting messages by queue key, queued or claimed, and nothing else.
func TestWaitingOfListsTheWaitingMessagesOfTheIncident(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A"+admitSuffix, "inc"))
	mustEnqueue(t, s, pend("01B"+admitSuffix, "inc"))
	mustEnqueue(t, s, pend("01C"+admitSuffix, "other"))
	mustEnqueue(t, s, pend("01D.ntfy", "inc"))
	mustClaim(t, s, "01B"+admitSuffix)
	got, err := s.WaitingOf("inc")
	if err != nil || strings.Join(got, ",") != "01A._admit,01B._admit" {
		t.Errorf("WaitingOf = %v, %v; want 01A._admit and 01B._admit", got, err)
	}
	in := s.path("inflight")
	if err := os.Chmod(in, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(in, 0o700) }()
	if _, err := s.WaitingOf("inc"); err == nil && os.Geteuid() != 0 {
		t.Error("WaitingOf of an unreadable inflight/: no error")
	}
}
