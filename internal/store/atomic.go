package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path so a reader sees the old file or the new one, never a partial
// one: a temp file in the same directory, fsync, chmod, rename, then fsync of the directory.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := writeTemp(dir, filepath.Base(path), data, perm)
	if err != nil {
		return ioProblem("cannot write", path, err)
	}
	if err := os.Rename(tmp, path); err != nil { //nolint:gosec // paths are built by the store
		_ = os.Remove(tmp) //nolint:gosec // temp file this call created
		return ioProblem("cannot replace", path, err)
	}
	if err := syncDir(dir); err != nil {
		return ioProblem("cannot sync", dir, err)
	}
	return nil
}

// writeTemp creates ".tmp-<base>-*" in dir holding data, synced and chmodded; on failure it is gone.
func writeTemp(dir, base string, data []byte, perm fs.FileMode) (name string, err error) {
	f, err := os.CreateTemp(dir, ".tmp-"+base+"-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name()) //nolint:gosec // temp file this call created
		}
	}()
	if _, err = f.Write(data); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Chmod(perm); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // dir is a path the store itself built
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// WriteFileExclusive creates path with data, or fails with an error wrapping fs.ErrExist if
// anything is already there. The content is written to a temp file and linked into place, so a
// reader never sees a partial file and two writers cannot both succeed.
func WriteFileExclusive(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := writeTemp(dir, filepath.Base(path), data, perm)
	if err != nil {
		return ioProblem("cannot write", path, err)
	}
	defer func() { _ = os.Remove(tmp) }() //nolint:gosec // temp file this call created
	if err := linkFile(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s: %w", path, fs.ErrExist)
		}
		return createExclusive(path, data, perm) // no hard links here
	}
	if err := syncDir(dir); err != nil {
		return ioProblem("cannot sync", dir, err)
	}
	return nil
}

// linkFile is a seam: a filesystem without hard links cannot be made to fail portably.
var linkFile = os.Link

// createExclusive is the fallback where hard links are unsupported: an O_EXCL create, written and
// synced in place. A crash can leave a short file, but a concurrent creator still cannot win twice.
func createExclusive(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // paths are built by the caller
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s: %w", path, fs.ErrExist)
	}
	if err != nil {
		return ioProblem("cannot create", path, err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path) //nolint:gosec // file this call created
		return ioProblem("cannot write", path, werr)
	}
	return nil
}
