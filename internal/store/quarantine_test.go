package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func plant(t *testing.T, s *Store, sub, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.dir, sub, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func onlyDead(t *testing.T, s *Store, key string) DeadEntry {
	t.Helper()
	dead, err := s.DeadEntries()
	if err != nil || len(dead) != 1 {
		t.Fatalf("DeadEntries = %v, %v, want exactly one", dead, err)
	}
	if dead[0].Key != key || !strings.HasPrefix(dead[0].Reason, "unreadable:") {
		t.Fatalf("dead entry = %+v, want key %q and an unreadable: reason", dead[0], key)
	}
	return dead[0]
}

func TestDueQuarantinesUnreadableFileAndKeepsListing(t *testing.T) {
	s := open(t)
	plant(t, s, "queue", "bad.x.json", "{not json")
	mustEnqueue(t, s, pend("good.x", "i"))
	due, err := s.Due(t0)
	if err != nil || len(due) != 1 || due[0].Key != "good.x" {
		t.Fatalf("Due = %v, %v, want only good.x", due, err)
	}
	wantCounts(t, s, 1, 0, 1)
	onlyDead(t, s, "bad.x")
}

func TestDueQuarantinesKeyMismatch(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("real.x", "i"))
	mustEnqueue(t, s, pend("good.x", "i"))
	if err := os.Rename(filepath.Join(s.dir, "queue", "real.x.json"), filepath.Join(s.dir, "queue", "other.x.json")); err != nil {
		t.Fatal(err)
	}
	due, err := s.Due(t0)
	if err != nil || len(due) != 1 || due[0].Key != "good.x" {
		t.Fatalf("Due = %v, %v, want only good.x", due, err)
	}
	wantCounts(t, s, 1, 0, 1)
	onlyDead(t, s, "other.x")
}

func TestCancelIncidentSurvivesCorruptFile(t *testing.T) {
	s := open(t)
	plant(t, s, "queue", "bad.x.json", "[")
	mustEnqueue(t, s, pend("good.x", "one"))
	got, err := s.CancelIncident("one")
	if err != nil || len(got) != 1 || got[0].Key != "good.x" {
		t.Fatalf("CancelIncident = %v, %v, want good.x", got, err)
	}
	wantCounts(t, s, 0, 0, 1)
}

func TestDeadEntriesListsUnreadableDeadFile(t *testing.T) {
	s := open(t)
	plant(t, s, "dead", "junk.x.json", "\x00\x01")
	onlyDead(t, s, "junk.x")
}

func TestClaimQuarantinesUnreadableEntry(t *testing.T) {
	s := open(t)
	plant(t, s, "queue", "bad.x.json", "{not json")
	_, ok, err := s.Claim("bad.x")
	if ok || err != nil {
		t.Fatalf("Claim = ok %v, err %v, want false nil", ok, err)
	}
	wantCounts(t, s, 0, 0, 1)
	onlyDead(t, s, "bad.x")
}

func deadReasons(t *testing.T, s *Store, key string) map[string][]byte {
	t.Helper()
	dead, err := s.DeadEntries()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, d := range dead {
		if d.Key != key {
			t.Errorf("dead entry key = %q, want %q", d.Key, key)
		}
		out[d.Reason] = d.Data
	}
	return out
}

func TestQuarantineNeverOverwritesExistingDeadRecord(t *testing.T) {
	s := open(t)
	mustEnqueue(t, s, pend("K.x", "i"))
	if err := s.Dead(mustClaim(t, s, "K.x"), "first"); err != nil {
		t.Fatal(err)
	}
	plant(t, s, "queue", "K.x.json", "{not json")
	if due, err := s.Due(t0); err != nil || len(due) != 0 {
		t.Fatalf("Due = %v, %v", due, err)
	}
	wantCounts(t, s, 0, 0, 2)
	got := deadReasons(t, s, "K.x")
	if len(got) != 2 || string(got["first"]) != `{"title":"t"}` {
		t.Errorf("dead entries by reason = %v, want the first record intact plus the quarantined one", got)
	}
}

func TestDeadTwiceOnSameKeyKeepsBoth(t *testing.T) {
	s := open(t)
	for i, reason := range []string{"one", "two"} {
		p := pend("K.x", "i")
		p.Data = []byte(`{"n":` + itoa(i) + `}`)
		mustEnqueue(t, s, p)
		if err := s.Dead(mustClaim(t, s, "K.x"), reason); err != nil {
			t.Fatal(err)
		}
	}
	wantCounts(t, s, 0, 0, 2)
	got := deadReasons(t, s, "K.x")
	if string(got["one"]) != `{"n":0}` || string(got["two"]) != `{"n":1}` {
		t.Errorf("dead entries by reason = %v, want both records with their own data", got)
	}
}

func TestQuarantineLinkFailureLeavesSourceVisible(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	s := open(t)
	plant(t, s, "queue", "bad.x.json", "{not json")
	dead := filepath.Join(s.dir, "dead")
	if err := os.Chmod(dead, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dead, 0o700) }()
	if _, err := s.Due(t0); err == nil {
		t.Error("Due: want the quarantine failure reported")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "queue", "bad.x.json")); err != nil {
		t.Fatalf("source must stay in queue/ when it cannot be quarantined: %v", err)
	}
	if err := os.Chmod(dead, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Due(t0); err != nil {
		t.Fatal(err)
	}
	wantCounts(t, s, 0, 0, 1)
}

func TestQuarantinedOldFileSurvivesRecover(t *testing.T) {
	s := open(t)
	plant(t, s, "queue", "K.x.json", "{not json")
	old := t0.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(s.dir, "queue", "K.x.json"), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Due(t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(t0, time.Minute, 0); err != nil {
		t.Fatal(err)
	}
	wantCounts(t, s, 0, 0, 1)
	onlyDead(t, s, "K.x")
}

func TestDeadKeepsInstanceNameOfManyDigits(t *testing.T) {
	s := open(t)
	key := "01A." + strings.Repeat("7", 18) // the instance looks like a nanosecond stamp
	mustEnqueue(t, s, pend(key, "i"))
	if err := s.Dead(mustClaim(t, s, key), "r"); err != nil {
		t.Fatal(err)
	}
	dead, err := s.DeadEntries()
	if err != nil || len(dead) != 1 || dead[0].Key != key {
		t.Fatalf("DeadEntries = %+v, %v, want key %q", dead, err, key)
	}
}
