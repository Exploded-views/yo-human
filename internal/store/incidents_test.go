package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

const (
	envDir   = "YO_HUMAN_STORE_TEST_DIR"
	envStart = "YO_HUMAN_STORE_TEST_START"
	perChild = 50
)

// TestHelperProcess is the child of TestWithIncidentsSerializes; it does nothing unless the env is set.
func TestHelperProcess(t *testing.T) {
	dir := os.Getenv(envDir)
	if dir == "" {
		t.Skip("helper process only")
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ns, _ := strconv.ParseInt(os.Getenv(envStart), 10, 64)
	time.Sleep(time.Until(time.Unix(0, ns)))
	for range perChild {
		err := s.WithIncidents(func(m map[string]*Incident) error {
			inc := m["counter"]
			if inc == nil {
				inc = &Incident{Key: "counter"}
				m["counter"] = inc
			}
			n := inc.Rank
			time.Sleep(500 * time.Microsecond) // widen the read-modify-write window
			inc.Rank = n + 1
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWithIncidentsSerializes(t *testing.T) {
	s := open(t)
	start := strconv.FormatInt(time.Now().Add(400*time.Millisecond).UnixNano(), 10)
	var cmds []*exec.Cmd
	for range 2 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), envDir+"="+s.dir, envStart+"="+start)
		cmds = append(cmds, cmd)
	}
	for _, c := range cmds {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("child: %v", err)
		}
	}
	err := s.WithIncidents(func(m map[string]*Incident) error {
		c := m["counter"]
		if c == nil {
			t.Error("counter incident missing")
			return nil
		}
		if c.Rank != 2*perChild {
			t.Errorf("counter = %d, want %d", c.Rank, 2*perChild)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWithIncidentsPersistsAndRollsBackOnError(t *testing.T) {
	s := open(t)
	inc := &Incident{Key: "k", Level: "high", Rank: 3, Open: true, Opened: t0,
		Generation: 2, Reached: []Reached{{Instance: "ntfy", Delivered: []string{"fp-a", "fp-b"}, Scheduled: []ScheduledSend{{ID: "p1", Fingerprint: "fp-a"}}}}}
	if err := s.WithIncidents(func(m map[string]*Incident) error { m["k"] = inc; return nil }); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := s.WithIncidents(func(m map[string]*Incident) error {
		m["k"] = &Incident{Key: "k", Rank: 99}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want boom", err)
	}
	_ = s.WithIncidents(func(m map[string]*Incident) error {
		if !sameIncident(m["k"], inc) {
			t.Errorf("persisted = %+v, want the first write only", m["k"])
		}
		return nil
	})
	if fi, err := os.Stat(filepath.Join(s.dir, "incidents.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("incidents.json: %v %v, want mode 600", fi, err)
	}
}

func TestWithIncidentsRejectsCorruptFile(t *testing.T) {
	s := open(t)
	if err := os.WriteFile(filepath.Join(s.dir, "incidents.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := s.WithIncidents(func(map[string]*Incident) error { called = true; return nil }); err == nil || called {
		t.Errorf("corrupt file: err=%v called=%v, want an error and no callback", err, called)
	}
}

func TestAppendLogAppendsLinesMode600(t *testing.T) {
	s := open(t)
	for i := range 3 {
		if err := s.AppendLog(map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(s.dir, "log.jsonl")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log: %v %v, want mode 600", fi, err)
	}
	f, _ := os.Open(path)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var m map[string]int
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m["n"] != n {
			t.Fatalf("line %d = %q (%v)", n, sc.Text(), err)
		}
		n++
	}
	if n != 3 {
		t.Errorf("%d lines, want 3", n)
	}
}

func TestAppendLogRejectsUnmarshalable(t *testing.T) {
	if err := open(t).AppendLog(make(chan int)); err == nil {
		t.Error("want an error for a value that cannot be encoded")
	}
}

func sameIncident(got, want *Incident) bool {
	return got != nil && got.Rank == want.Rank && got.Level == want.Level && got.Open == want.Open &&
		len(got.Reached) == 1 && reflect.DeepEqual(got.Reached[0], want.Reached[0]) && got.Generation == want.Generation && got.Opened.Equal(want.Opened)
}
