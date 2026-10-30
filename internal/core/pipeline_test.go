package core_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func TestEnqueueBeforeSend(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "phone")), map[string]channel.Capabilities{"phone": human})
	var q, in int
	seen := false
	e.fakes["phone"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		if !seen { // the first network call is the one that must find the entry written
			seen = true
			q, in, _ = e.counts()
		}
		return channel.Receipt{}, nil
	}
	e.notify("page")
	if q != 0 || in != 1 {
		t.Errorf("during the send queued=%d inflight=%d; want the entry written and claimed (0, 1) before the network call", q, in)
	}
	if q, in, d := e.counts(); q+in+d != 0 {
		t.Errorf("after a successful send counts = %d %d %d; want an empty store", q, in, d)
	}
}

func TestStatuses(t *testing.T) {
	tr, pe := failWith(channel.Transient, "down"), failWith(channel.Permanent, "revoked")
	caps := map[string]channel.Capabilities{"a": human, "b": human}
	tests := []struct {
		name         string
		a, b         func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error)
		want         core.Status
		wantA, wantB string
		queued, dead int
	}{
		{"all sent", nil, nil, core.StatusDelivered, "sent", "sent", 0, 0},
		{"one of two", nil, tr, core.StatusPartial, "sent", "queued", 1, 0},
		{"none reachable", tr, tr, core.StatusQueued, "queued", "queued", 2, 0},
		{"permanent plus transient", pe, tr, core.StatusQueued, "dead", "queued", 1, 1},
		{"all permanent", pe, pe, core.StatusFailed, "dead", "dead", 0, 2},
		{"permanent but one sent", pe, nil, core.StatusPartial, "dead", "sent", 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, levelsCfg(time.Second, page(retry5, "a", "b")), caps)
			e.fakes["a"].SendFunc, e.fakes["b"].SendFunc = tc.a, tc.b
			r := e.notify("page")
			got := statuses(r)
			if r.Status != tc.want || got["a"] != tc.wantA || got["b"] != tc.wantB {
				t.Errorf("status %q a=%q b=%q; want %q a=%q b=%q", r.Status, got["a"], got["b"], tc.want, tc.wantA, tc.wantB)
			}
			if r.ID != "M001" {
				t.Errorf("ID = %q; want the generated id", r.ID)
			}
			q, in, d := e.counts()
			if q != tc.queued || in != 0 || d != tc.dead {
				t.Errorf("queued=%d inflight=%d dead=%d; want %d 0 %d", q, in, d, tc.queued, tc.dead)
			}
			if (tc.want == core.StatusQueued) != (r.Next != nil) {
				t.Errorf("Next = %+v for status %q; want it exactly when queued", r.Next, r.Status)
			}
		})
	}
}

func TestNoneReachableIsQueuedWithNext(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	r := e.notify("page")
	want := &core.Next{Do: "yo-human flush", AfterSeconds: 60, AttemptsLeft: 5, Then: "tell the human in your session"}
	if r.Status != core.StatusQueued || !reflect.DeepEqual(r.Next, want) {
		t.Errorf("got %q %+v; want queued %+v", r.Status, r.Next, want)
	}
}

func TestAllPermanentIsFailed(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a", "b")), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = failWith(channel.Permanent, "key revoked")
	e.fakes["b"].SendFunc = failWith(channel.Permanent, "bad request")
	r := e.notify("page")
	need(t, r, e.fakes["a"], 2, 0)
	if r.Status != core.StatusFailed || r.Next != nil {
		t.Errorf("got %q next=%+v; want failed with no next", r.Status, r.Next)
	}
	if _, _, d := e.counts(); d != 2 {
		t.Errorf("dead = %d; want 2", d)
	}
	dead, err := e.st.DeadEntries()
	if err != nil || len(dead) != 2 {
		t.Fatalf("DeadEntries = %v, %v", dead, err)
	}
	if !strings.Contains(dead[0].Reason+dead[1].Reason, "key revoked") {
		t.Errorf("reasons %q %q lost the error text", dead[0].Reason, dead[1].Reason)
	}
	if r.Channels[0].Error != "key revoked" || r.Channels[1].Error != "bad request" {
		t.Errorf("channel errors = %+v; want the channels' text, in plan order", r.Channels)
	}
}

