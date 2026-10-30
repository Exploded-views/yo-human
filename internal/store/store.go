// Package store is the only write path for persistent state (constitution IV): atomic files, a
// queue whose entries are claimed by atomic rename, locked incidents, an append-only log and trust
// records. There is no daemon, so every operation must stay correct when many short-lived
// processes run at once. It imports only the standard library and internal/problem.
package store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// Store is a handle on one state directory. It holds no open files and no locks between calls.
type Store struct {
	dir string
	ctx context.Context // when set, a wait for the lock ends with it (see Within)
}

// Within is the store with every wait for the lock bounded by ctx: once ctx is done, the wait gives
// up with a Queued problem (IsBusy), and nothing is read or written. Without it a wait blocks.
func (s *Store) Within(ctx context.Context) *Store {
	c := *s
	c.ctx = ctx
	return &c
}

// Unbounded is the store with lock waits that block, whatever Within set: for a critical section of
// local writes that must not be dropped once the network work before it is done.
func (s *Store) Unbounded() *Store {
	c := *s
	c.ctx = nil
	return &c
}

// errBusy is the problem for a lock another process held past the call's deadline.
var errBusy = &problem.Problem{Kind: problem.Queued,
	What:  "another yo-human process held the incident lock past this call's deadline",
	Valid: "an incident lock that comes free within the deadline",
	Fix:   "run the next step; if this keeps happening, tell the human: a yo-human process may be stopped or hung"}

// IsBusy reports a wait for the lock that ran out (see Within).
func IsBusy(err error) bool { return errors.Is(err, errBusy) }

// Open creates the state directory (0700) with queue/, inflight/ and dead/ if missing, and refuses
// one that is group- or world-accessible.
func Open(dir string) (*Store, error) {
	if err := ensurePrivateDir(dir); err != nil {
		return nil, err
	}
	for _, sub := range []string{"queue", "inflight", "dead"} {
		if err := ensurePrivateDir(filepath.Join(dir, sub)); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir}, nil
}

func ensurePrivateDir(dir string) error {
	fi, err := os.Stat(dir) //nolint:gosec // the state directory is the caller's choice
	if errors.Is(err, fs.ErrNotExist) {
		return createPrivateDir(dir)
	}
	if err != nil {
		return ioProblem("cannot read", dir, err)
	}
	if !fi.IsDir() {
		return problem.Storef("%s is not a directory", "a directory", "remove it or choose another state directory", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return problem.Storef("state directory %s is accessible to others (mode %04o)", "mode 0700",
			"chmod 700 "+dir, dir, fi.Mode().Perm())
	}
	return nil
}

func createPrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // the state directory is the caller's choice
		return ioProblem("cannot create", dir, err)
	}
	// MkdirAll applies the umask and leaves intermediate directories alone; the leaf is made exact.
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 0700 is the private mode
		return ioProblem("cannot chmod", dir, err)
	}
	return nil
}

// ioProblem wraps a filesystem failure as a Store problem (exit 74).
func ioProblem(op, path string, err error) error {
	return problem.Storef("%s %s: %v", "a writable state directory",
		"check the permissions and free space of "+filepath.Dir(path), op, path, err)
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}
