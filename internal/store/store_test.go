package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// open returns a Store in a fresh state directory under t.TempDir().
func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOpenCreatesLayout(t *testing.T) {
	s := open(t)
	for _, d := range []string{"", "queue", "inflight", "dead"} {
		fi, err := os.Stat(filepath.Join(s.dir, d))
		if err != nil {
			t.Fatalf("stat %q: %v", d, err)
		}
		if got := fi.Mode().Perm(); got != 0o700 {
			t.Errorf("%q mode = %o, want 700", d, got)
		}
	}
}

func TestOpenAcceptsExistingPrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open on a 0700 dir: %v", err)
	}
}

func TestOpenRefusesLoosePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Open(dir)
	p, ok := problem.As(err)
	if !ok {
		t.Fatalf("Open error = %v, want a *problem.Problem", err)
	}
	if p.Kind != problem.Store {
		t.Errorf("Kind = %v, want Store", p.Kind)
	}
	if !strings.Contains(p.Fix, "chmod 700") || !strings.Contains(p.Fix, dir) {
		t.Errorf("Fix = %q, want chmod 700 <dir>", p.Fix)
	}
}

func TestOpenFailsWhenPathIsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(f)
	if p, ok := problem.As(err); !ok || p.Kind != problem.Store {
		t.Fatalf("Open on a file = %v, want a Store problem", err)
	}
}