func TestConfigErrorIsDeadLetteredAndReportedAsConfig(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	e.fakes["a"].SendFunc = failWith(channel.Misconfigured, "no such topic")
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t"})
	need(t, r, e.fakes["a"], 1, 0)
	if r.Status != core.StatusFailed || r.Channels[0].Status != "dead" {
		t.Errorf("got %q %+v; want failed, dead", r.Status, r.Channels)
	}
	var p *problem.Problem
	if !errors.As(err, &p) || p.Kind != problem.Config {
		t.Fatalf("err = %v; want a Config problem so the CLI exits 78", err)
	}
	if !strings.Contains(p.What, "a") || !strings.Contains(p.What, "no such topic") {
		t.Errorf("problem %q; want the instance and the channel's error text", p.What)
	}
	if dead, _ := e.st.DeadEntries(); len(dead) != 1 || !strings.Contains(dead[0].Reason, "no such topic") {
		t.Errorf("dead = %+v; want the entry kept with its reason", dead)
	}
	if !strings.Contains(p.Fix, "dead letter") {
		t.Errorf("fix %q; want it to say the message is in the dead letter, which it is", p.Fix)
	}
}

func TestConfigErrorNextToAQueuedChannelIsNotAConfigProblem(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a", "b")), map[string]channel.Capabilities{"a": human, "b": human})
	e.fakes["a"].SendFunc = failWith(channel.Misconfigured, "bad")
	e.fakes["b"].SendFunc = failWith(channel.Transient, "down")
	r, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
	if err != nil || r.Status != core.StatusQueued {
		t.Errorf("got %q, %v; want queued without error: the other channel can still deliver", r.Status, err)
	}
}

func TestLogChannelNeverCountsAsDelivery(t *testing.T) {
	caps := map[string]channel.Capabilities{"file": {}, "phone": human}
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "file", "phone")), caps)
	e.fakes["phone"].SendFunc = failWith(channel.Transient, "down")
	r := e.notify("page")
	if r.Status != core.StatusQueued || statuses(r)["file"] != "sent" {
		t.Errorf("got %q %v; want queued although the log line was written", r.Status, statuses(r))
	}
}

func TestUnknownLevelStoresNothing(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
	if _, err := e.p.Notify(context.Background(), channel.Message{Level: "nope"}); err == nil {
		t.Fatal("Notify with an unknown level succeeded")
	}
	if q, in, d := e.counts(); q+in+d != 0 || len(e.fakes["a"].Sent) != 0 {
		t.Errorf("counts %d %d %d, sent %d; want nothing", q, in, d, len(e.fakes["a"].Sent))
	}
}

func TestDeadlineBoundsTheCall(t *testing.T) {
	e := newEnv(t, levelsCfg(200*time.Millisecond, page(retry5, "slow")), map[string]channel.Capabilities{"slow": human})
	cancelled := make(chan bool, 1)
	e.fakes["slow"].SendFunc = func(ctx context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		select {
		case <-ctx.Done():
			cancelled <- true
			return channel.Receipt{}, ctx.Err()
		case <-time.After(5 * time.Second):
			cancelled <- false
			return channel.Receipt{}, nil
		}
	}
	start := time.Now()
	r := e.notify("page")
	if el := time.Since(start); el > 300*time.Millisecond {
		t.Errorf("Notify took %v; want within 300ms of a 200ms deadline", el)
	}
	select {
	case ok := <-cancelled:
		if !ok {
			t.Error("the channel's context was not cancelled at the deadline")
		}
	default:
		t.Error("Notify returned without the channel having seen the deadline")
	}
	if r.Status != core.StatusQueued || r.Channels[0].Status != "queued" {
		t.Errorf("got %q %+v; want the blocked channel queued", r.Status, r.Channels)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d; want the timed-out send kept for retry", q)
	}
}

func TestSendSpecCarriesTheLevelsSettings(t *testing.T) {
	lv := page(retry5, "a")
	lv.Send[0].Interruption = channel.Critical
	lv.Send[0].Options = map[string]any{"tag": "x"}
	e := newEnv(t, levelsCfg(time.Second, lv), map[string]channel.Capabilities{"a": human})
	r := e.notify("page")
	need(t, r, e.fakes["a"], 0, 1)
	s := e.fakes["a"].Specs[0]
	if s.Instance != "a" || s.Interruption != channel.Critical || s.Options["tag"] != "x" || !s.DeliverAt.IsZero() {
		t.Errorf("spec = %+v", s)
	}
	if m := e.fakes["a"].Sent[0]; m.ID != "M001" || !m.Created.Equal(t0) || m.Title != "t" {
		t.Errorf("message = %+v; want the id and creation time filled in", m)
	}
}
