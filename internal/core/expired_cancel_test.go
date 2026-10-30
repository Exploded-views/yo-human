package core_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// expiredThenLoud has a generation of "k" that expired while its "low" page was still scheduled on
// n; "page" does not hold, so a new notify at it pages now (13:50, inside the 13:00-14:00 window).
func expiredThenLoud(t *testing.T) *env {
	t.Helper()
	e := quietN(t, true, false)
	e.cfg.IncidentTTL = 10 * time.Minute
	if r := e.say("low", "k", "t"); r.Status != core.StatusHeld {
		t.Fatalf("low: %q; want held", r.Status)
	}
	e.clk.Advance(20 * time.Minute)
	return e
}

// hangingCancel is a channel whose cancellations hang until the call's deadline.
type hangingCancel struct{ *channeltest.Fake }

func (h hangingCancel) Cancel(ctx context.Context, _ channel.Receipt) error {
	<-ctx.Done()
	return channel.Transient(ctx.Err())
}

// The new page goes out first. A cancel of the expired generation that hangs must not eat the
// deadline the page needs.
func TestAnExpiredGenerationsCancelRunsAfterTheNewPage(t *testing.T) {
	e := expiredThenLoud(t)
	e.cfg.Deadline = 300 * time.Millisecond
	p := newWith(e, map[string]channel.Channel{"n": hangingCancel{e.fakes["n"]}})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if r.Status != core.StatusDelivered || err != nil {
		t.Errorf("Notify = %q, %v; want delivered: the page goes out before the old generation's cancel", r.Status, err)
	}
	if got := queuedKinds(t, e); len(got) != 1 || got[0] != "cancel" {
		t.Errorf("queue %v; want the cut-short cancel kept for a retry", got)
	}
}

// A delivered page exits 0. The old generation's cancel the provider refused is a warning beside
// it, kept in the dead letter where status shows it.
func TestAnExpiredGenerationsRefusedCancelDoesNotFailADeliveredPage(t *testing.T) {
	e := expiredThenLoud(t)
	p := newWith(e, map[string]channel.Channel{"n": failingCancel{e.fakes["n"], channel.Permanent}})
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if r.Status != core.StatusDelivered || err != nil {
		t.Fatalf("Notify = %q, %v; want delivered with no error", r.Status, err)
	}
	if len(r.Warnings) != 1 {
		t.Fatalf("warnings %v; want the refused cancel as one warning", r.Warnings)
	}
	if pr, ok := problem.As(r.Warnings[0]); !ok || pr.Kind != problem.Unavailable {
		t.Errorf("warning %v; want the cancel problem", r.Warnings[0])
	}
	if got := deadKinds(t, e); len(got) != 1 || got[0] != "cancel: provider refused" {
		t.Errorf("dead letter %q; want the refused cancel there", got)
	}
}

// A failed cancel that cannot even be kept in the queue is a store failure: that stays an error,
// beside the warning for the cancel itself.
func TestAnExpiredGenerationsCancelThatCannotBeKeptIsAStoreError(t *testing.T) {
	e := expiredThenLoud(t)
	p := newWith(e, map[string]channel.Channel{"n": failingCancel{e.fakes["n"], channel.Permanent}})
	// newWith's ids: N001 is the message; the failed cancel's entry is N002, keyed N003.n.
	if err := os.Mkdir(filepath.Join(e.dir, "queue", "N003.n.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if pr, ok := problem.As(err); !ok || pr.Kind != problem.Store || r.Status != core.StatusDelivered {
		t.Errorf("Notify = %q, %v; want delivered, with the store failure as the error", r.Status, err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Error(), "could not be cancelled") {
		t.Errorf("warnings %v; want the cancel itself as a warning", r.Warnings)
	}
}

// A queued cancel that ran out the deadline has had a fair run: an attempt is spent, measured in
// real time whatever clock the pipeline keeps, so a provider that always hangs reaches the dead
// letter in the end.
func TestAQueuedCancelThatHangsSpendsAnAttempt(t *testing.T) {
	e := queuedCancel(t)
	cfg := *e.cfg
	cfg.Deadline = 200 * time.Millisecond
	if _, err := e.with(&cfg, map[string]channel.Channel{"phone": hangingCancel{e.fakes["phone"]}}).Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	due, _ := e.st.Due(e.clk.Now().AddDate(1, 0, 0))
	if len(due) != 1 || due[0].Attempts != 1 {
		t.Errorf("%d queued, attempts %v; want the cancel back with one attempt spent", len(due), attemptsOf(due))
	}
}

func attemptsOf(due []store.Pending) []int {
	var out []int
	for _, d := range due {
		out = append(out, d.Attempts)
	}
	return out
}
