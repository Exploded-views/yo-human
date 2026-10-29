package core_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

var one = map[string]channel.Capabilities{"a": human}

func TestSuppressedSendsNothing(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if r := e.say("page", "k", "disk full"); r.Status != core.StatusDelivered {
		t.Fatalf("first = %q", r.Status)
	}
	r := e.say("low", "k", "disk still full")
	if r.Status != core.StatusSuppressed || r.ID == "" || len(r.Channels) != 0 || r.Next != nil {
		t.Fatalf("repeat = %+v; want suppressed with an id and no sends", r)
	}
	if sentCount(e, "a") != 1 {
		t.Errorf("sent %d; a suppressed repeat must send nothing", sentCount(e, "a"))
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("queue %d/%d/%d; a suppressed repeat leaves nothing behind", q, in, d)
	}
}

func TestSuppressedKeepsTheLatestContent(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "disk full")
	e.say("low", "k", "disk still full")
	if inc := e.incident("k"); inc.Latest != "disk still full" || inc.Level != "page" || !inc.Open {
		t.Errorf("incident = %+v; want the latest title kept and the level unchanged", inc)
	}
}

func TestSameLevelRepeatIsSuppressed(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Errorf("same level = %q, want suppressed", r.Status)
	}
}

func TestSuppressedStillDrainsTheBacklog(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.say("page", "other", "u") // queued, to retry
	e.fakes["a"].SendFunc = nil
	e.clk.Advance(time.Hour)
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Fatalf("status %q", r.Status)
	}
	if q, _, _ := e.counts(); q != 0 {
		t.Errorf("queued = %d; a suppressed call still sends what is due", q)
	}
}

func TestChangedRepeatUpdatesOnlyNativeDedup(t *testing.T) {
	caps := map[string]channel.Capabilities{
		"pd":   {ReachesHuman: true, NativeDedup: true},
		"ntfy": human,
	}
	e := newEnv(t, incCfg(0, "pd", "ntfy"), caps)
	e.say("page", "k", "v1")
	r := e.say("page", "k", "v2")
	if sentCount(e, "pd") != 2 || sentCount(e, "ntfy") != 1 {
		t.Fatalf("pd sent %d, ntfy sent %d; want 2 and 1", sentCount(e, "pd"), sentCount(e, "ntfy"))
	}
	if r.Status != core.StatusDelivered || len(r.Channels) != 1 || r.Channels[0].Channel != "pd" {
		t.Errorf("result = %+v; want delivered by pd alone", r)
	}
	if inc := e.incident("k"); inc.Latest != "v2" {
		t.Errorf("Latest = %q", inc.Latest)
	}
	if r := e.say("page", "k", "v2"); r.Status != core.StatusSuppressed {
		t.Errorf("the update's content is the new baseline; got %q", r.Status)
	}
}

func TestChangedRepeatWithoutNativeDedupIsRecordedOnly(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "v1")
	r := e.say("page", "k", "v2")
	if r.Status != core.StatusSuppressed || sentCount(e, "a") != 1 {
		t.Errorf("status %q, sent %d; want suppressed and one send", r.Status, sentCount(e, "a"))
	}
	if inc := e.incident("k"); inc.Latest != "v2" {
		t.Errorf("Latest = %q, want the new content kept", inc.Latest)
	}
}

func TestLouderRepeatSendsAndRaises(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("low", "k", "t")
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered || sentCount(e, "a") != 2 {
		t.Fatalf("louder repeat: %q, sent %d", r.Status, sentCount(e, "a"))
	}
	if inc := e.incident("k"); inc.Level != "page" || inc.Rank != 1 {
		t.Errorf("incident = %+v; want raised to page", inc)
	}
	if r := e.say("low", "k", "t"); r.Status != core.StatusSuppressed {
		t.Errorf("a lower repeat after the raise = %q, want suppressed", r.Status)
	}
}

func TestReminderSendsAgainWhenDue(t *testing.T) {
	e := newEnv(t, incCfg(30*time.Minute, "a"), one)
	e.say("page", "k", "t")
	e.clk.Advance(29 * time.Minute)
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Fatalf("before the reminder = %q", r.Status)
	}
	e.clk.Advance(2 * time.Minute)
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered || sentCount(e, "a") != 2 {
		t.Fatalf("after the reminder = %q, sent %d", r.Status, sentCount(e, "a"))
	}
	e.clk.Advance(10 * time.Minute)
	if r := e.say("page", "k", "t"); r.Status != core.StatusSuppressed {
		t.Errorf("a reminder restarts the interval; got %q", r.Status)
	}
}

func TestExpiredIncidentSendsAsNew(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "k", "t")
	e.clk.Advance(25 * time.Hour)
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered || sentCount(e, "a") != 2 {
		t.Fatalf("after the ttl = %q, sent %d", r.Status, sentCount(e, "a"))
	}
	if inc := e.incident("k"); !inc.Opened.Equal(t0.Add(25 * time.Hour)) {
		t.Errorf("Opened = %v; an expired incident reopens fresh", inc.Opened)
	}
}

func TestNoDedupKeyIsNeverAnIncident(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.say("page", "", "t")
	e.say("page", "", "t")
	if sentCount(e, "a") != 2 {
		t.Errorf("sent %d; messages without a key are not repeats", sentCount(e, "a"))
	}
}

func TestConcurrentRepeatsPageOnce(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[core.Status]int{}
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
			if err != nil {
				t.Errorf("Notify: %v", err)
			}
			mu.Lock()
			got[r.Status]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if n := sentCount(e, "a"); n != 1 {
		t.Errorf("sent %d times; one incident pages once", n)
	}
	if got[core.StatusDelivered] != 1 || got[core.StatusSuppressed] != 19 {
		t.Errorf("statuses = %v; want 1 delivered and 19 suppressed", got)
	}
}

