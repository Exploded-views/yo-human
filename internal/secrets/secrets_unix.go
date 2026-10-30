//go:build unix

package secrets

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// openFlags opens read-only, refuses a symlink in the last element and never makes a terminal the
// controlling one. O_NONBLOCK keeps a FIFO swapped in after the lstat from hanging the open; the
// caller then refuses it.
const openFlags = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_NOCTTY

// isSymlinkError reports whether err is the refusal of a symlink by the open.
func isSymlinkError(err error) bool { return errors.Is(err, syscall.ELOOP) }

// ownedByMe reports whether the file belongs to the current user.
func ownedByMe(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Getuid()
}
