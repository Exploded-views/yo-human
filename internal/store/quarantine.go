package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// entryKey returns the queue key of a directory entry: the filename is the authoritative key.
func entryKey(e fs.DirEntry) (string, bool) {
	name := e.Name()
	if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
		return "", false
	}
	return strings.TrimSuffix(name, ".json"), true
}

// loadEntry reads an entry file whose name says its key is key. It reports vanished when the file
// is gone (another process moved it), or a non-empty bad reason when it cannot be used: unreadable,
// not JSON, or holding a different key than its name. w.Key is always set to key.
func loadEntry(path, key string) (w wire, bad string, vanished bool) {
	b, err := os.ReadFile(path) //nolint:gosec // path built by the store
	if errors.Is(err, fs.ErrNotExist) {
		return wire{}, "", true
	}
	if err == nil {
		err = json.Unmarshal(b, &w)
	}
	switch {
	case err != nil:
		bad = fmt.Sprintf("unreadable: %v", err)
	case w.Key != key:
		bad = fmt.Sprintf("unreadable: file holds key %q, its name says %q", w.Key, key)
	}
	w.Key = key
	return w, bad, false
}

// deadNames are the names a dead record for key may take, in order: nothing in dead/ is ever
// overwritten, so a reused key falls back to a name with a nanosecond stamp.
func deadNames(key string) []string {
	return []string{key + ".json", fmt.Sprintf("%s.%d.json", key, time.Now().UnixNano())}
}

// stripStamp removes a trailing ".<unixnano>" (18 or more digits) from a dead/ file name stem.
func stripStamp(stem string) string {
	i := strings.LastIndexByte(stem, '.')
	if i <= 0 || len(stem)-i-1 < 18 || strings.Trim(stem[i+1:], "0123456789") != "" {
		return stem
	}
	return stem[:i]
}

// deadEntryKey is the queue key of the dead/ file at path with this name stem. A stamp is stripped
// only when the file does not store the whole stem as its key, so an instance name that merely
// looks like a stamp is kept.
func deadEntryKey(path, stem string) string {
	stripped := stripStamp(stem)
	if stripped == stem {
		return stem
	}
	var w wire
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &w) == nil && w.Key == stem { //nolint:gosec // path built by the store
		return stem
	}
	return stripped
}

// quarantine links an unusable entry file into dead/ without replacing anything there (EEXIST falls
// back to a stamped name), and only then removes the source, so Counts and DeadEntries show it
// (with an "unreadable:" reason) and nothing is ever hidden. If the link fails the source stays
// where it is. A missing source means another process handled it; two racing quarantiners may leave
// two dead records, which is harmless because both are visible.
func (s *Store) quarantine(path, key string) error {
	if err := linkFirstFree(path, s.path("dead"), deadNames(key)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return ioProblem("cannot quarantine", path, err)
	}
	if err := removeFile(path); err != nil {
		return err
	}
	for _, d := range []string{filepath.Dir(path), s.path("dead")} {
		if err := syncDir(d); err != nil {
			return ioProblem("cannot sync", d, err)
		}
	}
	return nil
}

// linkFirstFree hard-links src into dir under the first of names that does not exist yet.
func linkFirstFree(src, dir string, names []string) error {
	err := fs.ErrExist
	for _, n := range names {
		if err = os.Link(src, filepath.Join(dir, n)); !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	return err
}

// publishNoClobber writes w to dst only if dst does not exist: a temp file, then os.Link (which
// fails with EEXIST rather than replacing). It reports whether dst was created.
func publishNoClobber(dst string, w wire) (bool, error) {
	b, err := json.Marshal(w)
	if err != nil {
		return false, ioProblem("cannot encode", dst, err)
	}
	dir := filepath.Dir(dst)
	tmp, err := writeTemp(dir, filepath.Base(dst), b, 0o600)
	if err != nil {
		return false, ioProblem("cannot write", dst, err)
	}
	defer func() { _ = os.Remove(tmp) }() //nolint:gosec // temp file this call created
	if err := os.Link(tmp, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, ioProblem("cannot link", dst, err)
	}
	if err := syncDir(dir); err != nil {
		return false, ioProblem("cannot sync", dir, err)
	}
	return true, nil
}

// listWires reads every entry file of a state subdirectory by filename. In queue/ an unusable file
// is quarantined to dead/ and listing carries on; in dead/ it is listed with an "unreadable:"
// reason. Files that vanish mid-listing (claimed or cancelled elsewhere) are skipped.
func (s *Store) listWires(sub string) ([]wire, error) {
	dir := s.path(sub)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, ioProblem("cannot list", dir, err)
	}
	var out []wire
	for _, e := range ents {
		key, ok := entryKey(e)
		if !ok {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if sub == "dead" {
			key = deadEntryKey(path, key)
		}
		w, bad, vanished := loadEntry(path, key)
		switch {
		case vanished:
			continue
		case bad != "" && sub == "queue":
			if err := s.quarantine(path, key); err != nil {
				return nil, err
			}
			continue
		case bad != "":
			w.Reason = bad
		}
		out = append(out, w)
	}
	return out, nil
}
