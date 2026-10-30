package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/store"
)

// holdIncidentLock takes the incident lock in the sandbox's state directory, as another process
// would, until the test ends.
func (s *sandbox) holdIncidentLock() {
	s.t.Helper()
	st, err := store.Open(s.stateDir())
	if err != nil {
		s.t.Fatal(err)
	}
	held, rel, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = st.WithIncidents(func(map[string]*store.Incident) error { close(held); <-rel; return nil })
	}()
	<-held
	s.t.Cleanup(func() { close(rel); <-done })
}

// A resolve behind a held lock changes nothing: it exits 75, and its next step runs the same
// resolve again, which is safe to repeat.
func TestAResolveBehindAHeldLockSaysToRunItAgain(t *testing.T) {
	s, _ := notifySandbox(t)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "version: 1\n", "version: 1\ndeadline: 200ms\n", 1))
	s.notify("page", "--dedup-key", "disk 7")
	s.holdIncidentLock()
	got := make(chan result, 1)
	go func() { got <- s.do("", "resolve", "--dedup-key", "disk 7") }()
	var r result
	select {
	case r = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("resolve still waiting for the incident lock 5s after its 200ms deadline")
	}
	expect(t, r.code == ExitQueued && strings.HasPrefix(r.out, "RESULT queued"), "resolve: %+v; want exit 75 and RESULT queued", r)
	mustContain(t, "stdout", r.out, "next: yo-human resolve --dedup-key 'disk 7' in ")
}

// The same through the page shim, whose next step is the shim's own resolve.
func TestAPageResolveBehindAHeldLockSaysToRunItAgain(t *testing.T) {
	s, _ := notifySandbox(t)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "version: 1\n", "version: 1\ndeadline: 200ms\n", 1)+shimBlock)
	s.holdIncidentLock()
	r := s.do("", "page", "--resolve", "k1", "--json")
	var obj struct {
		Status string
		Next   struct{ Do string }
	}
	expect(t, r.code == ExitQueued && json.Unmarshal([]byte(r.out), &obj) == nil && obj.Status == "queued" && obj.Next.Do == "yo-human page --resolve k1",
		"page --resolve: %+v; want exit 75 and the same resolve as the next step", r)
}

// Status shows what it can read: the queue; the incidents wait for the lock, with a warning.
func TestStatusBehindAHeldLockWarnsAndShowsTheQueue(t *testing.T) {
	s, _ := notifySandbox(t)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "version: 1\n", "version: 1\ndeadline: 200ms\n", 1))
	s.holdIncidentLock()
	r := s.do("", "status")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "status: %+v; want RESULT ok", r)
	mustContain(t, "stderr", r.err, "warning: another yo-human process held the incident lock")
}

// A resolve run with --config names the same file in its next step.
func TestAResolveBehindAHeldLockKeepsItsConfig(t *testing.T) {
	s, _ := notifySandbox(t)
	own := s.ownConfig("c.yaml")
	write(t, own, strings.Replace(notifyConfigBody, "version: 1\n", "version: 1\ndeadline: 200ms\n", 1))
	s.holdIncidentLock()
	r := s.do("", "resolve", "--dedup-key", "k", "--project", "p", "--note", "fixed it", "--config", own)
	mustContain(t, "stdout", r.out, "next: yo-human resolve --dedup-key k --project p --note 'fixed it' --config "+own+" in ")
}
