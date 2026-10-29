package config

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
)

var (
	errTooBig     = errors.New("config file larger than 1 MiB")
	errNotRegular = errors.New("config file is not a regular file")
)

// Seams for tests: every config file is opened with openFile and its descriptor inspected with
// statOpen.
var (
	openFile = os.OpenFile
	statOpen = (*os.File).Stat
)

// openFlags never blocks on a FIFO and never makes a terminal the controlling one.
const openFlags = os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOCTTY

// readCapped reads a whole file, refusing one larger than maxBytes or that is not a regular file.
// A path that does not stat as a regular file is refused without being opened: opening a device
// can have side effects. The open descriptor is checked again, so a file swapped in between the
// stat and the open is refused too.
func readCapped(path string) ([]byte, error) {
	f, _, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return readOpen(f)
}

// openRegular opens path once and returns the descriptor with its own stat: every later check of
// the file is made on that descriptor, so nothing swapped in at the path afterwards is judged.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, errNotRegular
	}
	f, err := openFile(path, openFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	if fi, err = statOpen(f); err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			err = errNotRegular
		}
		return nil, nil, err
	}
	return f, fi, nil
}

// OpenRegular is openRegular for a caller outside the package (config trust): one open, without
// blocking and without taking a terminal, of a path that stats as a regular file, and the
// descriptor's own stat. IsNotRegular tells its refusal apart.
func OpenRegular(path string) (*os.File, os.FileInfo, error) { return openRegular(path) }

// IsNotRegular reports whether err is OpenRegular's refusal of a path that is not a regular file.
func IsNotRegular(err error) bool { return errors.Is(err, errNotRegular) }

// readOpen reads an open file whole, refusing one larger than maxBytes.
func readOpen(f *os.File) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err == nil && len(data) > maxBytes {
		err = errTooBig
	}
	return data, err
}

// reason is the system's short reason for err, such as "permission denied", without the path,
// which callers quote themselves.
func reason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "an unexpected error"
}
