package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/problem"
)

// Pending is one queued send. Key is "<message-id>.<instance>"; Data is opaque JSON from core.
type Pending struct {
	Key, IncidentKey       string
	Data                   []byte
	Attempts               int
	NextAttempt, DeliverAt time.Time
	Reason                 string // why a queued entry is parked, not sent; empty when nothing is wrong
}

// Claimed is a Pending this process owns through an inflight file named for its pid.
type Claimed struct {
	Pending
	path string
}

// DeadEntry is a send that will not be retried, with why.
type DeadEntry struct {
	Pending
	Reason string
	At     time.Time
}

// wire is the on-disk form of a Pending (and, under dead/, of a DeadEntry).
type wire struct {
	Key         string          `json:"key"`
	IncidentKey string          `json:"incident_key"`
	Data        json.RawMessage `json:"data"`
	Attempts    int             `json:"attempts"`
	NextAttempt time.Time       `json:"next_attempt"`
	DeliverAt   time.Time       `json:"deliver_at"`
	Reason      string          `json:"reason,omitempty"`
	DeadAt      time.Time       `json:"dead_at,omitzero"`
}

func (w wire) pending() Pending {
	p := Pending{Key: w.Key, IncidentKey: w.IncidentKey, Attempts: w.Attempts,
		NextAttempt: w.NextAttempt, DeliverAt: w.DeliverAt, Reason: w.Reason}
	if string(w.Data) != "null" {
		p.Data = []byte(w.Data)
	}
	return p
}

func wireOf(p Pending) wire {
	w := wire{Key: p.Key, IncidentKey: p.IncidentKey, Data: json.RawMessage(p.Data), Attempts: p.Attempts,
		NextAttempt: p.NextAttempt, DeliverAt: p.DeliverAt, Reason: p.Reason}
	if len(w.Data) == 0 {
		w.Data = json.RawMessage("null")
	}
	return w
}

func validKey(key string) error {
	if key == "" || strings.HasPrefix(key, ".") || strings.ContainsAny(key, "/\\\x00") {
		return problem.Storef("queue key %q cannot name a file", "a non-empty key with no path separators and no leading dot",
			"build keys as <message-id>.<instance>", key)
	}
	return nil
}

// Enqueue writes p to queue/<key>.json atomically, and refuses a key already queued: an entry is
// never replaced, so two sends that share a key cannot lose one of them.
func (s *Store) Enqueue(p Pending) error {
	if err := validKey(p.Key); err != nil {
		return err
	}
	path := s.path("queue", p.Key+".json")
	created, err := publishNoClobber(path, wireOf(p))
	if err == nil && !created {
		err = problem.Storef("queue entry %s already exists", "a key no queued entry has",
			"report this as a bug: queue keys are built from fresh message ids", p.Key)
	}
	return err
}

