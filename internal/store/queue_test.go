package store

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func pend(key, incident string) Pending {
	return Pending{Key: key, IncidentKey: incident, Data: []byte(`{"title":"t"}`), NextAttempt: t0, DeliverAt: t0}
}

func samePending(a, b Pending) bool {
	return a.Key == b.Key && a.IncidentKey == b.IncidentKey && a.Attempts == b.Attempts &&
		string(a.Data) == string(b.Data) && a.NextAttempt.Equal(b.NextAttempt) && a.DeliverAt.Equal(b.DeliverAt)
}

func mustEnqueue(t *testing.T, s *Store, p Pending) {
	t.Helper()
	if err := s.Enqueue(p); err != nil {
		t.Fatalf("Enqueue %s: %v", p.Key, err)
	}
}

func mustClaim(t *testing.T, s *Store, key string) Claimed {
	t.Helper()
	c, ok, err := s.Claim(key)
	if err != nil || !ok {
		t.Fatalf("Claim %s: ok=%v err=%v", key, ok, err)
	}
	return c
}

func wantCounts(t *testing.T, s *Store, q, i, d int) {
	t.Helper()
	if gq, gi, gd := counts(t, s); gq != q || gi != i || gd != d {
		t.Fatalf("counts = %d %d %d, want %d %d %d", gq, gi, gd, q, i, d)
	}
}

func counts(t *testing.T, s *Store) (q, i, d int) {
	t.Helper()
	q, i, d, err := s.Counts()
	if err != nil {
		t.Fatal(err)
	}
	return q, i, d
}

func TestEnqueueRoundTripsAndCounts(t *testing.T) {
	s := open(t)
	p := pend("01A.ntfy", "inc")
	p.Attempts = 2
	mustEnqueue(t, s, p)
	due, err := s.Due(t0)
	if err != nil || len(due) != 1 {
		t.Fatalf("Due = %v, %v", due, err)
	}
	g := due[0]
	if !samePending(g, p) {
		t.Errorf("round trip = %+v, want %+v", g, p)
	}
	if q, i, d := counts(t, s); q != 1 || i != 0 || d != 0 {
		t.Errorf("counts = %d %d %d, want 1 0 0", q, i, d)
	}
}

func TestEnqueueRejectsUnsafeKeys(t *testing.T) {
	s := open(t)
	for _, k := range []string{"", "../x", "a/b", ".hidden", "a\x00b"} {
		if err := s.Enqueue(pend(k, "i")); err == nil {
			t.Errorf("Enqueue(%q) = nil, want an error", k)
		}
	}
	if q, _, _ := counts(t, s); q != 0 {
		t.Errorf("queued = %d, want 0", q)
	}
}

func TestClaimIsExclusive(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A.ntfy", "i"))
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := s.Claim("01A.ntfy")
			if err != nil {
				t.Error(err)
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d claims won, want exactly 1", wins.Load())
	}
	if q, i, _ := counts(t, s); q != 0 || i != 1 {
		t.Errorf("counts q=%d i=%d, want 0 1", q, i)
	}
}

func TestClaimMovesFileByRename(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("01A.ntfy", "i"))
	before, _ := os.Stat(filepath.Join(s.dir, "queue", "01A.ntfy.json"))
	c := mustClaim(t, s, "01A.ntfy")
	if string(c.Data) != `{"title":"t"}` || c.Key != "01A.ntfy" {
		t.Errorf("claimed = %+v", c.Pending)
	}
	after, err := os.Stat(filepath.Join(s.dir, "inflight", "01A.ntfy."+itoa(os.Getpid())+".json"))
	if err != nil {
		t.Fatalf("inflight file: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("claim must move the same file (rename), not copy it")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "queue", "01A.ntfy.json")); !os.IsNotExist(err) {
		t.Errorf("queue file still present: %v", err)
	}
}

func TestClaimMissingIsFalse(t *testing.T) {
	_, ok, err := open(t).Claim("nope")
	if ok || err != nil {
		t.Errorf("Claim missing: ok=%v err=%v, want false nil", ok, err)
	}
}

func TestDueRespectsNextAttemptAndDeliverAt(t *testing.T) {
	s := open(t)
	later := pend("B.x", "i")
	later.DeliverAt = t0.Add(time.Hour)
	backoff := pend("C.x", "i")
	backoff.NextAttempt = t0.Add(time.Minute)
	mustEnqueue(t, s, later)
	mustEnqueue(t, s, backoff)
	mustEnqueue(t, s, pend("A.x", "i"))
	keys := func(at time.Time) (ks []string) {
		due, err := s.Due(at)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range due {
			ks = append(ks, p.Key)
		}
		return ks
	}
	if got := keys(t0); len(got) != 1 || got[0] != "A.x" {
		t.Errorf("at t0 due = %v, want [A.x]", got)
	}
	if got := keys(t0.Add(time.Minute)); len(got) != 2 {
		t.Errorf("at +1m due = %v, want A.x and C.x", got)
	}
	if got := keys(t0.Add(time.Hour)); len(got) != 3 || got[0] != "A.x" {
		t.Errorf("at +1h due = %v, want all three, A.x first", got)
	}
}

func TestDueIsOldestFirst(t *testing.T) {
	s := open(t)
	for i, k := range []string{"c.x", "a.x", "b.x"} {
		p := pend(k, "i")
		p.NextAttempt = t0.Add(-time.Duration(i) * time.Minute)
		mustEnqueue(t, s, p)
	}
	due, _ := s.Due(t0)
	if len(due) != 3 || due[0].Key != "b.x" || due[1].Key != "a.x" || due[2].Key != "c.x" {
		t.Errorf("order = %v, want b a c", due)
	}
}

