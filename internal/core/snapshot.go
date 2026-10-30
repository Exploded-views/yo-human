package core

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
)

// Snapshot is what status reports from the state: every queued entry, the dead letter, how many
// sends are in flight, and the incidents still open. Taking one builds no channel and writes nothing.
type Snapshot struct {
	Queue     []QueueItem
	Dead      []DeadItem
	Inflight  int
	Incidents []OpenIncident
}

// QueueItem is one queued entry. Kind is "send", "note" (a resolve note), "cancel" (the cancellation
// of a send a provider holds), "admit" (a message waiting for its incident check) or "unreadable".
type QueueItem struct {
	ID, Instance, Kind, Project string
	Attempts                    int
	NextAttempt, DeliverAt      time.Time
	Reason                      string // why the entry is parked, if it is
}

// DeadItem is one entry in the dead letter, with why it will not be retried.
type DeadItem struct {
	ID, Instance, Kind, Project, Reason string
	At                                  time.Time
}

// OpenIncident is an incident open at the snapshot's time.
type OpenIncident struct {
	Key, Project, Level string
	Generation          int
	Opened, LastSent    time.Time
	Latest              string
}

// Snapshot reads the state at at: the queue, the dead letter, the count in flight, then the open
// incidents (one silent for longer than the TTL starts afresh, so it is no longer open). It stops
// at the first failure and returns what it read so far; a busy incident lock (IsBusy) fails only
// the incidents, which are read last.
func (s *State) Snapshot(cfg *config.Config, at time.Time) (Snapshot, error) {
	var snap Snapshot
	steps := []func() error{
		func() error { return snap.addQueue(s, at) },
		func() error { return snap.addDead(s) },
		func() (err error) { _, snap.Inflight, _, err = s.st.Counts(); return },
		func() error { return snap.addIncidents(s, cfg.IncidentTTL, at) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return snap, err
		}
	}
	return snap, nil
}

func (snap *Snapshot) addQueue(s *State, at time.Time) error {
	all, err := s.st.Due(at.AddDate(100, 0, 0)) // every queued entry, whenever it is due
	for _, p := range all {
		id, inst := idAndInstance(p.Key)
		kind, project := entryOf(p.Data)
		snap.Queue = append(snap.Queue, QueueItem{ID: id, Instance: inst, Kind: kind, Project: project,
			Attempts: p.Attempts, NextAttempt: p.NextAttempt, DeliverAt: p.DeliverAt, Reason: p.Reason})
	}
	return err
}

func (snap *Snapshot) addDead(s *State) error {
	dead, err := s.st.DeadEntries()
	for _, d := range dead {
		id, inst := idAndInstance(d.Key)
		kind, project := entryOf(d.Data)
		snap.Dead = append(snap.Dead, DeadItem{ID: id, Instance: inst, Kind: kind, Project: project, Reason: d.Reason, At: d.At})
	}
	return err
}

func (snap *Snapshot) addIncidents(s *State, ttl time.Duration, at time.Time) error {
	incs, err := s.incidents()
	keys := make([]string, 0, len(incs))
	for k := range incs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if i := incs[k]; i.Open && at.Sub(i.LastSent) <= ttl {
			snap.Incidents = append(snap.Incidents, OpenIncident{Key: i.Key, Project: i.Project, Level: i.Level,
				Generation: i.Generation, Opened: i.Opened, LastSent: i.LastSent, Latest: i.Latest})
		}
	}
	return err
}

// entryOf says what a queue entry is (see QueueItem.Kind), so a parked cancellation cannot be
// mistaken for a pending page, and the project of its message.
func entryOf(data []byte) (kind, project string) {
	var p struct {
		Kind    string
		Message struct {
			Resolved bool
			Project  string
		}
	}
	switch err := json.Unmarshal(data, &p); {
	case err != nil:
		return "unreadable", ""
	case p.Kind == kindCancel || p.Kind == kindAdmit:
		return p.Kind, p.Message.Project
	case p.Message.Resolved:
		return "note", p.Message.Project
	}
	return "send", p.Message.Project
}

// idAndInstance splits a "<message-id>.<instance>" queue key.
func idAndInstance(key string) (id, instance string) {
	id, instance, _ = strings.Cut(key, ".")
	return id, instance
}
