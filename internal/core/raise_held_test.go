package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// raiseEnv is 13:30 inside a 13:00-14:00 quiet window. "low" holds; "page" holds only if holdPage.
// "local" cannot schedule, so its held sends wait in the queue; "sched" schedules at the provider,
// which answers each scheduled send with its own id (SCHED-FAKE-1, -2, ...) and a send for now with
// NOW-FAKE.
func raiseEnv(t *testing.T, holdPage bool, reminder time.Duration) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"local": human, "sched": schedules}
	cfg := quietCfg("local", "sched")
	cfg.Levels[1].HoldInQuietHours = holdPage
	for i := range cfg.Levels {
		cfg.Levels[i].Reminder = reminder
	}
	e := newEnv(t, cfg, caps)
	e.clk.Advance(90 * time.Minute)
	n := 0
	e.fakes["sched"].SendFunc = func(_ context.Context, _ channel.Message, s channel.SendSpec) (channel.Receipt, error) {
		if s.DeliverAt.IsZero() {
			return channel.Receipt{Instance: "sched", ProviderID: "NOW-FAKE"}, nil
		}
		n++
		return channel.Receipt{Instance: "sched", ProviderID: fmt.Sprintf("SCHED-FAKE-%d", n), Scheduled: true}, nil
	}
	return e
}

// afterTheWindow moves past the window's end and flushes, as the first call after it would.
func afterTheWindow(t *testing.T, e *env) {
	t.Helper()
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatalf("flush after the window: %v", err)
	}
}

func schedCancelled(e *env) map[string]int {
	out := map[string]int{}
	for _, c := range e.fakes["sched"].Cancelled {
		out[c.ProviderID]++
	}
	return out
}

// A louder repeat during quiet hours pages now. The quieter copy it replaces, still held, must not
// fire when the window ends: the human would be paged twice for one incident.
func TestALouderRepeatInQuietHoursPagesOnce(t *testing.T) {
	e := raiseEnv(t, false, 0)
	if r := e.say("low", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("low: %q; want held", r.Status)
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered {
		t.Fatalf("page: %q; want delivered", r.Status)
	}
	if got := schedCancelled(e); got["SCHED-FAKE-1"] != 1 {
		t.Errorf("cancelled %v; the scheduled quieter copy must be cancelled when the louder one goes out", got)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d; the held quieter copy must leave the queue", q, in, d)
	}
	for _, r := range e.incident("k").Reached {
		if len(r.Scheduled) != 0 {
			t.Errorf("reached %+v; a cancelled scheduled send is no longer held", r)
		}
	}
	afterTheWindow(t, e)
	if n := sentCount(e, "local"); n != 1 {
		t.Errorf("local got %d sends; want only the louder page, nothing more after the window", n)
	}
	if n := sentCount(e, "sched"); n != 2 {
		t.Errorf("sched got %d sends; want the scheduled copy and the louder page", n)
	}
}

// A reminder sends the incident again. A copy still held from the earlier send is replaced, not
// added to: when the window ends, one message arrives.
func TestAReminderReplacesTheHeldCopy(t *testing.T) {
	e := raiseEnv(t, true, 5*time.Minute)
	if r := e.say("page", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("first: %q; want held", r.Status)
	}
	e.clk.Advance(10 * time.Minute) // 13:40: the reminder is due, the window still on
	if r := e.say("page", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("reminder: %q; want held", r.Status)
	}
	if got := schedCancelled(e); got["SCHED-FAKE-1"] != 1 || got["SCHED-FAKE-2"] != 0 {
		t.Errorf("cancelled %v; want the first scheduled copy cancelled and the reminder kept", got)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued %d; want only the reminder's held send for local", q)
	}
	afterTheWindow(t, e)
	if n := sentCount(e, "local"); n != 1 {
		t.Errorf("local got %d sends after the window; want one", n)
	}
}

// The cancel of the replaced scheduled copy can fail. The louder page was delivered, so the call
// still succeeds; the failure is a warning, and the cancel is queued for a flush to retry.
func TestARaisesFailedCancelIsAWarningAndIsQueued(t *testing.T) {
	e := raiseEnv(t, false, 0)
	e.say("low", "k", "t")
	p := newWith(e, map[string]channel.Channel{"local": e.fakes["local"], "sched": failingCancel{e.fakes["sched"], channel.Transient}})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if err != nil || r.Status != core.StatusDelivered {
		t.Fatalf("Notify = %q, %v; want delivered and no error: the human was reached", r.Status, err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Error(), "the scheduled send on sched could not be cancelled: provider refused") {
		t.Errorf("warnings %v; want the failed cancel as one warning", r.Warnings)
	}
	if got := queuedKinds(t, e); len(got) != 1 || got[0] != "cancel" {
		t.Errorf("queue %v; want only the cancel, kept for a retry", got)
	}
}

// queuedKinds is the kind of each queued entry ("" for a send, "cancel" for a cancellation).
func queuedKinds(t *testing.T, e *env) []string {
	t.Helper()
	due, err := e.st.Due(e.clk.Now().AddDate(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range due {
		var pl struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(d.Data, &pl)
		out = append(out, pl.Kind)
	}
	return out
}

// A raise that cannot take the held copy out of the queue records nothing and sends nothing: the
// store failure stops it, as any store failure under the incident lock does. Here the queue holds
// an unreadable file that cannot be set aside, because dead/ is read-only; new entries can still be
// written, so only the removal fails.
func TestARaiseThatCannotRemoveTheHeldCopyStops(t *testing.T) {
	e := raiseEnv(t, false, 0)
	e.say("low", "k", "t")
	if err := os.WriteFile(filepath.Join(e.dir, "queue", "junk.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	dead := filepath.Join(e.dir, "dead")
	if err := os.Chmod(dead, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dead, 0o700) })
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if pr, ok := problem.As(err); !ok || pr.Kind != problem.Store || r.Status != "" {
		t.Fatalf("Notify = %+v, %v; want a store problem and no result", r, err)
	}
	if n := sentCount(e, "local"); n != 0 {
		t.Errorf("local got %d sends; nothing may go out", n)
	}
	if inc := e.incident("k"); inc.Level != "low" {
		t.Errorf("incident level %q; the raise must not be recorded", inc.Level)
	}
}
