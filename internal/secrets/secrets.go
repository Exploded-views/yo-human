// Package secrets reads and writes the local secrets file: names to values, mode 0600 in a 0700
// directory. No function here returns a value except Load, and no error text ever holds one: parse
// failures report the file, never the parser's message, which can quote the text it choked on.
package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// namePattern is compiled on first use, not at init: init runs on every command, version included.
var namePattern = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`) })

// Entry is a secret's name and the size of its value, never the value.
type Entry struct {
	Name  string
	Bytes int
}

// maxBytes caps what Load reads; a secrets file is a few lines.
const maxBytes = 1 << 20

// Seams for tests: the secrets file is opened with openFile and its descriptor inspected with
// statOpen.
var (
	openFile = os.OpenFile
	statOpen = (*os.File).Stat
)

// Load returns every secret in the file at path. A missing file is an empty map. A directory
// looser than 0700, and a file that is a symlink, is not a regular file, is looser than 0600 or
// belongs to another user, are refused with a fix. The file is opened first and the open
// descriptor inspected, so what is checked is what is read.
func Load(path string) (map[string]string, error) {
	all, err := loadAll(path)
	if err != nil {
		return nil, err
	}
	for name := range all {
		if !ValidName(name) { // unusable: no reference can name it
			delete(all, name)
		}
	}
	return all, nil
}

// loadAll is Load without the name filter, for List and Set, which must see every entry.
func loadAll(path string) (map[string]string, error) {
	dir := filepath.Dir(path)
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm()&^0o700 != 0 {
		return nil, problem.Configf("the secrets directory %q is mode %o; secrets must not be readable by others",
			"mode 700 or tighter", "chmod 700 "+problem.ShellQuote(dir), dir, fi.Mode().Perm())
	}
	data, err := readPrivate(path)
	if err != nil {
		return nil, err
	}
	if data == nil { // no file yet
		return map[string]string{}, nil
	}
	m := map[string]string{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, problem.Configf("the secrets file %q is not a mapping of names to string values",
			"name: value pairs, one per line", "fix the file by hand, or delete it and re-run `yo-human secret set <name>` for each secret", path)
	}
	if m == nil { // a file holding only null
		m = map[string]string{}
	}
	return m, nil
}

// readPrivate refuses a path that is not a regular file from its lstat alone, without opening it
// (opening a device can have side effects), then opens it without following a symlink, checks the
// open descriptor and reads it. It returns nil, nil for a missing file.
func readPrivate(path string) ([]byte, error) {
	exists, p := checkPath(path)
	if p != nil {
		return nil, p
	}
	if !exists {
		return nil, nil
	}
	f, err := openFile(path, openFlags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if isSymlinkError(err) {
		return nil, symlinkProblem(path)
	}
	if err != nil {
		return nil, cannotRead(path, err)
	}
	defer func() { _ = f.Close() }()
	if p := checkOpenFile(f, path); p != nil {
		return nil, p
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return nil, problem.Configf("the secrets file %q cannot be read or is larger than 1 MiB", "a small file of name: value pairs",
			"check the file by hand", path)
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

// checkPath reports whether something exists at path, and refuses a symlink or anything else
// that is not a regular file.
func checkPath(path string) (bool, *problem.Problem) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, cannotRead(path, err)
	case fi.Mode()&fs.ModeSymlink != 0:
		return true, symlinkProblem(path)
	case !fi.Mode().IsRegular():
		return true, notRegular(path)
	}
	return true, nil
}

func symlinkProblem(path string) *problem.Problem {
	return problem.Configf("the secrets file %q is a symlink", "a regular file you own, mode 600",
		"remove the link with `rm "+problem.ShellQuote(path)+"`, then re-run `yo-human secret set <name>` for each secret", path)
}

func notRegular(path string) *problem.Problem {
	return problem.Configf("the secrets file %q is not a regular file", "a regular file you own, mode 600",
		"remove it with `rm "+problem.ShellQuote(path)+"`, then re-run `yo-human secret set <name>`", path)
}

func cannotRead(path string, err error) *problem.Problem {
	return problem.Configf("cannot read the secrets file %q: %s", "a file you can read, mode 600", "check its owner and mode, then retry", path, reason(err))
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

// checkOpenFile refuses an open file that is not a private regular file of the current user.
func checkOpenFile(f *os.File, path string) *problem.Problem {
	fi, err := statOpen(f)
	if err != nil {
		return cannotRead(path, err)
	}
	if !fi.Mode().IsRegular() {
		return notRegular(path)
	}
	if !ownedByMe(fi) {
		return problem.Configf("the secrets file %q belongs to another user", "a file you own, mode 600",
			"remove it with `rm "+problem.ShellQuote(path)+"`, then re-run `yo-human secret set <name>` for each secret", path)
	}
	if fi.Mode().Perm()&^0o600 != 0 {
		return problem.Configf("the secrets file %q is mode %o; secrets must not be readable by others",
			"mode 600 or tighter", "chmod 600 "+problem.ShellQuote(path), path, fi.Mode().Perm())
	}
	return nil
}

// ValidName reports whether name may name a secret.
func ValidName(name string) bool { return namePattern().MatchString(name) && !looksLikeHexToken(name) }

// looksLikeHexToken is true for 16 or more hex digits and nothing else: a pasted routing key or
// API token (PagerDuty's are 32 lower-case hex characters), not a name anyone chose.
func looksLikeHexToken(name string) bool {
	return len(name) >= 16 && strings.Trim(name, "0123456789abcdef") == ""
}

// Set stores value under name, replacing any earlier value. The value's trailing whitespace is
// trimmed and an empty value is refused. The write is atomic and the file is 0600.
func Set(path, name string, value []byte) error {
	if !ValidName(name) {
		return problem.Usagef("that is not a valid secret name (it is not echoed: a pasted value must never be printed)",
			"lower-case letters, digits and hyphens, starting with a letter or digit, 63 characters at most, and not a run of 16 or more hex digits",
			"choose a descriptive name such as pd-critical")
	}
	value = bytes.TrimRightFunc(value, unicode.IsSpace)
	if len(value) == 0 {
		return problem.Usagef("the value for %q is empty", "a non-empty value on stdin",
			`pipe it in: op read "op://<vault>/<item>/<field>" | yo-human secret set `+name, name)
	}
	m, err := loadAll(path)
	if err != nil {
		return err
	}
	m[name] = string(value)
	out, err := yaml.Marshal(m)
	if err != nil {
		return problem.Internalf("cannot encode the secrets file", "a mapping of names to strings", "report this as a bug")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return problem.Storef("cannot create the secrets directory %q", "a directory you can write", "check the permissions of its parents", filepath.Dir(path))
	}
	return store.WriteFileAtomic(path, out, 0o600)
}

// List returns each secret's name and value size, sorted by name.
func List(path string) ([]Entry, error) {
	m, err := loadAll(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(m))
	var bad []string
	for name, v := range m {
		if ValidName(name) {
			out = append(out, Entry{Name: name, Bytes: len(v)})
		} else {
			bad = append(bad, name)
		}
	}
	sort.Strings(bad) // numbered in key order; the text of the key is never shown
	for i, name := range bad {
		out = append(out, Entry{Name: fmt.Sprintf("<invalid name %d>", i+1), Bytes: len(m[name])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
