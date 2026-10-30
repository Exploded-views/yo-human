//go:build e2e && linux

package e2e

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// openPTY opens a pseudo-terminal pair: the test holds the master and types on it, the binary gets
// the slave as its controlling terminal, as a human's shell would give it.
func openPTY() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	var unlock int32
	var n uint32
	for _, op := range []struct{ req, arg uintptr }{
		{syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))}, {syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))}, //nolint:gosec // ioctl takes a pointer
	} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), op.req, op.arg); errno != 0 {
			_ = m.Close()
			return nil, nil, errno
		}
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		return nil, nil, err
	}
	return m, s, nil
}
