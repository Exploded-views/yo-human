package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// bodyFlags open --body-file without blocking on a FIFO and without taking a terminal.
const bodyFlags = os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOCTTY

// bodyOpenGap is a test seam: it runs between the stat of --body-file and its open. Nil in the binary.
var bodyOpenGap func()

// openBody opens --body-file once: a regular file that is not a config or secrets file and not in
// the user config directory. Every decision is made on the opened descriptor's identity, never on
// path text (a case-insensitive filesystem has many spellings of one path). A path that does not
// stat as a regular file is refused without being opened; the descriptor must be that same file.
func (e *env) openBody(file string) (*os.File, error) {
	fi, err := os.Stat(file)
	if err != nil {
		return nil, problem.Usagef("--body-file %s cannot be opened", "the path of a readable file", "check the path", problem.Clip(file))
	}
	if !fi.Mode().IsRegular() {
		return nil, notRegularBody(file)
	}
	if bodyOpenGap != nil {
		bodyOpenGap()
	}
	f, err := os.OpenFile(file, bodyFlags, 0) //nolint:gosec // the caller names their own file; protected files are refused below
	if err != nil {
		return nil, problem.Usagef("--body-file %s cannot be opened", "the path of a readable file", "check the path", problem.Clip(file))
	}
	got, err := f.Stat()
	if err != nil || !os.SameFile(fi, got) {
		_ = f.Close()
		return nil, notRegularBody(file)
	}
	if e.protected(file, got) {
		_ = f.Close()
		return nil, problem.Usagef("--body-file %s is in the user config directory, or is a config or secrets file", "a file outside $XDG_CONFIG_HOME/yo-human that no config layer reads",
			"never send a config or secrets file; write the body to a file elsewhere, or pass it with --body", problem.Clip(file))
	}
	if !e.insideProject(file) {
		_ = f.Close()
		return nil, problem.Usagef("--body-file %s is outside the working directory and the project", "a file inside the working directory or the project (the directory holding .yo-human.yaml)",
			"copy the text into the project, or pass it on stdin with --body -", problem.Clip(file))
	}
	return f, nil
}

// insideProject reports whether the file, symlinks resolved, sits under the working directory or
// the project root: the directory in which the search found the nearest project config file
// (trusted or not), an ancestor of the working directory. A symlinked project file never moves the
// root to where it points. Both are judged by identity, as protected is.
func (e *env) insideProject(file string) bool {
	p := e.paths()
	roots := []string{p.Cwd}
	if root := config.ProjectRoot(p.Cwd); root != "" {
		roots = append(roots, root)
	}
	for _, root := range roots {
		if fi, err := os.Stat(root); err == nil && underDir(file, fi) {
			return true
		}
	}
	return false
}

func notRegularBody(file string) error {
	return problem.Usagef("--body-file %s is not a regular file", "a regular file (not a FIFO, a device, a terminal or a directory)",
		"write the body to a file, or pipe it in with --body -", problem.Clip(file))
}

// protected reports whether the opened file (fi) is the secrets file, a config layer, or any file
// under the user config directory (a hard link included), or sits in a directory that is the user
// config directory under another spelling.
func (e *env) protected(file string, fi os.FileInfo) bool {
	p := e.paths()
	for _, path := range []string{e.secretsFile(), p.System, filepath.Join(p.UserDir, "config.yaml"), p.Explicit, config.ProjectFile(p.Cwd)} {
		if same(path, fi) {
			return true
		}
	}
	dir, err := os.Stat(p.UserDir) // with no user config directory there is nothing in it to protect
	return (err == nil && underDir(file, dir)) || holdsFile(p.UserDir, fi)
}

// same reports whether path names the file fi.
func same(path string, fi os.FileInfo) bool {
	if path == "" {
		return false
	}
	other, err := os.Stat(path)
	return err == nil && os.SameFile(fi, other)
}

// underDir reports whether any directory above file's resolved path is dir.
func underDir(file string, dir os.FileInfo) bool {
	abs, err := absPath(file)
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	for p := filepath.Dir(abs); ; p = filepath.Dir(p) {
		if same(p, dir) {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}

// holdsFile reports whether a file anywhere under root is fi.
func holdsFile(root string, fi os.FileInfo) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && same(path, fi) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
