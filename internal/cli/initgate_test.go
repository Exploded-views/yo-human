package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// config init with an explicit path writes where the caller says: it is the human's, like config
// trust. With no terminal it is refused and writes nothing.
func TestConfigInitAtAnExplicitPathNeedsTheHuman(t *testing.T) {
	s := newSandbox(t)
	target := filepath.Join(s.home, "mine.yaml")
	s.noTTY()
	wantHumanOnly(t, s.do("", "config", "init", "--config", target), "config init --config", "config init")
	_, err := os.Stat(target)
	expect(t, errors.Is(err, os.ErrNotExist), "config init wrote %s without the human: %v", target, err)
	shown := s.tty("no\n")
	s.do("", "config", "init", "--config", target).fails(t, "init answered no", ExitUsage, "did not confirm")
	mustContain(t, "terminal", shown.String(), target, "Type yes")
	s.tty("yes\n")
	s.do("", "config", "init", "--config", target).succeeds(t, "init answered yes")
	_, err = os.Stat(target)
	expect(t, err == nil, "init answered yes did not write: %v", err)
}

// The human's scripts may init an explicit path without a terminal; the run is audited.
func TestConfigInitAtAnExplicitPathNoninteractiveIsAudited(t *testing.T) {
	s := newSandbox(t)
	target := filepath.Join(s.home, "mine.yaml")
	s.noTTY()
	t.Setenv("YO_HUMAN_NONINTERACTIVE", "1")
	s.do("", "config", "init", "--config", target).succeeds(t, "noninteractive init")
	s.wantAudit("config-init", "path", target)
}

// config init never creates directories for an explicit path: the parent must already exist.
func TestConfigInitAtAnExplicitPathCreatesNoDirectory(t *testing.T) {
	s := newSandbox(t)
	target := filepath.Join(s.home, "new", "dir", "mine.yaml")
	s.do("", "config", "init", "--config", target).fails(t, "init into a missing directory", ExitUsage, "does not exist")
	_, err := os.Stat(filepath.Join(s.home, "new"))
	expect(t, errors.Is(err, os.ErrNotExist), "config init created a directory: %v", err)
}

// The default path is the human's own config: init there stays ungated.
func TestConfigInitAtTheDefaultPathNeedsNoTerminal(t *testing.T) {
	s := newSandbox(t)
	s.noTTY()
	s.do("", "config", "init").succeeds(t, "init at the default path")
	_, err := os.Stat(s.userConfig())
	expect(t, err == nil, "init did not write the user config: %v", err)
}

// config trust opens the file without blocking: one swapped for a FIFO after the path was resolved
// is refused at once, never hangs the human's terminal.
func TestConfigTrustOfAFileSwappedForAFIFODoesNotHang(t *testing.T) {
	s := newSandbox(t)
	path := write(t, filepath.Join(t.TempDir(), ".yo-human.yaml"), "version: 1\n")
	trustOpenGap = func() {
		_ = os.Remove(path)
		_ = syscall.Mkfifo(path, 0o600)
	}
	t.Cleanup(func() { trustOpenGap = nil })
	done := make(chan result, 1)
	go func() { done <- s.do("", "config", "trust", path) }()
	select {
	case r := <-done:
		r.fails(t, "trust of a swapped-in FIFO", ExitUsage, "not a regular file")
	case <-time.After(5 * time.Second):
		t.Fatal("config trust blocked on a FIFO")
	}
}

// The starter config tells whoever reads it that secrets are the human's to set, in their terminal.
func TestTheStarterSaysTheHumanSetsSecrets(t *testing.T) {
	mustContain(t, "starter", starterConfig, "the human", "in their own terminal")
	expect(t, !strings.Contains(starterConfig, "NONINTERACTIVE"), "the starter names the bypass")
}