func TestCompleteRemovesInflight(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("A.x", "i"))
	if err := s.Complete(mustClaim(t, s, "A.x")); err != nil {
		t.Fatal(err)
	}
	if q, i, d := counts(t, s); q+i+d != 0 {
		t.Errorf("counts = %d %d %d, want all 0", q, i, d)
	}
}

func TestRetryIncrementsAttempts(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("A.x", "i"))
	next := t0.Add(time.Minute)
	if err := s.Retry(mustClaim(t, s, "A.x"), next); err != nil {
		t.Fatal(err)
	}
	if q, i, _ := counts(t, s); q != 1 || i != 0 {
		t.Fatalf("counts q=%d i=%d, want 1 0", q, i)
	}
	if due, _ := s.Due(t0); len(due) != 0 {
		t.Error("retried entry is due before its next attempt")
	}
	due, _ := s.Due(next)
	if len(due) != 1 || due[0].Attempts != 1 || !due[0].NextAttempt.Equal(next) {
		t.Errorf("due = %+v, want Attempts 1 at %v", due, next)
	}
}

func TestDeadIsKeptAndListed(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("A.x", "inc"))
	if err := s.Dead(mustClaim(t, s, "A.x"), "rejected: 400"); err != nil {
		t.Fatal(err)
	}
	wantCounts(t, s, 0, 0, 1)
	dead, err := s.DeadEntries()
	if err != nil || len(dead) != 1 || dead[0].At.IsZero() {
		t.Fatalf("DeadEntries = %v, %v", dead, err)
	}
	if dead[0].Key != "A.x" || dead[0].IncidentKey != "inc" || dead[0].Reason != "rejected: 400" {
		t.Errorf("dead entry = %+v", dead[0])
	}
}

func TestCancelIncidentRemovesOnlyThatIncident(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("A.x", "one"))
	mustEnqueue(t, s, pend("B.y", "one"))
	mustEnqueue(t, s, pend("C.x", "two"))
	got, err := s.CancelIncident("one")
	if err != nil || len(got) != 2 {
		t.Fatalf("CancelIncident = %v, %v, want 2 entries", got, err)
	}
	due, _ := s.Due(t0)
	if len(due) != 1 || due[0].Key != "C.x" {
		t.Errorf("left in queue = %v, want only C.x", due)
	}
}

func TestCancelIncidentSkipsClaimedEntries(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("A.x", "one"))
	mustClaim(t, s, "A.x")
	got, err := s.CancelIncident("one")
	if err != nil || len(got) != 0 {
		t.Errorf("CancelIncident = %v, %v, want nothing (already in flight)", got, err)
	}
}

func TestReleaseKeepsAttemptsAndIsDueNow(t *testing.T) {
	s := open(t)
	p := pend("A.x", "inc")
	p.Attempts = 2
	p.NextAttempt = t0.Add(time.Hour) // a claimed entry's old backoff must not delay the release
	mustEnqueue(t, s, p)
	c := mustClaim(t, s, "A.x")
	now := t0.Add(5 * time.Minute) // the caller's clock, not the real one
	if err := s.Release(c, now); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if q, i, d := counts(t, s); q != 1 || i != 0 || d != 0 {
		t.Fatalf("counts = %d %d %d, want 1 0 0", q, i, d)
	}
	due, err := s.Due(t0.Add(1000 * time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("Due = %v, %v", due, err)
	}
	wantUnchanged := p
	wantUnchanged.NextAttempt = due[0].NextAttempt
	if !samePending(due[0], wantUnchanged) {
		t.Errorf("released = %+v, want the entry unchanged with Attempts 2", due[0])
	}
	if n := due[0].NextAttempt; !n.Equal(now) {
		t.Errorf("NextAttempt = %v, want the caller's now %v", n, now)
	}
}

func TestReleaseRefusesAnUnnameableKey(t *testing.T) {
	if err := open(t).Release(Claimed{}, t0); err == nil {
		t.Error("Release of an entry with no key succeeded")
	}
}

func TestParkKeepsAttemptsAndRecordsWhy(t *testing.T) {
	s := open(t)
	p := pend("A.x", "inc")
	p.Attempts = 2
	mustEnqueue(t, s, p)
	c := mustClaim(t, s, "A.x")
	if err := s.Park(c, t0.Add(time.Minute), "channel x is not configured here"); err != nil {
		t.Fatalf("Park: %v", err)
	}
	due, err := s.Due(t0.Add(time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("Due = %v, %v", due, err)
	}
	if due[0].Attempts != 2 || due[0].Reason != "channel x is not configured here" {
		t.Errorf("parked = %+v; want 2 attempts and the reason", due[0])
	}
	if q, i, d := counts(t, s); q != 1 || i != 0 || d != 0 {
		t.Errorf("counts = %d %d %d, want 1 0 0", q, i, d)
	}
}

func TestReleaseAndRetryClearTheReason(t *testing.T) {
	s := open(t)
	p := pend("A.x", "inc")
	p.Reason = "parked earlier"
	mustEnqueue(t, s, p)
	if err := s.Release(mustClaim(t, s, "A.x"), t0); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.Due(t0.Add(time.Hour)); len(due) != 1 || due[0].Reason != "" {
		t.Errorf("after Release: %+v; want no reason", due)
	}
	p.Key = "B.x"
	mustEnqueue(t, s, p)
	if err := s.Retry(mustClaim(t, s, "B.x"), t0); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.Due(t0.Add(time.Hour)); len(due) != 2 || due[0].Reason != "" || due[1].Reason != "" {
		t.Errorf("after Retry: %+v; want no reason", due)
	}
}
