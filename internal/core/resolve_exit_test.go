package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// kinds lists the kind of every problem in err, in order.
func kinds(err error) []problem.Kind {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		var out []problem.Kind
		for _, e := range joined.Unwrap() {
			out = append(out, kinds(e)...)
		}
		return out
	}
	if p, ok := problem.As(err); ok {
		return []problem.Kind{p.Kind}
	}
	return nil
}

// A cancel queued for another try leaves the human able to be paged until a flush runs it: the
// resolve says so with a Queued problem (exit 75) and a next step that runs the flush.
func TestAResolveWhoseCancelIsQueuedSaysToFlush(t *testing.T) {
	e := scheduledEnv(t)
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Transient}})
	r, err := p.Resolve(context.Background(), "disk", "")
	if got := kinds(err); len(got) != 1 || got[0] != problem.Queued {
		t.Errorf("problems %v (%v); want one Queued problem", got, err)
	}
	if r.Status != core.StatusResolved || r.Next == nil || r.Next.Do != "yo-human flush" || r.Next.AttemptsLeft < 1 {
		t.Errorf("result %q next %+v; want resolved, with a next step that flushes", r.Status, r.Next)
	}
}

// A cancel the provider refused is dead: no flush helps. Unavailable (exit 69), no next.
func TestAResolveWhoseCancelIsDeadHasNoNext(t *testing.T) {
	e := scheduledEnv(t)
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Permanent}})
	r, err := p.Resolve(context.Background(), "disk", "")
	if got := kinds(err); len(got) != 1 || got[0] != problem.Unavailable || r.Next != nil {
		t.Errorf("problems %v, next %+v; want one Unavailable problem and no next", got, r.Next)
	}
}

// pdResolveEnv has an incident delivered to pd, a channel that closes alerts, and to n, which does
// not.
func pdResolveEnv(t *testing.T) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"pd": {ReachesHuman: true, NativeDedup: true, Resolve: true}, "n": human}
	e := newEnv(t, incCfg(0, "pd", "n"), caps)
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered {
		t.Fatalf("page: %q", r.Status)
	}
	return e
}

// PagerDuty keeps escalating an alert it was not told to close. A close queued for another try is
// a Queued problem (exit 75) with a next step, never a plain success.
func TestAResolveWhoseCloseIsQueuedSaysToFlush(t *testing.T) {
	e := pdResolveEnv(t)
	p := newWith(e, map[string]channel.Channel{"pd": failing{Fake: e.fakes["pd"], resolveErr: channel.Transient(errors.New("pagerduty answered HTTP 503"))}, "n": e.fakes["n"]})
	r, err := p.Resolve(context.Background(), "k", "")
	if got := kinds(err); len(got) != 1 || got[0] != problem.Queued || !strings.Contains(err.Error(), "pd") {
		t.Errorf("problems %v (%v); want one Queued problem naming pd", got, err)
	}
	if r.Status != core.StatusResolved || r.Next == nil || r.Next.Do != "yo-human flush" {
		t.Errorf("result %q next %+v; want resolved, with a next step that flushes", r.Status, r.Next)
	}
}

// A close the provider refused for good leaves the alert open: Unavailable (exit 69).
func TestAResolveWhoseCloseIsRefusedIsUnavailable(t *testing.T) {
	e := pdResolveEnv(t)
	p := newWith(e, map[string]channel.Channel{"pd": failing{Fake: e.fakes["pd"], resolveErr: channel.Permanent(errors.New("pagerduty answered HTTP 400"))}, "n": e.fakes["n"]})
	r, err := p.Resolve(context.Background(), "k", "")
	if got := kinds(err); len(got) != 1 || got[0] != problem.Unavailable || r.Next != nil {
		t.Errorf("problems %v (%v), next %+v; want one Unavailable problem and no next", got, err, r.Next)
	}
}

