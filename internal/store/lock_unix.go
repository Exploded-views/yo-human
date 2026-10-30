//go:build unix

package store

import (
	"errors"
	"os"
	"syscall"
)

// lockExclusive takes a blocking exclusive flock on f. The lock is released when f is closed.
func lockExclusive(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX) //nolint:gosec // fds fit in int
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// tryLockExclusive takes an exclusive flock on f without waiting; busy reports that another holder
// has it.
func tryLockExclusive(f *os.File) (busy bool, err error) {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // fds fit in int
		switch {
		case err == nil:
			return false, nil
		case errors.Is(err, syscall.EWOULDBLOCK):
			return true, nil
		case !errors.Is(err, syscall.EINTR):
			return false, err
		}
	}
}

// pidAlive reports whether a process with this pid exists. EPERM means it exists but belongs to
// someone else, so it counts as alive.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
