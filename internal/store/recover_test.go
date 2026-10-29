package store

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

const deadPID = 2147483646

// inflightFile plants an inflight file for pid whose mtime is age before t0.
func inflightFile(t *testing.T, s *Store, key string, pid int, age time.Duration) string {
	t.Helper()
	mustEnqueue(t, s, pend(key, "i"))
	dst := filepath.Join(s.dir, "inflight", key+"."+itoa(pid)+".json")
	if err := os.Rename(filepath.Join(s.dir, "queue", key+".json"), dst); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dst, t0.Add(-age), t0.Add(-age)); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestRecoverStaleInflight(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "A.x", deadPID, time.Hour)
	n, err := s.Recover(t0, time.Minute, 0)
	if err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v, want 1", n, err)
	}
	if q, i, _ := counts(t, s); q != 1 || i != 0 {
		t.Errorf("counts q=%d i=%d, want 1 0", q, i)
	}
	if due, _ := s.Due(t0); len(due) != 1 || due[0].Key != "A.x" {
		t.Errorf("due after recover = %v", due)
	}
}

func TestRecoverSkipsLiveOwner(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "A.x", os.Getpid(), time.Hour)
	n, err := s.Recover(t0, time.Minute, 0)
	if err != nil || n != 0 {
		t.Fatalf("Recover = %d, %v, want 0", n, err)
	}
	if _, i, _ := counts(t, s); i != 1 {
		t.Errorf("inflight = %d, want 1", i)
	}
}

func TestRecoverSkipsFreshInflight(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "A.x", deadPID, time.Second)
	if n, err := s.Recover(t0, time.Minute, 0); err != nil || n != 0 {
		t.Fatalf("Recover = %d, %v, want 0 for a young file", n, err)
	}
}

func TestRecoverIgnoresUnparseableNames(t *testing.T) {
	s := open(t)
	if err := os.WriteFile(filepath.Join(s.dir, "inflight", "junk.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Recover(t0, 0, 0); err != nil || n != 0 {
		t.Fatalf("Recover = %d, %v, want 0", n, err)
	}
}

func TestClaimRefreshesMtimeSoFreshClaimsAreNotStale(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("A.x", "i"))
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(s.dir, "queue", "A.x.json"), old, old); err != nil {
		t.Fatal(err)
	}
	c := mustClaim(t, s, "A.x")
	fi, err := os.Stat(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(fi.ModTime()) > time.Minute {
		t.Errorf("inflight mtime %v is old; a claim must stamp it", fi.ModTime())
	}
}

func TestRecoverIncrementsAttempts(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "A.x", deadPID, time.Hour)
	if n, err := s.Recover(t0, time.Minute, 0); err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v", n, err)
	}
	due, _ := s.Due(t0)
	if len(due) != 1 || due[0].Attempts != 1 {
		t.Errorf("due = %+v, want Attempts 1 (a crash counts as an attempt)", due)
	}
}

func TestRecoverNeverOverwritesNewerQueueEntry(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "A.x", deadPID, time.Hour)
	newer := pend("A.x", "i")
	newer.Attempts = 7
	mustEnqueue(t, s, newer)
	n, err := s.Recover(t0, time.Minute, 0)
	if err != nil || n != 0 {
		t.Fatalf("Recover = %d, %v, want 0", n, err)
	}
	due, _ := s.Due(t0)
	if len(due) != 1 || due[0].Attempts != 7 {
		t.Errorf("due = %+v, want the newer entry (Attempts 7) untouched", due)
	}
	wantCounts(t, s, 1, 0, 0)
}

func TestRecoverQuarantinesUnreadableInflight(t *testing.T) {
	s := open(t)
	dst := filepath.Join(s.dir, "inflight", "A.x."+itoa(deadPID)+".json")
	if err := os.WriteFile(dst, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := t0.Add(-time.Hour)
	_ = os.Chtimes(dst, old, old)
	if n, err := s.Recover(t0, time.Minute, 0); err != nil || n != 0 {
		t.Fatalf("Recover = %d, %v, want 0", n, err)
	}
	wantCounts(t, s, 0, 0, 1)
}

func TestRecoverRemovesStaleTempFiles(t *testing.T) {
	s := open(t)
	old := t0.Add(-time.Hour)
	fresh := t0.Add(-time.Second)
	for _, sub := range []string{"", "queue", "inflight", "dead"} {
		stale := filepath.Join(s.dir, sub, ".tmp-stale")
		young := filepath.Join(s.dir, sub, ".tmp-young")
		for p, mt := range map[string]time.Time{stale: old, young: fresh} {
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.Chtimes(p, mt, mt)
		}
		if _, err := s.Recover(t0, time.Minute, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("%q: stale temp file still present (%v)", sub, err)
		}
		if _, err := os.Stat(young); err != nil {
			t.Errorf("%q: young temp file was removed: %v", sub, err)
		}
	}
}

// No yo-human call lives for maxAge: an inflight entry older than that is requeued even if a
// process with its pid is alive (the pid has been reused), so a page is never stranded in flight.
func TestRecoverRequeuesAnInflightEntryOlderThanMaxAgeWhateverItsPid(t *testing.T) {
	s := open(t)
	inflightFile(t, s, "A.x", os.Getpid(), 11*time.Minute)
	inflightFile(t, s, "B.x", os.Getpid(), 9*time.Minute)
	n, err := s.Recover(t0, time.Minute, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v, want 1 (only the entry older than maxAge)", n, err)
	}
	if due, _ := s.Due(t0); len(due) != 1 || due[0].Key != "A.x" {
		t.Errorf("due after recover = %v, want A.x", due)
	}
	if _, i, _ := counts(t, s); i != 1 {
		t.Errorf("inflight = %d, want B.x left with its live owner", i)
	}
}
