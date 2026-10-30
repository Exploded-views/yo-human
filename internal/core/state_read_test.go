package core_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// putIncidents writes incidents straight into the store.
func putIncidents(t *testing.T, st *store.Store, incs ...store.Incident) {
	t.Helper()
	err := st.WithIncidents(func(m map[string]*store.Incident) error {
		for i := range incs {
			m[incs[i].Key] = &incs[i]
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var sched = []store.ScheduledSend{{ID: "P1", Fingerprint: "f"}}

func TestStateReached(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.cfg.IncidentTTL = time.Hour
	putIncidents(t, e.st,
		store.Incident{Key: "open", Open: true, LastSent: t0, Reached: []store.Reached{
			{Instance: "a", Delivered: []string{"f"}}, {Instance: "b"}, {Instance: "c", Scheduled: sched}}},
		store.Incident{Key: "closed", LastSent: t0, Reached: []store.Reached{{Instance: "a", Delivered: []string{"f"}}}},
	)
	s := openState(t, e)
	for _, c := range []struct {
		key  string
		at   time.Time
		want string
	}{
		{"open", t0.Add(time.Hour), "[{a true} {b false} {c false}]"}, // at the TTL it is still open
		{"open", t0.Add(time.Hour + time.Nanosecond), "[{c false}]"},  // expired: only what a provider holds
		{"closed", t0, "[]"},
		{"unknown", t0, "[]"},
	} {
		got, err := s.Reached(e.cfg, c.key, c.at)
		if err != nil || fmt.Sprint(got) != c.want {
			t.Errorf("Reached(%s, %v) = %v, %v; want %s", c.key, c.at.Sub(t0), got, err, c.want)
		}
	}
}

// snapshotWorld queues one entry of each kind, claims one, buries one and opens incidents at, past
// and before the TTL.
func snapshotWorld(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, incCfg(0, "a"), one)
	e.cfg.IncidentTTL = time.Hour
	for _, p := range []store.Pending{
		{Key: "M1.a", Data: []byte(`{"message":{"project":"p1"}}`), Attempts: 2, NextAttempt: t0.Add(-time.Minute), Reason: "not built"},
		{Key: "M2.b", Data: []byte(`{"message":{"resolved":true,"project":"p2"}}`), DeliverAt: t0.Add(time.Hour)},
		{Key: "M3.c", Data: []byte(`{"kind":"cancel"}`)},
		{Key: "M4._admit", Data: []byte(`{"kind":"admit","message":{"project":"p1"}}`)},
		{Key: "M5.d", Data: []byte(`{"kind":"send","message":{"project":"p3"}}`)},
		{Key: "M6.e", Data: []byte(`[1]`)},
	} {
		if err := e.st.Enqueue(p); err != nil {
			t.Fatal(err)
		}
	}
	claim(t, e.st, "M5.d")
	dead(t, e.st, "M6.e", "gone")
	putIncidents(t, e.st,
		store.Incident{Key: "live", Project: "p1", Level: "page", Generation: 2, Open: true, Opened: t0.Add(-time.Hour), LastSent: t0.Add(-time.Hour), Latest: "disk"},
		store.Incident{Key: "stale", Open: true, LastSent: t0.Add(-time.Hour - time.Nanosecond)},
		store.Incident{Key: "shut", LastSent: t0},
	)
	return e
}

func TestStateSnapshot(t *testing.T) {
	e := snapshotWorld(t)
	snap, err := openState(t, e).Snapshot(e.cfg, t0)
	if err != nil {
		t.Fatal(err)
	}
	wantQueue := fmt.Sprint([]core.QueueItem{
		{ID: "M1", Instance: "a", Kind: "send", Project: "p1", Attempts: 2, NextAttempt: t0.Add(-time.Minute), Reason: "not built"},
		{ID: "M2", Instance: "b", Kind: "note", Project: "p2", DeliverAt: t0.Add(time.Hour)},
		{ID: "M3", Instance: "c", Kind: "cancel"},
		{ID: "M4", Instance: "_admit", Kind: "admit", Project: "p1"},
	})
	sort.Slice(snap.Queue, func(i, j int) bool { return snap.Queue[i].ID < snap.Queue[j].ID })
	if got := fmt.Sprint(snap.Queue); got != wantQueue {
		t.Errorf("queue =\n%s\nwant\n%s", got, wantQueue)
	}
	if len(snap.Dead) != 1 || snap.Dead[0].At.IsZero() {
		t.Fatalf("dead = %+v; want one, with its time", snap.Dead)
	}
	if d := snap.Dead[0]; fmt.Sprint(d.ID, d.Instance, d.Kind, d.Project, d.Reason) != fmt.Sprint("M6", "e", "unreadable", "", "gone") {
		t.Errorf("dead = %+v; want M6 on e, unreadable, of no project, with its reason", d)
	}
	if snap.Inflight != 1 {
		t.Errorf("inflight = %d; want 1", snap.Inflight)
	}
	want := core.OpenIncident{Key: "live", Project: "p1", Level: "page", Generation: 2, Opened: t0.Add(-time.Hour), LastSent: t0.Add(-time.Hour), Latest: "disk"}
	if len(snap.Incidents) != 1 || fmt.Sprint(snap.Incidents[0]) != fmt.Sprint(want) {
		t.Errorf("incidents = %+v; want only %+v (one at the TTL is open, one past it or closed is not)", snap.Incidents, want)
	}
}

func claim(t *testing.T, st *store.Store, key string) store.Claimed {
	t.Helper()
	c, ok, err := st.Claim(key)
	if err != nil || !ok {
		t.Fatalf("claim %s: %v %v", key, ok, err)
	}
	return c
}

func dead(t *testing.T, st *store.Store, key, reason string) {
	t.Helper()
	if err := st.Dead(claim(t, st, key), reason); err != nil {
		t.Fatal(err)
	}
}

// A snapshot stops at the first part it cannot read and keeps what it read before.
func TestStateSnapshotStopsAtTheFirstFailure(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if err := e.st.Enqueue(store.Pending{Key: "M1.a", Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	s := openState(t, e)
	if err := os.RemoveAll(filepath.Join(e.dir, "dead")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "dead"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(e.cfg, t0)
	if err == nil || len(snap.Queue) != 1 || snap.Incidents != nil {
		t.Errorf("snapshot over an unreadable dead letter = %+v, %v; want the queue and the error", snap, err)
	}
}
