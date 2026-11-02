package core_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// closer is a channel that can resolve an alert (a), beside one that cannot (n).
var closer = map[string]channel.Capabilities{
	"a": {ReachesHuman: true, Resolve: true},
	"n": {ReachesHuman: true},
}

// putAt stores incs under their store keys (core.IncidentKey of project and key).
func putAt(t *testing.T, e *env, incs ...store.Incident) {
	t.Helper()
	err := e.st.WithIncidents(func(m map[string]*store.Incident) error {
		for i := range incs {
			m[core.IncidentKey(incs[i].Project, incs[i].Key)] = &incs[i]
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveSource(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.cfg.IncidentTTL = time.Hour
	delivered := []store.Reached{{Instance: "a", Delivered: []string{"f"}}, {Instance: "b"}, {Instance: "c", Scheduled: sched}}
	putAt(t, e,
		store.Incident{Key: "own", Project: "me", Open: true, LastSent: t0, Reached: delivered},
		store.Incident{Key: "one", Project: "p1", Open: true, LastSent: t0},
		store.Incident{Key: "many", Project: "zed", Open: true, LastSent: t0},
		store.Incident{Key: "many", Project: "alpha", Open: true, LastSent: t0},
		store.Incident{Key: "many", Project: "mid", Open: true, LastSent: t0},
		store.Incident{Key: "stale", Project: "p1", Open: true, LastSent: t0.Add(-2 * time.Hour)},
		store.Incident{Key: "closed", Project: "p1", LastSent: t0},
		store.Incident{Key: "done", Project: "me", LastSent: t0, Updated: t0, Reached: delivered},
		store.Incident{Key: "old", Project: "me", LastSent: t0, Updated: t0.Add(-2 * time.Hour), Reached: delivered},
		store.Incident{Key: "undelivered", Project: "me", LastSent: t0, Updated: t0, Reached: []store.Reached{{Instance: "b"}}},
	)
	s := openState(t, e)
	for _, c := range []struct {
		key  string
		at   time.Time
		want string
	}{
		{"own", t0, "{0 [] [] false}"},                                          // open under the project: resolve it
		{"one", t0, "{1 [p1] [] false}"},                                        // open under exactly one other project
		{"many", t0, "{2 [alpha mid zed] [] false}"},                            // two or more: sorted
		{"own", t0.Add(2 * time.Hour), "{3 [] [{a true}] false}"},               // expired: what it delivered is still recorded
		{"done", t0, "{3 [] [{a true}] true}"},                                  // closed: the same
		{"undelivered", t0, "{4 [] [] false}"},                                  // nothing delivered to list
		{"stale", t0, "{4 [] [] false}"},                                        // another project's expired incident is not open
		{"closed", t0, "{4 [] [] false}"},                                       // nor is a closed one
		{"old", t0, "{4 [] [] false}"},                                          // closed longer than the TTL ago: as good as pruned
		{"never", t0, "{4 [] [] false}"},                                        // never seen
		{"own", t0.Add(time.Hour + time.Nanosecond), "{3 [] [{a true}] false}"}, // at the TTL it is still open; one nanosecond past, it is not
	} {
		got, err := s.ResolveSource(e.cfg, "me", c.key, c.at, true)
		if err != nil || fmt.Sprint(got) != c.want {
			t.Errorf("ResolveSource(%s, +%v) = %v, %v; want %s", c.key, c.at.Sub(t0), got, err, c.want)
		}
	}
	if got, _ := s.ResolveSource(e.cfg, "me", "own", t0.Add(time.Hour), true); got.Kind != core.SourceOpen {
		t.Errorf("at the TTL: %v; want it still open", got)
	}
}

func TestResolveSourceNeverLooksAcrossForAProjectCaller(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	putAt(t, e,
		store.Incident{Key: "k", Project: "p1", Open: true, LastSent: t0},
		store.Incident{Key: "k", Project: "p2", Open: true, LastSent: t0})
	for _, key := range []string{"k", "other"} {
		got, err := openState(t, e).ResolveSource(e.cfg, "me", key, t0, false)
		if err != nil || got.Kind != core.SourceNone || len(got.Projects) != 0 {
			t.Errorf("ResolveSource(%s, across=false) = %+v, %v; want none, naming no project", key, got, err)
		}
	}
}

func TestResolveThroughSendsTheKeyInTheSendProjectsNamespace(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), closer)
	e.cfg.IncidentTTL = time.Hour
	e.say("page", "k", "t") // an incident with a different record, generation 1, under no project
	e.resolve("k", "")
	if _, err := e.p.ResolveThrough(context.Background(), "me", "me", "fresh", "page", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if want := core.ProviderKey("me", "fresh"); !slices.Equal(e.fakes["a"].Resolved[len(e.fakes["a"].Resolved)-1:], []string{want}) {
		t.Errorf("a resolved %v; want %q", e.fakes["a"].Resolved, want)
	}
}

func TestResolveSourceStoreFailure(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if err := os.Mkdir(filepath.Join(e.dir, "incidents.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := openState(t, e).ResolveSource(e.cfg, "me", "k", t0, true); err == nil {
		t.Error("an unreadable incident store went unreported")
	}
}

func through(e *env, key string, instances ...string) (core.Result, error) {
	return e.p.ResolveThrough(context.Background(), "", "", key, "page", instances)
}

func TestResolveThroughSendsTheBareKeyToTheInstances(t *testing.T) {
	e := newEnv(t, incCfg(0, "a", "n"), closer)
	r, err := through(e, "old", "a")
	if err != nil || r.Status != core.StatusResolved {
		t.Fatalf("ResolveThrough = %q, %v; want resolved", r.Status, err)
	}
	if !slices.Equal(e.fakes["a"].Resolved, []string{"old"}) || len(e.fakes["n"].Sent) != 0 {
		t.Errorf("a resolved %v, n got %d sends; want the bare key on a and nothing on n", e.fakes["a"].Resolved, len(e.fakes["n"].Sent))
	}
	if e.hasIncident("old") && e.incident("old").Open {
		t.Error("the key's record is open after the resolve")
	}
}

func TestResolveThroughUsesTheRecordedInstances(t *testing.T) {
	e := newEnv(t, incCfg(0, "a", "n"), closer)
	e.say("page", "k", "disk full")
	e.resolve("k", "")
	e.clk.Advance(time.Hour)
	r, err := through(e, "k") // the instances passed are for a key with no record: the record wins
	if err != nil || r.Status != core.StatusResolved {
		t.Fatalf("ResolveThrough = %q, %v; want resolved", r.Status, err)
	}
	if len(e.fakes["a"].Resolved) != 2 || len(e.fakes["n"].Sent) != 2 {
		t.Errorf("a resolved %v, n got %d sends; want the resolver told again and no second passive note", e.fakes["a"].Resolved, len(e.fakes["n"].Sent))
	}
}

func TestResolveThroughCancelsWhatAnExpiredIncidentHolds(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), closer)
	e.cfg.IncidentTTL = time.Hour
	putAt(t, e, store.Incident{Key: "k", Open: true, LastSent: t0.Add(-2 * time.Hour), Reached: []store.Reached{
		{Instance: "a", Delivered: []string{"f"}, Scheduled: sched}}})
	// The recorded fingerprint "f" is no instance's, so both the cancel and the close wait in the queue.
	r, err := through(e, "k", "a")
	if err == nil || len(r.Channels) != 2 {
		t.Fatalf("ResolveThrough = %q, %v, %v; want the held send's cancel reported beside the close", r.Status, err, r.Channels)
	}
}

func TestResolveThroughQueuedAndFailedCloses(t *testing.T) {
	for _, c := range []struct {
		name string
		kind func(error) error
		want core.Status
		pk   problem.Kind
	}{
		{"timeout", channel.Transient, core.StatusQueued, problem.Queued},
		{"refusal", channel.Permanent, core.StatusFailed, problem.Unavailable},
	} {
		e := newEnv(t, incCfg(0, "a", "n"), closer)
		e.fakes["n"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
			return channel.Receipt{}, channel.Transient(errors.New("n is down")) // a passive channel that fails does not decide the status
		}
		e.fakes["a"].ResolveFunc = func(context.Context, string, channel.Message) error { return c.kind(errors.New("provider said no")) }
		r, err := through(e, "k", "a")
		if pr, ok := problem.As(err); !ok || pr.Kind != c.pk || r.Status != c.want {
			t.Errorf("%s: %q, %v; want %q with a %v problem", c.name, r.Status, err, c.want, c.pk)
		}
		if c.want == core.StatusQueued && r.Next == nil {
			t.Errorf("%s: no next step for a queued close", c.name)
		}
	}
}

func TestResolveThroughStoreFailures(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), closer)
	if err := os.Mkdir(filepath.Join(e.dir, "queue", "M001.a.json"), 0o700); err != nil { // the first id is M001
		t.Fatal(err)
	}
	if _, err := through(e, "k", "a"); err == nil {
		t.Error("a close that could not be queued went unreported")
	}
	if err := os.Remove(filepath.Join(e.dir, "incidents.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(e.dir, "incidents.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := through(e, "k", "a"); err == nil {
		t.Error("an unreadable incident store went unreported")
	}
	if err := os.RemoveAll(filepath.Join(e.dir, "inflight")); err != nil {
		t.Fatal(err)
	}
	if _, err := through(e, "k", "a"); err == nil {
		t.Error("a failing recovery went unreported")
	}
}

func TestResolveThroughWithNothingToSendFails(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), closer)
	r, err := through(e, "k")
	if pr, ok := problem.As(err); !ok || pr.Kind != problem.Unavailable || r.Status != core.StatusFailed {
		t.Errorf("ResolveThrough = %q, %v; want failed: a resolve that sent nothing is never a success", r.Status, err)
	}
}

// A passive channel that fails to hear of a re-resolve does not decide the status.
func TestResolveThroughIgnoresAPassiveChannelsFailure(t *testing.T) {
	e := newEnv(t, incCfg(0, "a", "n"), closer)
	e.cfg.IncidentTTL = time.Hour
	e.say("page", "k", "t")
	e.clk.Advance(2 * time.Hour) // expired, never closed: the full fan-out
	e.fakes["n"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{}, channel.Transient(errors.New("n is down"))
	}
	r, _ := through(e, "k")
	if r.Status != core.StatusResolved || len(e.fakes["a"].Resolved) != 1 {
		t.Errorf("status %q, a resolved %v; want resolved, the failing note aside", r.Status, e.fakes["a"].Resolved)
	}
}
