package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Recover returns to the queue (with Attempts+1) every inflight entry that is older than staleAfter
// and whose owner process is gone, and reports how many it requeued. It also deletes stale temp
// files. An entry owned by a live process is left alone, unless it is older than maxAge (when
// maxAge > 0): no call lives that long, so that pid now belongs to another process.
func (s *Store) Recover(now time.Time, staleAfter, maxAge time.Duration) (int, error) {
	if err := s.sweepTemps(now, staleAfter); err != nil {
		return 0, err
	}
	dir := s.path("inflight")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, ioProblem("cannot list", dir, err)
	}
	moved := 0
	for _, e := range ents {
		ok, err := s.recoverOne(dir, e, now, staleAfter, maxAge)
		if err != nil {
			return moved, err
		}
		if ok {
			moved++
		}
	}
	return moved, nil
}

func (s *Store) recoverOne(dir string, e fs.DirEntry, now time.Time, staleAfter, maxAge time.Duration) (bool, error) {
	key, pid, ok := parseInflight(e.Name())
	if !ok {
		return false, nil
	}
	fi, err := e.Info()
	if err != nil || now.Sub(fi.ModTime()) <= staleAfter {
		return false, nil // vanished, or too young to judge
	}
	if pidAlive(pid) && (maxAge <= 0 || now.Sub(fi.ModTime()) <= maxAge) {
		return false, nil // its owner may still be sending it
	}
	// Under the incident lock, so a repeat that checks HasPending under it never misses the entry
	// while it moves from inflight/ to queue/. Recover is called at the start of a command, never
	// holding the lock.
	var requeued bool
	err = s.withLock(func() error {
		var err error
		requeued, err = s.requeue(filepath.Join(dir, e.Name()), key)
		return err
	})
	return requeued, err
}

// requeue puts a dead owner's inflight entry back in the queue with Attempts+1, so a payload that
// kills its sender every time eventually reaches dead/. It never replaces an entry already in
// queue/ (that one is newer): the copy is written to a temp file and linked in, and on EEXIST the
// inflight copy is simply dropped. It reports whether the entry was requeued.
func (s *Store) requeue(path, key string) (bool, error) {
	w, bad, vanished := loadEntry(path, key)
	if vanished {
		return false, nil
	}
	if bad != "" {
		return false, s.quarantine(path, key)
	}
	w.Attempts++
	linked, err := publishNoClobber(s.path("queue", key+".json"), w)
	if err != nil {
		return false, err
	}
	return linked, removeSynced(path)
}

// sweepTemps deletes ".tmp-*" files that WriteFileAtomic left behind (a crash between create and
// rename) once they are older than staleAfter; younger ones may belong to a live writer.
func (s *Store) sweepTemps(now time.Time, staleAfter time.Duration) error {
	for _, dir := range []string{s.dir, s.path("queue"), s.path("inflight"), s.path("dead")} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return ioProblem("cannot list", dir, err)
		}
		for _, e := range ents {
			fi, err := e.Info()
			if e.IsDir() || !strings.HasPrefix(e.Name(), ".tmp-") || err != nil || now.Sub(fi.ModTime()) <= staleAfter {
				continue
			}
			if err := removeFile(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseInflight splits "<key>.<pid>.json".
func parseInflight(name string) (key string, pid int, ok bool) {
	base, found := strings.CutSuffix(name, ".json")
	if !found {
		return "", 0, false
	}
	i := strings.LastIndexByte(base, '.')
	if i <= 0 {
		return "", 0, false
	}
	pid, err := strconv.Atoi(base[i+1:])
	if err != nil || pid <= 0 {
		return "", 0, false
	}
	return base[:i], pid, true
}

// Counts returns how many entries are queued, in flight and dead.
func (s *Store) Counts() (queued, inflight, dead int, err error) {
	var n [3]int
	for i, sub := range []string{"queue", "inflight", "dead"} {
		ents, err := os.ReadDir(s.path(sub))
		if err != nil {
			return 0, 0, 0, ioProblem("cannot list", s.path(sub), err)
		}
		for _, e := range ents {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".json") {
				n[i]++
			}
		}
	}
	return n[0], n[1], n[2], nil
}

// DeadEntries lists the dead-letter entries, oldest first.
func (s *Store) DeadEntries() ([]DeadEntry, error) {
	ws, err := s.listWires("dead")
	if err != nil {
		return nil, err
	}
	out := make([]DeadEntry, 0, len(ws))
	for _, w := range ws {
		out = append(out, DeadEntry{Pending: w.pending(), Reason: w.Reason, At: w.DeadAt})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}
