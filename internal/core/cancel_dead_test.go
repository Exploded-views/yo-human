package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// deadKinds lists the kind and reason of each dead-letter entry.
func deadKinds(t *testing.T, e *env) []string {
	t.Helper()
	ds, err := e.st.DeadEntries()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range ds {
		var pl struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(d.Data, &pl)
		out = append(out, pl.Kind+": "+d.Reason)
	}
	return out
}

func TestResolveDeadLettersACancelTheProviderRefused(t *testing.T) {
	e := scheduledEnv(t)
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Permanent}})
	r, err := p.Resolve(context.Background(), "disk", "")
	if !cancelFailure(err, "phone", errors.New("provider refused")) || r.Status != core.StatusResolved || !strings.Contains(joinErrs(r), "phone:dead:provider refused") {
		t.Fatalf("resolve = %s, %v; want resolved, the failed cancel reported as dead", joinErrs(r), err)
	}
	if !fixSays(err, "dead letter (see `yo-human status`)", "yo-human flush") {
		t.Errorf("problem %v; want its fix to point at the dead letter", err)
	}
	if got := deadKinds(t, e); len(got) != 1 || got[0] != "cancel: provider refused" {
		t.Errorf("dead letter %q; want the refused cancel kept there, so status shows the human may still be paged", got)
	}
	if q, _, _ := e.counts(); q != 0 {
		t.Errorf("queued %d; a refused cancel is not retried", q)
	}
}

func TestResolveRetriesACancelThatFailedForNow(t *testing.T) {
	e := scheduledEnv(t)
	p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Transient}})
	r, err := p.Resolve(context.Background(), "disk", "")
	if !cancelFailure(err, "phone", errors.New("provider refused")) || !strings.Contains(joinErrs(r), "phone:queued:provider refused") {
		t.Fatalf("resolve = %s, %v; want the failed cancel reported and queued for a retry", joinErrs(r), err)
	}
	if !fixSays(err, "(`yo-human flush`), which retries it", "dead letter") || kinds(err)[0] != problem.Queued {
		t.Errorf("problem %v; want its fix to say the cancel is retried", err)
	}
	if q, _, d := e.counts(); q != 1 || d != 0 {
		t.Fatalf("queued %d, dead %d; want the cancel kept for a retry", q, d)
	}
	e.clk.Advance(2 * time.Minute)
	if _, err := e.with(e.cfg, map[string]channel.Channel{"phone": e.fakes["phone"]}).Flush(context.Background()); err != nil || len(e.fakes["phone"].Cancelled) != 1 {
		t.Errorf("retry: %v, cancelled %d; want the scheduled send cancelled once", err, len(e.fakes["phone"].Cancelled))
	}
}

// lateOn sends "page" to n through p and resolves the key while that send is in flight; then the
// send lands with rcpt, after onLand runs.
func lateOn(t *testing.T, e *env, p *core.Pipeline, rcpt channel.Receipt, onLand func()) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fakes["n"].SendFunc = func(_ context.Context, m channel.Message, sp channel.SendSpec) (channel.Receipt, error) {
		if m.Resolved {
			return channel.Receipt{Instance: sp.Instance}, nil
		}
		once.Do(func() { close(started) })
		<-release
		onLand()
		return rcpt, nil
	}
	done := make(chan error)
	go func() {
		_, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
		done <- err
	}()
	<-started
	e.resolve("k", "")
	close(release)
	if err := <-done; err != nil && rcpt.ProviderID == "" {
		t.Fatal(err)
	}
}

func nAt(server string) *config.Config {
	cfg := incCfg(0, "n")
	cfg.Channels = map[string]config.ChannelInstance{"n": phoneAt(server)}
	return cfg
}

func TestALateScheduledSendWhoseCancelIsRefusedIsDeadLettered(t *testing.T) {
	caps := map[string]channel.Capabilities{"n": schedules}
	e := newEnv(t, nAt("https://one.example"), caps)
	var n atomic.Int64
	p := core.New(core.Deps{Config: e.cfg, Store: e.st, Channels: map[string]channel.Channel{"n": failingCancel{e.fakes["n"], channel.Permanent}},
		Caps: caps, Now: e.clk.Now, NewID: func() string { return fmt.Sprintf("L%03d", n.Add(1)) }})
	lateOn(t, e, p, channel.Receipt{Instance: "n", ProviderID: "SCHED-FAKE-1", Scheduled: true}, func() {})
	if got := deadKinds(t, e); len(got) != 1 || got[0] != "cancel: provider refused" {
		t.Errorf("dead letter %q; a scheduled page that landed after the resolve and could not be cancelled must be visible", got)
	}
}