// Due returns the queued entries whose NextAttempt and DeliverAt are both at or before now, oldest
// (earliest NextAttempt) first, ties by key.
func (s *Store) Due(now time.Time) ([]Pending, error) {
	ws, err := s.listWires("queue")
	if err != nil {
		return nil, err
	}
	var out []Pending
	for _, w := range ws {
		if !w.NextAttempt.After(now) && !w.DeliverAt.After(now) {
			out = append(out, w.pending())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].NextAttempt.Equal(out[j].NextAttempt) {
			return out[i].NextAttempt.Before(out[j].NextAttempt)
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// Claim atomically renames queue/<key>.json to inflight/<key>.<pid>.json. It reports false when
// another process claimed (or cancelled) the entry first. No lock is held, and none is needed:
// rename succeeds for exactly one caller.
func (s *Store) Claim(key string) (Claimed, bool, error) {
	if err := validKey(key); err != nil {
		return Claimed{}, false, err
	}
	dst := s.path("inflight", key+"."+strconv.Itoa(os.Getpid())+".json")
	if err := os.Rename(s.path("queue", key+".json"), dst); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Claimed{}, false, nil
		}
		return Claimed{}, false, ioProblem("cannot claim", dst, err)
	}
	// Stamp the claim time so Recover measures how long it has been in flight, not in the queue.
	now := time.Now()
	if err := os.Chtimes(dst, now, now); err != nil {
		return Claimed{}, false, ioProblem("cannot stamp", dst, err)
	}
	w, bad, vanished := loadEntry(dst, key)
	if vanished {
		return Claimed{}, false, nil
	}
	if bad != "" { // nothing may be stranded in inflight: set it aside where status shows it
		return Claimed{}, false, s.quarantine(dst, key)
	}
	return Claimed{Pending: w.pending(), path: dst}, true, nil
}

// Complete removes a claimed entry after a successful send.
func (s *Store) Complete(c Claimed) error { return removeSynced(c.path) }

// Retry returns a claimed entry to the queue with Attempts+1, to be tried again at next. The queue
// file is written before the inflight one is removed, so a crash duplicates rather than loses it.
func (s *Store) Retry(c Claimed, next time.Time) error {
	p := c.Pending
	p.Attempts++
	p.NextAttempt, p.Reason = next, ""
	return s.requeueClaimed(c, p)
}

// Release returns a claimed entry to the queue as it was, due at now (the caller's clock, as with
// Retry), without spending an attempt. It is for a send the caller abandoned through no fault of
// the channel.
func (s *Store) Release(c Claimed, now time.Time) error {
	p := c.Pending
	p.NextAttempt, p.Reason = now, ""
	return s.requeueClaimed(c, p)
}

// Park returns a claimed entry to the queue as Release does, without spending an attempt, and keeps
// the reason it could not be sent, for status to show. The next Release or Retry clears it.
//
// It holds the incident lock while it moves the entry, as the pipeline does for Retry and Release
// (it must not be called holding it): a repeat checking HasPending under that lock then never
// finds the entry in neither directory. For the same reason Recover requeues under the lock.
func (s *Store) Park(c Claimed, now time.Time, reason string) error {
	p := c.Pending
	p.NextAttempt, p.Reason = now, reason
	return s.withLock(func() error { return s.requeueClaimed(c, p) })
}

// requeueClaimed writes p to the queue, then removes the inflight file. The claim took the key out
// of the queue; an entry queued under it since is newer, so, as Recover does, it is kept and this
// copy is dropped.
func (s *Store) requeueClaimed(c Claimed, p Pending) error {
	if err := validKey(p.Key); err != nil {
		return err
	}
	if _, err := publishNoClobber(s.path("queue", p.Key+".json"), wireOf(p)); err != nil {
		return err
	}
	return removeSynced(c.path)
}

// Dead moves a claimed entry to dead/ with the reason it will not be retried.
func (s *Store) Dead(c Claimed, reason string) error {
	if err := validKey(c.Key); err != nil {
		return err
	}
	w := wireOf(c.Pending)
	w.Reason, w.DeadAt = reason, time.Now().UTC()
	if err := s.publishDead(c.Key, w); err != nil {
		return err
	}
	return removeSynced(c.path)
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ioProblem("cannot remove", path, err)
	}
	return nil
}

// removeSynced removes path and fsyncs its directory so the removal survives a crash.
func removeSynced(path string) error {
	if err := removeFile(path); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return ioProblem("cannot sync", filepath.Dir(path), err)
	}
	return nil
}

// CancelIncident removes and returns every queued entry of that incident. An entry is returned only
// if this call's remove deleted it, so one already claimed for sending is not.
func (s *Store) CancelIncident(incidentKey string) ([]Pending, error) {
	return s.cancelWhere(func(w wire) bool { return w.IncidentKey == incidentKey })
}

// CancelHeld removes and returns the queued entries of that incident held for delivery after now:
// the copies a louder repeat or a reminder replaces. Like CancelIncident, it returns only what its
// own removes deleted.
func (s *Store) CancelHeld(incidentKey string, now time.Time) ([]Pending, error) {
	return s.cancelWhere(func(w wire) bool { return w.IncidentKey == incidentKey && w.DeliverAt.After(now) })
}

// cancelWhere removes and returns the queued entries that match.
func (s *Store) cancelWhere(match func(wire) bool) ([]Pending, error) {
	ws, err := s.listWires("queue")
	if err != nil {
		return nil, err
	}
	var out []Pending
	for _, w := range ws {
		if !match(w) {
			continue
		}
		path := s.path("queue", w.Key+".json")
		if err := os.Remove(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return out, ioProblem("cannot remove", path, err)
		}
		out = append(out, w.pending())
	}
	if len(out) > 0 {
		if err := syncDir(s.path("queue")); err != nil {
			return out, ioProblem("cannot sync", s.path("queue"), err)
		}
	}
	return out, nil
}

