package core

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/store"
)

// State is the local state a command works on: the queue, the incidents, the trust records and the
// audit log, in one state directory. It is what the cli holds instead of the store (constitution IV:
// cli → core → store); every write still goes through internal/store.
type State struct{ st *store.Store }

// OpenState opens the state directory, creating it (0700) if it is missing; one that others can
// read is refused.
func OpenState(dir string) (*State, error) {
	st, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	return &State{st: st}, nil
}

// Within is the state with every wait for the incident lock bounded by ctx: once ctx is done, the
// wait gives up with a problem that IsBusy reports.
func (s *State) Within(ctx context.Context) *State { return &State{st: s.st.Within(ctx)} }

// IsBusy reports a wait for the incident lock that ran out: another yo-human process held it past
// the call's deadline, and nothing was changed.
func IsBusy(err error) bool { return store.IsBusy(err) }

// Trusted reports whether the project file at path was trusted with exactly this content hash. It
// makes State a config.Truster.
func (s *State) Trusted(path, sha string) (bool, error) { return s.st.Trusted(path, sha) }

// Trust records the project file at path, with this content hash, as trusted.
func (s *State) Trust(path, sha string) error { return s.st.Trust(path, sha) }

// Audit appends one line to log.jsonl. It must never hold a secret value.
func (s *State) Audit(entry map[string]any) error { return s.st.AppendLog(entry) }

// Pipeline builds a pipeline over this state; d.Store is replaced.
func (s *State) Pipeline(d Deps) *Pipeline {
	d.Store = s.st
	return New(d)
}

// Recover returns to the queue the entries in flight whose sender is gone (see RecoverStale).
func (s *State) Recover(cfg *config.Config) error { return RecoverStale(cfg, s.st) }

// DueInstances names, sorted, the channel instances the entries due at at need built.
func (s *State) DueInstances(at time.Time) ([]string, error) {
	due, err := s.st.Due(at)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range due {
		for _, inst := range EntryInstances(p) {
			set[inst] = true
		}
	}
	out := make([]string, 0, len(set))
	for inst := range set {
		out = append(out, inst)
	}
	sort.Strings(out)
	return out, nil
}

// Reach is one channel instance an incident reached, and whether it delivered there (so a resolve
// note waits for it).
type Reach struct {
	Instance  string
	Delivered bool
}

// Reached lists what the open incident key reached; a key with none open reached nothing. An
// incident silent for longer than the TTL at at is over, but a page it still has a provider hold
// back must be stopped: only its instances with a scheduled send are listed, as not delivered.
func (s *State) Reached(cfg *config.Config, key string, at time.Time) ([]Reach, error) {
	incs, err := s.incidents()
	if err != nil {
		return nil, err
	}
	inc, ok := incs[key]
	if !ok || !inc.Open {
		return nil, nil
	}
	expired := at.Sub(inc.LastSent) > cfg.IncidentTTL
	var out []Reach
	for _, r := range inc.Reached {
		switch {
		case !expired:
			out = append(out, Reach{Instance: r.Instance, Delivered: len(r.Delivered) > 0})
		case len(r.Scheduled) > 0:
			out = append(out, Reach{Instance: r.Instance})
		}
	}
	return out, nil
}

// CreateFile writes a new file holding data, mode 0600, and fails with an error wrapping
// fs.ErrExist if anything is already at path. A reader never sees a partial file.
func CreateFile(path string, data []byte) error { return store.WriteFileExclusive(path, data, 0o600) }

// errReadOnly ends a WithIncidents callback that only reads, so nothing is written back.
var errReadOnly = errors.New("read only")

// incidents copies the incidents out of the store without writing them back.
func (s *State) incidents() (map[string]store.Incident, error) {
	out := map[string]store.Incident{}
	err := s.st.WithIncidents(func(m map[string]*store.Incident) error {
		for k, v := range m {
			out[k] = *v
		}
		return errReadOnly
	})
	if errors.Is(err, errReadOnly) {
		err = nil
	}
	return out, err
}
