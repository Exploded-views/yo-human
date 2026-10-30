//go:build unix

package config_test

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

type loadResult struct {
	cfg     *config.Config
	notices []config.Notice
	err     error
}

// loadWithin runs Load and fails the test if it blocks.
func loadWithin(t *testing.T, p config.Paths, tr config.Truster) loadResult {
	t.Helper()
	done := make(chan loadResult, 1)
	go func() {
		c, n, err := config.Load(p, registry(), tr)
		done <- loadResult{c, n, err}
	}()
	select {
	case r := <-done:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("Load hung on a FIFO")
		return loadResult{}
	}
}

func TestFIFOProjectFileIsANotice(t *testing.T) {
	dir := canon(t, t.TempDir())
	f := filepath.Join(dir, ".yo-human.yaml")
	if err := syscall.Mkfifo(f, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	r := loadWithin(t, userOnly(t, dir), truster{})
	if r.err != nil {
		t.Fatalf("Load: %v, want the FIFO ignored", r.err)
	}
	oneNotice(t, r.notices, "not a regular file")
}

func TestFIFOExplicitFileIsAConfigProblem(t *testing.T) {
	dir := canon(t, t.TempDir())
	f := filepath.Join(dir, "c.yaml")
	if err := syscall.Mkfifo(f, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	r := loadWithin(t, config.Paths{Explicit: f, Cwd: filepath.Join(dir, "none")}, truster{})
	p := wantProblem(t, r.err, problem.Config)
	if !strings.Contains(p.What, "not a regular file") || !strings.Contains(p.What, `"`+f+`"`) {
		t.Errorf("problem = %+v", p)
	}
}
