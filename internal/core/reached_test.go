package core_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// reminded leaves two scheduled sends for one incident on one instance that was repointed between
// them: sched-1 went through one.example, and after the config moved to two.example a reminder
// scheduled sched-2 through it. The reminder replaces sched-1, which cannot be cancelled through
// two.example: its cancellation is queued, pinned to one.example, and the reminder warns of it.
func reminded(t *testing.T) (e *env, one, two *config.Config) {
	t.Helper()
	e = scheduledEnv(t) // sched-1 through one.example
	e.cfg.IncidentTTL = 24 * time.Hour
	e.cfg.Levels[0].Reminder = time.Minute
	one = e.cfg
	two = e.configWith(phoneAt("https://two.example"))
	two.Levels = e.cfg.Levels
	e.clk.Advance(2 * time.Minute)
	e.fakes["phone"].SendFunc = func(_ context.Context, _ channel.Message, sp channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: sp.Instance, ProviderID: "sched-2", Scheduled: !sp.DeliverAt.IsZero()}, nil
	}
	p := e.with(two, map[string]channel.Channel{"phone": e.fakes["phone"]})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "disk", DedupKey: "disk"})
	if err != nil || r.Status != core.StatusHeld {
		t.Fatalf("reminder under two.example = %+v, %v; want the provider holding a second send", r, err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Error(), "could not be cancelled yet") {
		t.Fatalf("reminder warnings %v; want sched-1's cancellation queued for one.example", r.Warnings)
	}
	return e, one, two
}

func ids(e *env) map[string]int {
	out := map[string]int{}
	for _, c := range e.fakes["phone"].Cancelled {
		out[c.ProviderID]++
	}
	return out
}

func built(e *env, cfg *config.Config) *core.Pipeline {
	return e.with(cfg, map[string]channel.Channel{"phone": e.fakes["phone"]})
}

// Each scheduled send is cancelled only through the destination it went through; the other waits,
// pinned to its own, for the config that points there.
func TestEachScheduledSendIsCancelledThroughItsOwnDestination(t *testing.T) {
	for name, first := range map[string]int{"under two first": 2, "under one first": 1} {
		e, one, two := reminded(t)
		cfgs := map[int]*config.Config{1: one, 2: two}
		direct := map[int]string{1: "sched-1", 2: "sched-2"}
		resolveCancelsOnly(t, name, e, cfgs[first], direct[first])
		if _, err := built(e, cfgs[3-first]).Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := ids(e); got["sched-1"] != 1 || got["sched-2"] != 1 {
			t.Errorf("%s: cancelled %v; want each send cancelled exactly once", name, got)
		}
	}
}

// resolveCancelsOnly resolves under cfg and checks that only the send that went through it was
// cancelled, the other's cancellation is queued, and a flush under cfg leaves it parked.
func resolveCancelsOnly(t *testing.T, name string, e *env, cfg *config.Config, want string) {
	t.Helper()
	if _, err := built(e, cfg).Resolve(context.Background(), "disk", ""); err != nil && !strings.Contains(err.Error(), "could not be cancelled yet") {
		t.Fatalf("%s: Resolve error = %v; want none, or a Config problem for the other send", name, err)
	}
	if got := ids(e); len(got) != 1 || got[want] != 1 {
		t.Fatalf("%s: cancelled %v; want only %s, through its own destination", name, got, want)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("%s: queued %d; want the other send's cancellation queued", name, q)
	}
	fr, _ := built(e, cfg).Flush(context.Background())
	if got := ids(e); len(got) != 1 || !strings.Contains(joinErrs(fr), "points somewhere else") {
		t.Fatalf("%s: flush under the same config cancelled %v (%s); want it parked", name, got, joinErrs(fr))
	}
}