// A note to a channel that cannot close anything is passive: queued, it is retried quietly and the
// resolve is done.
func TestAResolveWhoseNoteIsQueuedIsDone(t *testing.T) {
	e := pdResolveEnv(t)
	e.fakes["n"].SendFunc = failWith(channel.Transient, "down")
	r, err := e.p.Resolve(context.Background(), "k", "")
	if err != nil || r.Next != nil || statuses(r)["n"] != core.ChannelQueued {
		t.Errorf("resolve = %+v, %v; want done, the note queued, no next", r, err)
	}
}

// When a cancel is dead and a close is queued, the dead cancel decides the exit: 69 comes first,
// whatever order they happened in.
func TestADeadCancelOutranksAQueuedClose(t *testing.T) {
	caps := map[string]channel.Capabilities{"pd": {ReachesHuman: true, NativeDedup: true, Resolve: true}, "sched": schedules}
	e := newEnv(t, incCfg(0, "pd", "sched"), caps)
	e.fakes["sched"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true}, nil
	}
	e.say("page", "k", "t")
	p := newWith(e, map[string]channel.Channel{
		"pd":    failing{Fake: e.fakes["pd"], resolveErr: channel.Transient(errors.New("down"))},
		"sched": failingCancel{e.fakes["sched"], channel.Permanent},
	})
	_, err := p.Resolve(context.Background(), "k", "")
	if got := kinds(err); len(got) != 2 || got[0] != problem.Unavailable || got[1] != problem.Queued {
		t.Fatalf("problems %v (%v); want the dead cancel first, then the queued close", got, err)
	}
}

// isQueued says err is exactly one Queued problem.
func isQueued(err error) bool {
	got := kinds(err)
	return len(got) == 1 && got[0] == problem.Queued
}

// closeAndCancelEnv has an incident delivered to pd, which closes alerts, and scheduled on sched.
func closeAndCancelEnv(t *testing.T) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"pd": {ReachesHuman: true, NativeDedup: true, Resolve: true}, "sched": schedules}
	e := newEnv(t, incCfg(0, "pd", "sched"), caps)
	e.fakes["sched"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true}, nil
	}
	e.say("page", "k", "t")
	return e
}

// The cancel is queued first and the close refused after it: the refusal still decides the exit.
func TestARefusedCloseOutranksAQueuedCancel(t *testing.T) {
	e := closeAndCancelEnv(t)
	p := newWith(e, map[string]channel.Channel{
		"pd":    failing{Fake: e.fakes["pd"], resolveErr: channel.Permanent(errors.New("refused"))},
		"sched": failingCancel{e.fakes["sched"], channel.Transient},
	})
	_, err := p.Resolve(context.Background(), "k", "")
	if got := kinds(err); len(got) != 2 || got[0] != problem.Unavailable || got[1] != problem.Queued {
		t.Fatalf("problems %v (%v); want the refused close first, then the queued cancel", got, err)
	}
}

// The next step is for what is due first: here the cancel of a send on a channel that could not be
// built, queued to run at once, before the close's retry a minute out.
func TestAResolvesNextIsForWhatIsDueFirst(t *testing.T) {
	e := closeAndCancelEnv(t)
	p := newWith(e, map[string]channel.Channel{"pd": failing{Fake: e.fakes["pd"], resolveErr: channel.Transient(errors.New("down"))}})
	r, _ := p.Resolve(context.Background(), "k", "")
	if r.Next == nil || r.Next.AfterSeconds != 0 {
		t.Errorf("next %+v; want the flush now, for the cancel due at once", r.Next)
	}
}

// An incident silent for longer than incident_ttl is over already: resolving it is a noop, with no
// close and no note (its held pages are stopped: TestResolvingAnExpiredIncidentStopsItsHeldPages).
func TestResolveOfAnExpiredIncidentIsANoop(t *testing.T) {
	e := pdResolveEnv(t)
	e.clk.Advance(25 * time.Hour)
	if r := e.resolve("k", ""); r.Status != core.StatusNoop || len(e.fakes["pd"].Resolved) != 0 || sentCount(e, "n") != 1 {
		t.Errorf("resolve = %+v; want noop, nothing closed or noted", r)
	}
	if inc := e.incident("k"); inc.Open {
		t.Error("the expired incident was left open")
	}
}
