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

// SourceKind says what a shim `page --resolve` has to close: see State.ResolveSource.
type SourceKind int

// The sources of a shim resolve, in the order ResolveSource looks for them.
const (
	SourceOpen     SourceKind = iota // an open incident under the project: resolve it
	SourceOther                      // none there, one open under exactly one other project: resolve that one
	SourceSeveral                    // none there, open under two or more other projects: ambiguous
	SourceRecorded                   // none open, but a record lists where the key was delivered
	SourceNone                       // never seen, or nothing recorded: resolve everywhere that can
)

// ResolveSource is what ResolveSource found: the projects to name (SourceOther: the one to close;
// SourceSeveral: all of them, sorted), or where the record delivered (SourceRecorded).
type ResolveSource struct {
	Kind     SourceKind
	Projects []string
	Reached  []Reach
}

// ResolveSource looks up what a shim resolve of key, for project, has to close. An open incident is
// one not silent for longer than incident_ttl at at. Only when across is set (the caller has no
// project of its own) are other projects' incidents looked for by the bare key, for a shim whose
// trigger and resolve ran under different YO_HUMAN_PROJECT values; a caller with a project never
// sees another project's incidents. A
// record of the project's own that is closed, or expired and still marked open, still lists the
// destinations that delivered (the incident record keeps them until it is pruned), and those are
// returned in the order they were recorded.
func (s *State) ResolveSource(cfg *config.Config, project, key string, at time.Time, across bool) (ResolveSource, error) {
	incs, err := s.incidents()
	if err != nil {
		return ResolveSource{}, err
	}
	live := func(inc store.Incident) bool { return inc.Open && at.Sub(inc.LastSent) <= cfg.IncidentTTL }
	own, held := incs[IncidentKey(project, key)]
	if held && live(own) {
		return ResolveSource{Kind: SourceOpen}, nil
	}
	var others []string
	if across {
		others = openElsewhere(incs, project, key, live)
	}
	switch {
	case len(others) == 1:
		return ResolveSource{Kind: SourceOther, Projects: others}, nil
	case len(others) > 1:
		return ResolveSource{Kind: SourceSeveral, Projects: others}, nil
	}
	return recordedSource(own), nil
}

// openElsewhere lists, sorted, the other projects that hold a live incident for the bare key.
func openElsewhere(incs map[string]store.Incident, project, key string, live func(store.Incident) bool) []string {
	var out []string
	for id, inc := range incs {
		if inc.Key == key && inc.Project != project && id == IncidentKey(inc.Project, inc.Key) && live(inc) {
			out = append(out, inc.Project)
		}
	}
	sort.Strings(out)
	return out
}

// recordedSource is the source for a project's own record that is not open: SourceRecorded when it
// lists a destination that delivered, otherwise SourceNone.
func recordedSource(rec store.Incident) ResolveSource {
	var reached []Reach
	for _, r := range rec.Reached {
		if len(r.Delivered) > 0 {
			reached = append(reached, Reach{Instance: r.Instance, Delivered: true})
		}
	}
	if len(reached) == 0 {
		return ResolveSource{Kind: SourceNone}
	}
	return ResolveSource{Kind: SourceRecorded, Reached: reached}
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