func TestALateResolveNoteIsPinnedToTheDestinationTheSendWentThrough(t *testing.T) {
	e := newEnv(t, nAt("https://one.example"), map[string]channel.Capabilities{"n": human})
	lateOn(t, e, e.p, channel.Receipt{Instance: "n"}, func() {
		e.cfg.Channels = map[string]config.ChannelInstance{"n": phoneAt("https://two.example")} // the server changes
	})
	if got := notes(e); got != 0 {
		t.Fatalf("notes sent %d; the late page went through one.example, its note must not go to two.example", got)
	}
	e.clk.Advance(time.Minute) // past the window in which a first attempt is left to its own caller
	if _, err := e.p.Flush(context.Background()); err != nil || notes(e) != 0 {
		t.Fatalf("flush under two.example: %v, notes %d; want the note kept from two.example", err, notes(e))
	}
	q, err := e.st.Due(e.clk.Now())
	if err != nil || len(q) != 1 || !strings.Contains(q[0].Reason, "points somewhere else") {
		t.Fatalf("queue %+v, %v; want the note parked, with the reason", q, err)
	}
	one := core.New(core.Deps{Config: nAt("https://one.example"), Store: e.st, Channels: map[string]channel.Channel{"n": e.fakes["n"]},
		Caps: map[string]channel.Capabilities{"n": human}, Now: e.clk.Now, NewID: newIDs()})
	if _, err := one.Flush(context.Background()); err != nil || notes(e) != 1 {
		t.Errorf("flush under one.example: %v, notes %d; want the note sent where the page went", err, notes(e))
	}
}

// cancelFailure says err holds the problem for a cancel on instance that failed with cause: exit 69
// when it is dead, 75 when it is queued for another try, with the provider's error and a fix saying
// the human may still be paged.
func cancelFailure(err error, instance string, cause error) bool {
	for _, e := range flat(err) {
		pr, ok := problem.As(e)
		if ok && (pr.Kind == problem.Unavailable || pr.Kind == problem.Queued) && strings.Contains(pr.Fix, "the human may still be paged") &&
			pr.What == "the scheduled send on "+instance+" could not be cancelled: "+cause.Error() {
			return true
		}
	}
	return false
}

// flat lists the errors inside err, looking inside errors.Join.
func flat(err error) []error {
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		return []error{err}
	}
	var out []error
	for _, e := range joined.Unwrap() {
		out = append(out, flat(e)...)
	}
	return out
}

// fixSays says the cancel problem in err tells the human where the cancel is: want, and not other.
func fixSays(err error, want, other string) bool {
	pr, ok := problem.As(err)
	return ok && strings.Contains(pr.Fix, want) && !strings.Contains(pr.Fix, other)
}

func TestAFailedCancelThatCannotBeKeptIsStillReported(t *testing.T) {
	for _, tc := range []struct{ dir, want string }{{"queue", "phone:dead:provider refused"}, {"inflight", "phone:queued:provider refused"}} {
		e := scheduledEnv(t)
		lock(t, filepath.Join(e.dir, tc.dir))
		p := e.with(e.cfg, map[string]channel.Channel{"phone": failingCancel{e.fakes["phone"], channel.Permanent}})
		r, err := p.Resolve(context.Background(), "disk", "")
		if !cancelFailure(err, "phone", errors.New("provider refused")) || !strings.Contains(joinErrs(r), tc.want) {
			t.Errorf("%s locked: resolve = %s, %v; want %q and the human told a page may still fire", tc.dir, joinErrs(r), err, tc.want)
		}
		if _, ok := problem.As(err); !ok || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s locked: %v; want the store failure reported too", tc.dir, err)
		}
	}
}

func TestALateScheduledSendWhoseCancelFailedForNowIsCancelledByAFlush(t *testing.T) {
	caps := map[string]channel.Capabilities{"n": schedules}
	e := newEnv(t, nAt("https://one.example"), caps)
	var n atomic.Int64
	p := core.New(core.Deps{Config: e.cfg, Store: e.st, Channels: map[string]channel.Channel{"n": failingCancel{e.fakes["n"], channel.Transient}},
		Caps: caps, Now: e.clk.Now, NewID: func() string { return fmt.Sprintf("L%03d", n.Add(1)) }})
	lateOn(t, e, p, channel.Receipt{Instance: "n", ProviderID: "SCHED-FAKE-1", Scheduled: true}, func() {})
	e.clk.Advance(2 * time.Minute)
	if _, err := e.p.Flush(context.Background()); err != nil || cancelledIDs(e)["SCHED-FAKE-1"] != 1 {
		t.Errorf("flush: %v, cancelled %v; want the late page's cancel retried through the destination it went to", err, cancelledIDs(e))
	}
}
