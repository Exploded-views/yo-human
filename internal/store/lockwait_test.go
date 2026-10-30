package store

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// holdLock takes the incident lock through a second handle, as another process would, and keeps it
// until the test ends.
func holdLock(t *testing.T, dir string) {
	t.Helper()
	other, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = other.WithIncidents(func(map[string]*Incident) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	t.Cleanup(func() { close(release); <-done })
}

// A stopped holder (Ctrl-Z, a frozen network home) must not stall the call past its deadline: the
// wait ends with the context, as a Queued problem the caller can report with a next step.
func TestALockWaitEndsWithTheContext(t *testing.T) {
	s := open(t)
	holdLock(t, s.dir)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	got := make(chan error, 1)
	go func() { got <- s.Within(ctx).WithIncidents(func(map[string]*Incident) error { return nil }) }()
	select {
	case err := <-got:
		pr, ok := problem.As(err)
		if !IsBusy(err) || !ok || pr.Kind != problem.Queued {
			t.Errorf("WithIncidents = %v; want the busy lock as a Queued problem", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("still waiting for the lock 3s after a 150ms deadline")
	}
}
