//go:build unix

package config

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByMe reports whether the file belongs to the current user.
func ownedByMe(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
