package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
)

// An incident whose sends reached no human, with nothing left pending, must not silence a repeat.
func TestRepeatAfterADeadFirstSendIsSent(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	if r := e.say("page", "k", "t"); r.Status != core.StatusFailed {
		t.Fatalf("first = %q", r.Status)
	}
	e.fakes["a"].SendFunc = nil
	r := e.say("page", "k", "t")
	if r.Status != core.StatusDelivered || sentCount(e, "a") != 2 { // the refused attempt, then this one
		t.Fatalf("repeat = %+v, sent %d; want it sent", r, sentCount(e, "a"))
	}
	if inc := e.incident("k"); inc.Generation != 1 || !inc.Open {
		t.Errorf("incident = %+v; want the same generation, still open", inc)
	}
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Errorf("third = %q; once a human is reached the repeat is suppressed again", r.Status)
	}
}

func TestAQuieterOrSameLevelRepeatAfterADeadSendIsSentToo(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	e.say("page", "k", "t")
	e.fakes["a"].SendFunc = nil
	if r := e.say("low", "k", "t"); r.Status != core.StatusDelivered {
		t.Fatalf("quieter repeat = %q; want sent, no human has been reached", r.Status)
	}
}

func TestADeliveryToANonHumanChannelDoesNotCount(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": human, "log": {}}
	e := newEnv(t, incCfg(0, "a", "log"), caps)
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	e.say("page", "k", "t")
	e.fakes["a"].SendFunc = nil
	if r := e.say("page", "k", "t"); r.Status == core.StatusSuppressed {
		t.Fatalf("repeat = %+v; the log line reached no human", r)
	}
}

func TestPendingSendsStillSuppressARepeat(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "k", "t") // queued, to retry
	e.fakes["a"].SendFunc = nil
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Fatalf("repeat = %q; a send is still pending, so suppress", r.Status)
	}
}

func TestADeliveredFirstSendStillSuppressesARepeat(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Fatalf("repeat = %q", r.Status)
	}
}

func TestAScheduledSendCountsAsReached(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = func(_ context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "a", ProviderID: "SCHED-FAKE-1", Scheduled: true}, nil
	}
	e.say("page", "k", "t")
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Fatalf("repeat = %q; the provider holds a send for the human", r.Status)
	}
}

func TestAChangedRepeatAfterADeadSendIsSentToo(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	e.say("page", "k", "v1")
	e.fakes["a"].SendFunc = nil
	if r := e.say("page", "k", "v2"); r.Status != core.StatusDelivered {
		t.Fatalf("changed repeat = %q; want sent", r.Status)
	}
}

// A queue it cannot list is a failure, not a reason to suppress: the repeat reports it.
func TestARepeatReportsAQueueItCannotList(t *testing.T) {
	e, dir := faultEnv(t)
	e.cfg.IncidentTTL = 24 * time.Hour
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "revoked")
	if r := e.say("page", "k", "t"); r.Status != core.StatusFailed {
		t.Fatalf("first = %q", r.Status)
	}
	if err := os.WriteFile(filepath.Join(dir, "queue", "BAD.a.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock(t, filepath.Join(dir, "dead")) // the corrupt file cannot be quarantined
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"}); err == nil {
		t.Error("the repeat ignored a queue it could not list")
	}
}