// reachedEnv has four channels: pd and sched succeed (the latter by scheduling), down fails for good
// and late fails for now. One page to "k" has been sent.
func reachedEnv(t *testing.T) *env {
	t.Helper()
	caps := map[string]channel.Capabilities{"pd": human, "late": human, "sched": human, "down": human}
	e := newEnv(t, incCfg(0, "pd", "late", "sched", "down"), caps)
	receipt := func(r channel.Receipt) func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) { return r, nil }
	}
	e.fakes["pd"].SendFunc = receipt(channel.Receipt{Instance: "pd", ProviderID: "PD-FAKE-1"})
	e.fakes["sched"].SendFunc = receipt(channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true})
	e.fakes["down"].SendFunc = failWith(channel.Permanent, "revoked")
	e.fakes["late"].SendFunc = failWith(channel.Transient, "down")
	e.say("low", "k", "t")
	return e
}

func TestReachedComesFromReceipts(t *testing.T) {
	e := reachedEnv(t)
	got := map[string]store.Reached{}
	for _, r := range e.incident("k").Reached {
		got[r.Instance] = r
	}
	if len(got) != 2 {
		t.Fatalf("Reached = %+v; want pd and sched only", got)
	}
	if pd := got["pd"]; len(pd.Delivered) == 0 || len(pd.Scheduled) != 0 {
		t.Errorf("pd = %+v; want delivered", pd)
	}
	if sc := got["sched"]; len(sc.Delivered) > 0 || len(sc.Scheduled) != 1 || sc.Scheduled[0].ID != "SCHED-FAKE-1" {
		t.Errorf("sched = %+v; want one scheduled receipt and nothing delivered", sc)
	}
}

func TestAQueuedSendIsRecordedWhenItLands(t *testing.T) {
	e := reachedEnv(t)
	e.fakes["late"].SendFunc = nil
	e.clk.Advance(time.Hour)
	if _, err := e.p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(e.incident("k").Reached); n != 3 {
		t.Errorf("Reached has %d after the flush; the late send must be recorded", n)
	}
}

// One record per instance, however many sends reach it: delivered and every held provider id are
// kept, whichever came first. A louder repeat replaces what an instance held before it: that is
// cancelled and leaves the record.
func TestLaterReceiptsAccumulateOnTheInstance(t *testing.T) {
	e := reachedEnv(t) // pd delivered, sched scheduled
	e.fakes["pd"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "pd", ProviderID: "PD-FAKE-2", Scheduled: true}, nil
	}
	e.fakes["sched"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "sched", ProviderID: "SCHED-FAKE-2"}, nil
	}
	e.say("page", "k", "louder")
	got := map[string]store.Reached{}
	for _, r := range e.incident("k").Reached {
		if _, dup := got[r.Instance]; dup {
			t.Errorf("%s recorded twice", r.Instance)
		}
		got[r.Instance] = r
	}
	if pd := got["pd"]; len(pd.Delivered) == 0 || len(pd.Scheduled) != 1 || pd.Scheduled[0].ID != "PD-FAKE-2" {
		t.Errorf("pd = %+v; want delivered, with the later held send", pd)
	}
	if sc := got["sched"]; len(sc.Delivered) == 0 || len(sc.Scheduled) != 0 {
		t.Errorf("sched = %+v; want delivered, and the earlier held send replaced", sc)
	}
	if c := e.fakes["sched"].Cancelled; len(c) != 1 || c[0].ProviderID != "SCHED-FAKE-1" {
		t.Errorf("cancelled %+v; want the earlier held send cancelled", c)
	}
}

func TestTheSameReceiptTwiceIsKeptOnce(t *testing.T) {
	rcpt := core.ChannelResult{Channel: "sched", ProviderID: "SCHED-FAKE-1", Scheduled: true}
	rs := core.AddReached(core.AddReached(nil, rcpt, "fp"), rcpt, "fp")
	if len(rs) != 1 || len(rs[0].Scheduled) != 1 {
		t.Errorf("reached %+v; want the id once", rs)
	}
}

func TestIncidentStoreFailureStopsNotify(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	if err := os.Mkdir(filepath.Join(e.dir, "incidents.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", DedupKey: "k"})
	if err == nil || sentCount(e, "a") != 0 || r.ID != "M001" {
		t.Errorf("err = %v, sent %d, id %q; want the store error, no send, and the id", err, sentCount(e, "a"), r.ID)
	}
	if q, _, _ := e.counts(); q != 0 {
		t.Errorf("queued = %d; a message the incident could not record is not enqueued", q)
	}
}

func TestEnqueueFailureLeavesNoIncident(t *testing.T) {
	e, dir := faultEnv(t)
	e.cfg.IncidentTTL = time.Hour
	lock(t, filepath.Join(dir, "queue"))
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page", DedupKey: "k"}); err == nil {
		t.Fatal("no error")
	}
	if e.hasIncident("k") {
		t.Error("an incident was recorded for a message that never reached the queue; its repeats would be suppressed")
	}
}

func TestReachedFailureAfterASendIsReported(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		p := filepath.Join(e.dir, "incidents.json")
		_ = os.Remove(p)
		_ = os.Mkdir(p, 0o700)
		return channel.Receipt{}, nil
	}
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", DedupKey: "k"})
	if err == nil || len(r.Channels) != 1 || r.Channels[0].Status != "sent" {
		t.Errorf("err = %v, channels %+v; want the failure reported, the send still sent", err, r.Channels)
	}
}
