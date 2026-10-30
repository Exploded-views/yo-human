//go:build unix

package logch

import "syscall"

// openFlags never follows a symlink at the path and never blocks opening a FIFO for writing.
const openFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
