package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseInflight(t *testing.T) {
	cases := []struct {
		name string
		key  string
		pid  int
		ok   bool
	}{
		{"A.x.123.json", "A.x", 123, true},
		{"A.x.123", "", 0, false},
		{"nodots.json", "", 0, false},
		{".5.json", "", 0, false},
		{"A.x.abc.json", "", 0, false},
		{"A.x.0.json", "", 0, false},
	}
	for _, c := range cases {
		key, pid, ok := parseInflight(c.name)
		if key != c.key || pid != c.pid || ok != c.ok {
			t.Errorf("parseInflight(%q) = %q %d %v, want %q %d %v", c.name, key, pid, ok, c.key, c.pid, c.ok)
		}
	}
}

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("own pid must be alive")
	}
	for _, pid := range []int{0, -1, deadPID} {
		if pidAlive(pid) {
			t.Errorf("pidAlive(%d) = true", pid)
		}
	}
}

func TestEnqueueWithoutDataRoundTrips(t *testing.T) {
	s := open(t)
	p := pend("A.x", "i")
	p.Data = nil
	mustEnqueue(t, s, p)
	due, _ := s.Due(t0)
	if len(due) != 1 || due[0].Data != nil {
		t.Errorf("due = %+v, want nil Data", due)
	}
}

func TestInvalidKeysAreRefusedByClaimAndDead(t *testing.T) {
	s := open(t)
	if _, _, err := s.Claim("../x"); err == nil {
		t.Error("Claim accepted an unsafe key")
	}
	if err := s.Dead(Claimed{Pending: Pending{Key: "a/b"}}, "r"); err == nil {
		t.Error("Dead accepted an unsafe key")
	}
}

func TestDeadEntriesAreOldestFirst(t *testing.T) {
	s := open(t)
	for _, k := range []string{"b.x", "a.x"} {
		mustEnqueue(t, s, pend(k, "i"))
		if err := s.Dead(mustClaim(t, s, k), "r"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	dead, _ := s.DeadEntries()
	if len(dead) != 2 || dead[0].Key != "b.x" || dead[1].Key != "a.x" {
		t.Errorf("dead order = %v, want b.x then a.x", dead)
	}
}

func TestMissingDirectoriesAreStoreErrors(t *testing.T) {
	s := open(t)
	for _, d := range []string{"queue", "inflight", "dead"} {
		if err := os.RemoveAll(filepath.Join(s.dir, d)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Due(t0); err == nil {
		t.Error("Due: want an error")
	}
	if _, err := s.CancelIncident("i"); err == nil {
		t.Error("CancelIncident: want an error")
	}
	if _, err := s.DeadEntries(); err == nil {
		t.Error("DeadEntries: want an error")
	}
	if _, _, _, err := s.Counts(); err == nil {
		t.Error("Counts: want an error")
	}
	if _, err := s.Recover(t0, 0, 0); err == nil {
		t.Error("Recover: want an error")
	}
}

func TestTrustRejectsCorruptFile(t *testing.T) {
	s := open(t)
	if err := os.WriteFile(filepath.Join(s.dir, "trust.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Trusted("/p", "x"); err == nil {
		t.Error("Trusted: want an error")
	}
	if err := s.Trust("/p", "x"); err == nil {
		t.Error("Trust: want an error")
	}
}

func TestEnqueueInMissingQueueDirFails(t *testing.T) {
	s := open(t)
	_ = os.RemoveAll(filepath.Join(s.dir, "queue"))
	if err := s.Enqueue(pend("A.x", "i")); err == nil {
		t.Error("Enqueue: want an error")
	}
	if err := s.AppendLog(1); err != nil {
		t.Errorf("AppendLog should not need queue/: %v", err)
	}
}

func TestStripStamp(t *testing.T) {
	cases := map[string]string{
		"A.x":                     "A.x",
		"A.x.1761480000123456789": "A.x",
		"A.x.12345":               "A.x.12345", // too short to be a stamp
		"A.x.17614800001234567ab": "A.x.17614800001234567ab",
		"plain":                   "plain",
		".1761480000123456789":    ".1761480000123456789",
	}
	for in, want := range cases {
		if got := stripStamp(in); got != want {
			t.Errorf("stripStamp(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinkFirstFree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := linkFirstFree(src, dir, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := linkFirstFree(src, dir, []string{"a", "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c")); err != nil {
		t.Errorf("second name not used when the first exists: %v", err)
	}
	if err := linkFirstFree(src, dir, []string{"a", "c"}); err == nil {
		t.Error("want an error when every name exists")
	}
	if err := linkFirstFree(filepath.Join(dir, "missing"), dir, []string{"z"}); err == nil {
		t.Error("want an error for a missing source")
	}
}