// publishDead writes a dead record without replacing an existing one for the same key.
func (s *Store) publishDead(key string, w wire) error {
	for _, n := range deadNames(key) {
		ok, err := publishNoClobber(s.path("dead", n), w)
		if err != nil || ok {
			return err
		}
	}
	return ioProblem("cannot write dead record for", key, fs.ErrExist)
}

// hasPendingGap is a test seam: it runs between HasPending's scan of queue/ and its scan of inflight/.
var hasPendingGap func()

// admitSuffix ends the key of a message waiting for its incident check (core's "_admit" entry). It
// is not a send: it never makes an incident's message pending.
const admitSuffix = "._admit"

// isAdmit says a queue or in-flight key (an in-flight one ends in its owner's pid) is a message
// waiting for its incident check.
func isAdmit(key string) bool {
	return strings.HasSuffix(key, admitSuffix) || strings.Contains(key, admitSuffix+".")
}

// WaitingOf lists the queue keys of that incident's waiting messages, queued or in flight (an
// in-flight key loses its owner's pid). Like an IncidentsInUse pass it lists inflight/, queue/, then
// inflight/ again, so one that moves once meanwhile is listed. Call it holding the incident lock.
func (s *Store) WaitingOf(incidentKey string) ([]string, error) {
	seen := map[string]bool{}
	for _, sub := range []string{"inflight", "queue", "inflight"} {
		ws, err := s.listWires(sub)
		if err != nil {
			return nil, err
		}
		for _, w := range ws {
			if w.IncidentKey == incidentKey && isAdmit(w.Key) {
				seen[w.Key[:strings.Index(w.Key, admitSuffix)+len(admitSuffix)]] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// inUseGap is a test seam: it runs after each directory IncidentsInUse lists.
var inUseGap func()

// maxInUsePasses bounds the passes IncidentsInUse makes looking for two that agree.
const maxInUsePasses = 5

// errUnstable is IncidentsInUse finding no two passes that agree.
var errUnstable = errors.New("the queue kept changing while it was listed")

// IncidentsInUse lists the incidents that a queued or in-flight entry refers to: a waiting message,
// a resolve note or any other entry with an incident key (a queued cancellation has none, and never
// reads an incident). Dead entries are not in use. Entries move between queue/ and inflight/
// without the incident lock (a flush's Claim, and the Release of a message whose incident check
// found the lock busy), so it lists until two passes agree, and fails if none do: the caller then
// prunes nothing.
func (s *Store) IncidentsInUse() (map[string]bool, error) {
	prev, err := s.inUsePass()
	if err != nil {
		return nil, err
	}
	for range maxInUsePasses - 1 {
		cur, err := s.inUsePass()
		if err != nil {
			return nil, err
		}
		if maps.Equal(prev, cur) {
			return cur, nil
		}
		prev = cur
	}
	return nil, errUnstable
}

// inUsePass lists inflight/, queue/, then inflight/ again, so an entry that moves once during the
// pass, either way, is seen.
func (s *Store) inUsePass() (map[string]bool, error) {
	out := map[string]bool{}
	for _, sub := range []string{"inflight", "queue", "inflight"} {
		ws, err := s.listWires(sub)
		if err != nil {
			return nil, err
		}
		if inUseGap != nil {
			inUseGap()
		}
		for _, w := range ws {
			if w.IncidentKey != "" {
				out[w.IncidentKey] = true
			}
		}
	}
	return out, nil
}

// HasPending reports whether an entry of that incident is still queued or in flight: a send that
// is waiting to be tried, or being tried right now. Dead entries are not pending, and neither is a
// message still waiting for its incident check. Call it holding
// the incident lock (WithIncidents): the scans are not atomic, and the lock is what keeps an entry
// from moving from inflight/ back to queue/ between them.
func (s *Store) HasPending(incidentKey string) (bool, error) {
	for i, sub := range []string{"queue", "inflight"} {
		if i == 1 && hasPendingGap != nil {
			hasPendingGap()
		}
		ws, err := s.listWires(sub)
		if err != nil {
			return false, err
		}
		for _, w := range ws {
			if w.IncidentKey == incidentKey && !isAdmit(w.Key) {
				return true, nil
			}
		}
	}
	return false, nil
}