// A superseded generation's held sends are cancelled the same way.
func TestAnExpiredGenerationsHeldSendsAreCancelledThroughTheirOwnDestination(t *testing.T) {
	e := scheduledEnv(t) // sched-1 through one.example
	e.cfg.IncidentTTL = time.Hour
	e.clk.Advance(2 * time.Hour)
	two := e.configWith(phoneAt("https://two.example"))
	two.Levels = e.cfg.Levels
	two.IncidentTTL = time.Hour
	r, err := built(e, two).Notify(context.Background(), channel.Message{Level: "page", Title: "disk again", DedupKey: "disk"})
	if err != nil || len(r.Warnings) != 1 {
		t.Fatalf("Notify = %v, warnings %v; want the old generation's cancellation as a warning", err, r.Warnings)
	}
	if p, ok := problem.As(r.Warnings[0]); !ok || !strings.Contains(p.What, "could not be cancelled yet") {
		t.Fatalf("warning = %v; want the old generation's cancellation reported", r.Warnings[0])
	}
	if got := ids(e); got["sched-1"] != 0 {
		t.Errorf("cancelled %v through two.example; sched-1 went through one.example", got)
	}
	if _, err := built(e, e.cfg).Flush(context.Background()); err != nil || ids(e)["sched-1"] != 1 {
		t.Errorf("flush under one.example: %v, cancelled %v; want sched-1 cancelled once", err, ids(e))
	}
}

// A message delivered through one destination and then another needs a note through each.
func TestAResolveNoteGoesThroughEveryDestinationThatDelivered(t *testing.T) {
	lv := page(retry5, "phone")
	lv.Reminder = time.Minute
	cfg := levelsCfg(time.Second, lv)
	cfg.IncidentTTL = 24 * time.Hour
	cfg.Channels = map[string]config.ChannelInstance{"phone": phoneAt("https://one.example")}
	e := newEnv(t, cfg, map[string]channel.Capabilities{"phone": human})
	if r := e.notifyKey("page", "disk"); r.Status != core.StatusDelivered {
		t.Fatalf("setup: %+v", r)
	}
	two := e.configWith(phoneAt("https://two.example"))
	two.Levels, two.IncidentTTL = cfg.Levels, cfg.IncidentTTL
	e.clk.Advance(2 * time.Minute)
	if r, err := built(e, two).Notify(context.Background(), channel.Message{Level: "page", Title: "disk", DedupKey: "disk"}); err != nil || r.Status != core.StatusDelivered {
		t.Fatalf("reminder under two.example = %+v, %v", r, err)
	}
	n := len(e.fakes["phone"].Sent)
	resolveSendsOneNote(t, e, two, n)
	e.clk.Advance(time.Minute)
	if _, err := built(e, cfg).Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sent := e.fakes["phone"].Sent[n:]; len(sent) != 2 || !sent[1].Resolved {
		t.Errorf("after the flush under one.example %d notes were sent; want one through each destination", len(sent))
	}
}

func resolveSendsOneNote(t *testing.T, e *env, cfg *config.Config, n int) {
	t.Helper()
	if _, err := built(e, cfg).Resolve(context.Background(), "disk", ""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sent := e.fakes["phone"].Sent[n:]; len(sent) != 1 || !sent[0].Resolved {
		t.Fatalf("under the config in force sent %d messages; want the one note for its own destination", len(sent))
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Fatalf("queued %d; want the other destination's note waiting", q)
	}
}

// A note for an incident with no title on record is titled by its key.
func TestAResolveNoteFallsBackToTheKey(t *testing.T) {
	cfg := levelsCfg(time.Second, page(retry5, "phone"))
	e := newEnv(t, cfg, map[string]channel.Capabilities{"phone": human})
	e.notifyKey("page", "disk")
	if err := e.st.WithIncidents(func(m map[string]*storeIncident) error { m["disk"].Latest = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	n := len(e.fakes["phone"].Sent)
	if _, err := e.p.Resolve(context.Background(), "disk", ""); err != nil {
		t.Fatal(err)
	}
	if sent := e.fakes["phone"].Sent[n:]; len(sent) != 1 || sent[0].Title != "Resolved: disk" {
		t.Errorf("sent %+v; want a note titled by the key", sent)
	}
}
