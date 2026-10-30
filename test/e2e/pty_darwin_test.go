//go:build e2e && darwin

package e2e

import (
	"os"
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
	var name [128]byte
	for _, op := range []struct{ req, arg uintptr }{
		{syscall.TIOCPTYGRANT, 0}, {syscall.TIOCPTYUNLK, 0}, {syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))}, //nolint:gosec // ioctl takes a pointer
	} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), op.req, op.arg); errno != 0 {
			_ = m.Close()
			return nil, nil, errno
		}
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	s, err := os.OpenFile(string(name[:n]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		return nil, nil, err
	}
	return m, s, nil
}
