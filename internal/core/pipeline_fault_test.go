package core_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// faultEnv is a one-channel env on a real directory, for tests that break the directory.
func faultEnv(t *testing.T) (*env, string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission faults need a non-root user")
	}
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	return e, e.dir
}

func lock(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
}

func TestNotifyReportsAStoreFailureAndSendsNothing(t *testing.T) {
	e, dir := faultEnv(t)
	lock(t, filepath.Join(dir, "queue"))
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
	if err == nil || len(e.fakes["a"].Sent) != 0 {
		t.Errorf("err = %v, sent %d; want the enqueue failure, and no send without a queued copy", err, len(e.fakes["a"].Sent))
	}
	if r.ID != "M001" {
		t.Errorf("ID = %q; want it reported so the agent can say which message", r.ID)
	}
}

func TestRecoverFailureStopsNotifyAndFlush(t *testing.T) {
	e, dir := faultEnv(t)
	if err := os.RemoveAll(filepath.Join(dir, "inflight")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page"}); err == nil {
		t.Error("Notify ignored a failing recovery")
	}
	if _, err := e.p.Flush(context.Background()); err == nil {
		t.Error("Flush ignored a failing recovery")
	}
}

func TestClaimFailureIsReportedAndTheEntryKept(t *testing.T) {
	e, dir := faultEnv(t)
	lock(t, filepath.Join(dir, "inflight"))
	_, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
	if err == nil || len(e.fakes["a"].Sent) != 0 {
		t.Errorf("err = %v, sent %d; want a store error and no send", err, len(e.fakes["a"].Sent))
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d; want the unclaimed entry kept for the next flush", q)
	}
}

func TestSettleFailureAfterASendIsReported(t *testing.T) {
	e, dir := faultEnv(t)
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		lock(t, filepath.Join(dir, "inflight")) // the entry can no longer be removed
		return channel.Receipt{}, nil
	}
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
	if err == nil {
		t.Error("a failed Complete went unreported")
	}
	if len(r.Channels) != 1 || r.Channels[0].Status != "sent" {
		t.Errorf("channels = %+v; the send happened, so it is reported as sent", r.Channels)
	}
}

func TestLogFailureIsReportedWithTheResult(t *testing.T) {
	e, dir := faultEnv(t)
	if err := os.Mkdir(filepath.Join(dir, "log.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
	if err == nil || r.Status != core.StatusDelivered {
		t.Errorf("status %q, err %v; want delivered with the log failure reported", r.Status, err)
	}
}

func TestListingFailureIsReported(t *testing.T) {
	e, dir := faultEnv(t)
	// A corrupt queue file must be quarantined to dead/, which cannot be written: Due fails.
	if err := os.WriteFile(filepath.Join(dir, "queue", "BAD.a.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock(t, filepath.Join(dir, "dead"))
	if _, err := e.p.Flush(context.Background()); err == nil {
		t.Error("Flush ignored a queue it could not list")
	}
}

func TestUnencodableOptionsAreAnInternalProblem(t *testing.T) {
	lv := page(retry5, "a")
	lv.Send[0].Options = map[string]any{"bad": make(chan int)}
	e := newEnv(t, levelsCfg(time.Second, lv), map[string]channel.Capabilities{"a": human})
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page"}); err == nil || len(e.fakes["a"].Sent) != 0 {
		t.Errorf("err = %v, sent %d; want an error and no send", err, len(e.fakes["a"].Sent))
	}
}

func enqueueRaw(t *testing.T, e *env, key, data string) {
	t.Helper()
	if err := e.st.Enqueue(store.Pending{Key: key, Data: []byte(data)}); err != nil {
		t.Fatal(err)
	}
}

func TestUnreadableEntriesGoToTheDeadLetter(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	enqueueRaw(t, e, "X1.a", `[1]`) // not a payload
	r, err := e.p.Flush(context.Background())
	if err != nil || len(r.Channels) != 1 {
		t.Fatalf("flush = %+v, %v", r, err)
	}
	if c := r.Channels[0]; c.Status != "dead" || c.Error == "" {
		t.Errorf("%+v; want dead with a reason", c)
	}
	if _, _, d := e.counts(); d != 1 {
		t.Errorf("dead = %d; want it kept, never dropped", d)
	}
}

// The queue is per user, the config per invocation: an entry for a channel this config does not
// name may belong to another config, so it is parked, never buried.
func TestEntryForAChannelThisConfigDoesNotNameIsParked(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	enqueueRaw(t, e, "X2.gone", `{"instance":"gone","message":{"level":"page"}}`)
	r, err := e.p.Flush(context.Background())
	if err != nil || len(r.Channels) != 1 || r.Channels[0].Status != core.ChannelQueued || !strings.Contains(r.Channels[0].Error, "not configured") {
		t.Fatalf("flush = %+v, %v; want the entry queued with a reason naming the config", r, err)
	}
	if q, _, d := e.counts(); q != 1 || d != 0 {
		t.Errorf("queued %d, dead %d; want it kept in the queue", q, d)
	}
}

func TestEntryForAnUnknownLevelRetriesWithDefaults(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	enqueueRaw(t, e, "X1.a", `{"instance":"a","fingerprint":"`+core.FingerprintOf(e.cfg.Channels["a"])+`","message":{"level":"removed-level"}}`)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(59 * time.Second)
	due, _ := e.st.Due(e.clk.Now())
	e.clk.Advance(time.Second)
	later, _ := e.st.Due(e.clk.Now())
	if len(due) != 0 || len(later) != 1 {
		t.Errorf("due at +59s: %d, at +60s: %d; want the default 60s backoff", len(due), len(later))
	}
}

func TestConfigProblemDoesNotClaimADeadLetterThatFailed(t *testing.T) {
	e, dir := faultEnv(t)
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		lock(t, filepath.Join(dir, "dead")) // the dead letter can no longer be written
		return channel.Receipt{}, channel.Misconfigured(errors.New("no such topic"))
	}
	_, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
	var p *problem.Problem
	if !errors.As(err, &p) || p.Kind != problem.Config {
		t.Fatalf("err = %v; want the Config problem first, whatever else failed", err)
	}
	if strings.Contains(p.Fix, "dead letter") {
		t.Errorf("fix %q; the dead-letter write failed, so it must not say the message is there", p.Fix)
	}
}
