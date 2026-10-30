package core_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
)

// A send that is in flight when Resolve runs is out of CancelIncident's reach. If it then fails, it
// must not go back to the queue, whether by a retry or by a release after our own deadline.
func TestResolveWhileSendInflightDropsTheRetry(t *testing.T) {
	for _, cancelCall := range []bool{false, true} {
		name := map[bool]string{false: "retry", true: "release"}[cancelCall]
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, incCfg(0, "a"), one)
			started, release := make(chan struct{}), make(chan struct{})
			e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
				close(started)
				<-release
				return channel.Receipt{}, channel.Transient(os.ErrDeadlineExceeded)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var res core.Result
			var nerr error
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, nerr = e.p.Notify(ctx, channel.Message{Level: "page", Title: "t", DedupKey: "k"})
			}()
			<-started
			if r := e.resolve("k", "fixed"); r.Status != core.StatusResolved {
				t.Fatalf("resolve = %q", r.Status)
			}
			if cancelCall {
				cancel() // the send now ends early, on our own deadline, not the channel's fault
			}
			close(release)
			wg.Wait()
			if nerr != nil {
				t.Fatalf("Notify: %v", nerr)
			}
			if q, in, d := e.counts(); q+in+d != 0 {
				t.Errorf("queue %d/%d/%d; the resolved incident's send must be dropped, not requeued", q, in, d)
			}
			if res.Status != core.StatusResolved || res.Next != nil {
				t.Errorf("Notify = %q next=%+v; want resolved", res.Status, res.Next)
			}
			log, _ := os.ReadFile(filepath.Join(e.dir, "log.jsonl"))
			if !strings.Contains(string(log), `"status":"cancelled"`) {
				t.Errorf("log %q has no line for the dropped send", log)
			}
		})
	}
}

// Claim fails both when another process took the entry and when Resolve removed it.
func TestNotifyResolvedMidCallReportsResolved(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	core.AfterEnqueue(e.p, func() { e.resolve("k", "fixed") })
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != core.StatusResolved || r.Next != nil || sentCount(e, "a") != 0 {
		t.Errorf("got %q next=%+v sent=%d; want resolved, no retry advice, nothing sent", r.Status, r.Next, sentCount(e, "a"))
	}
}

func TestNotifyLosingAClaimToAnotherProcessStaysQueued(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	core.AfterEnqueue(e.p, func() {
		if _, ok, err := e.st.Claim("M001.a"); !ok || err != nil {
			t.Errorf("claim: %v %v", ok, err)
		}
	})
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != core.StatusQueued || r.Next == nil {
		t.Errorf("got %q next=%+v; the other process is sending it, so it is still on its way", r.Status, r.Next)
	}
}

func TestClaimLossCheckFailureIsReported(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	core.AfterEnqueue(e.p, func() {
		_, _, _ = e.st.Claim("M001.a")
		p := filepath.Join(e.dir, "incidents.json")
		_ = os.Remove(p)
		_ = os.Mkdir(p, 0o700)
	})
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "page", DedupKey: "k"}); err == nil {
		t.Error("an unreadable incident store went unreported")
	}
}
