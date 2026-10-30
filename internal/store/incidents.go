package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

// Incident is a deduplicated alert: its level, the instances reached and when it was last sent.
type Incident struct {
	Key         string    `json:"key"`                // the caller's dedup key, as the providers see it
	Project     string    `json:"project,omitempty"`  // the project whose incident it is (see core.IncidentKey)
	Generation  int       `json:"generation"`         // bumped every time the key opens or reopens
	Previous    string    `json:"previous,omitempty"` // how the generation before ended: "resolved" or "expired"
	Level       string    `json:"level"`
	Rank        int       `json:"rank"`
	Open        bool      `json:"open"`
	Reached     []Reached `json:"reached,omitempty"`
	ContentHash string    `json:"content_hash,omitempty"`
	Latest      string    `json:"latest,omitempty"`
	Opened      time.Time `json:"opened"`
	LastSent    time.Time `json:"last_sent"`
	Updated     time.Time `json:"updated"`
	// Withdrawn lists the waiting messages (their queue keys, "<id>._admit") a resolve found queued
	// or in flight: each is dropped when its incident check runs, whatever the clocks say. Kept
	// across a reopen, and pruned with the record.
	Withdrawn []string `json:"withdrawn,omitempty"`
}

// Reached records what an instance received for the incident: the destinations (fingerprints) a
// message was delivered through, and the sends it holds for later delivery, each with the
// destination it went through. An instance can have been repointed between two deliveries, so
// neither is a single value.
type Reached struct {
	Instance  string          `json:"instance"`
	Delivered []string        `json:"delivered,omitempty"` // the distinct fingerprints that delivered
	Scheduled []ScheduledSend `json:"scheduled,omitempty"`
}

// ScheduledSend is a send a provider holds for later delivery: its id there, and where it went.
type ScheduledSend struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

// withLock runs fn holding an exclusive flock on state.lock, which guards incidents.json and
// trust.json. The lock is held only for the read-modify-write, never during network I/O.
func (s *Store) withLock(fn func() error) error {
	path := s.path("state.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path built by the store
	if err != nil {
		return ioProblem("cannot open", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := s.lockFile(f, path); err != nil {
		return err
	}
	return fn()
}

// maxLockPoll is the longest pause between two tries for a lock that is held: short, since a
// holder keeps the lock only for a few file writes.
const maxLockPoll = 10 * time.Millisecond

// lockFile takes the exclusive lock on f: blocking, or, for a store bound by Within, trying again
// until the context ends, then giving up with errBusy.
func (s *Store) lockFile(f *os.File, path string) error {
	if s.ctx == nil {
		if err := lockExclusive(f); err != nil {
			return ioProblem("cannot lock", path, err)
		}
		return nil
	}
	pause := time.Millisecond
	for {
		busy, err := tryLockExclusive(f)
		switch {
		case err != nil:
			return ioProblem("cannot lock", path, err)
		case !busy:
			return nil
		}
		select {
		case <-s.ctx.Done():
			return errBusy
		case <-time.After(pause):
		}
		pause = min(2*pause, maxLockPoll)
	}
}

// readJSON decodes path into v; a missing file leaves v untouched.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path) //nolint:gosec // path built by the store
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ioProblem("cannot read", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return ioProblem("cannot parse", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ioProblem("cannot encode", path, err)
	}
	return WriteFileAtomic(path, b, 0o600)
}

// State is incidents.json: the incidents by key, and the generation the next one to open takes.
// Generations are never reused, even for a key whose closed record was pruned, so an entry written
// for an earlier incident never matches a later one.
type State struct {
	NextGeneration int                  `json:"next_generation"`
	Incidents      map[string]*Incident `json:"incidents"`
}

// NewGeneration hands out a generation no incident has had.
func (st *State) NewGeneration() int {
	g := st.NextGeneration
	st.NextGeneration++
	return g
}

// WithIncidents runs fn on the incidents under an exclusive lock for the whole read-modify-write.
// If fn returns an error nothing is written and the error is returned as is.
func (s *Store) WithIncidents(fn func(map[string]*Incident) error) error {
	return s.WithState(func(st *State) error { return fn(st.Incidents) })
}

// WithState is WithIncidents with the generation counter too.
func (s *Store) WithState(fn func(*State) error) error {
	path := s.path("incidents.json")
	return s.withLock(func() error {
		st, err := readState(path)
		if err != nil {
			return err
		}
		if err := fn(st); err != nil {
			return err
		}
		return writeJSON(path, st)
	})
}

// readState reads incidents.json. A file written before the counter (a bare map of incidents) is
// read too. The counter is never below 1, nor at or below a generation an incident holds.
func readState(path string) (*State, error) {
	raw := map[string]json.RawMessage{}
	if err := readJSON(path, &raw); err != nil {
		return nil, err
	}
	st := &State{Incidents: map[string]*Incident{}}
	if _, ok := raw["next_generation"]; ok {
		if err := decodeState(raw, st); err != nil {
			return nil, ioProblem("cannot parse", path, err)
		}
	} else {
		for k, v := range raw {
			inc := &Incident{}
			if err := json.Unmarshal(v, inc); err != nil {
				return nil, ioProblem("cannot parse", path, err)
			}
			st.Incidents[k] = inc
		}
	}
	st.NextGeneration = max(st.NextGeneration, 1)
	for _, inc := range st.Incidents {
		st.NextGeneration = max(st.NextGeneration, inc.Generation+1)
	}
	return st, nil
}

func decodeState(raw map[string]json.RawMessage, st *State) error {
	if err := json.Unmarshal(raw["next_generation"], &st.NextGeneration); err != nil {
		return err
	}
	if v, ok := raw["incidents"]; ok {
		if err := json.Unmarshal(v, &st.Incidents); err != nil {
			return err
		}
	}
	if st.Incidents == nil {
		st.Incidents = map[string]*Incident{}
	}
	for k, inc := range st.Incidents {
		if inc == nil {
			delete(st.Incidents, k)
		}
	}
	return nil
}

// AppendLog appends entry as one JSON line to log.jsonl (mode 0600). Callers must not pass secrets.
// The line goes out in a single O_APPEND write, so concurrent processes do not interleave.
func (s *Store) AppendLog(entry any) error {
	path := s.path("log.jsonl")
	b, err := json.Marshal(entry)
	if err != nil {
		return ioProblem("cannot encode log entry for", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // path built by the store
	if err != nil {
		return ioProblem("cannot open", path, err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return ioProblem("cannot append to", path, err)
	}
	if err := f.Close(); err != nil {
		return ioProblem("cannot close", path, err)
	}
	return nil
}
