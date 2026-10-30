//go:build unix

package secrets_test

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/secrets"
)

// A FIFO where the file belongs would block an open; it must be refused instead.
func TestSecretsFIFORefusedWithoutHanging(t *testing.T) {
	path := privateFile(t)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := secrets.Load(path); done <- err }()
	select {
	case err := <-done:
		p := wantConfigProblem(t, err)
		if !strings.Contains(p.What, "not a regular file") {
			t.Errorf("What = %q", p.What)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Load hung on a FIFO")
	}
}
