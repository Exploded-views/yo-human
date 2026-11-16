package web_test

import (
	"context"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

func TestNodeWriteTools(t *testing.T) {
	h := newHarness(t)
	cs := h.session(h.node)
	draft := obj{"kind": "question", "severity": "normal", "handle": "q1", "title": "Which port?", "action": "none", "urgency": "digest"}
	posted := mustOK(t, cs, "post", draft)
	eq(t, "outcome", posted["outcome"], "created")
	has(t, "item", at(posted, "item").(obj), obj{"to": "human", "swarm": "crew", "from": "ci-1", "revision": float64(1)})
	eq(t, "a repeat post, naming the owner", mustOK(t, cs, "post", forge(draft, obj{"to": "human"}))["outcome"], "unchanged")
	mustFail(t, cs, "post", forge(draft, obj{"to": "crew", "handle": "q2"}), "invalid")

	id := at(posted, "item", "id")
	rep := mustOK(t, cs, "reply", obj{"thread_id": id, "body": "thanks", "severity": "low", "urgency": "digest"})
	has(t, "message", at(rep, "message").(obj), obj{"from": "ci-1", "body": "thanks", "urgency": "digest", "severity": "low"})
	mustFail(t, h.session(h.other), "reply", obj{"thread_id": id, "body": "x", "severity": "low", "urgency": "digest"}, "not_found")

	ap := h.seed(nodeP, mergeApproval("m7"))
	rl := mustOK(t, cs, "relay", obj{"item_id": ap.ID, "revision": 1, "text": "the owner said yes", "human_ts": humanTS})
	has(t, "relay", at(rl, "relay").(obj), obj{"source": "node:ci-1", "text": "the owner said yes", "item_id": ap.ID})
	mustFail(t, cs, "relay", obj{"item_id": ap.ID, "revision": 7, "text": "x", "human_ts": humanTS}, "stale_revision")

	doc := obj{"swarm": "crew", "sessions": []obj{{"name": "ci", "state": "working"}}, "waiting_on_human": []string{"q1"}, "main_health": "ok", "spend": 12.5, "ts": 1790000000}
	mustOK(t, cs, "status_push", doc)
	mustFail(t, cs, "status_push", forge(doc, obj{"swarm": "other"}), "invalid")
	mustFail(t, cs, "status_push", forge(doc, obj{"sessions": []obj{{"name": "ci", "state": "napping"}}}), "invalid")
}

func TestNodeReadTools(t *testing.T) {
	h := newHarness(t)
	q := h.seed(nodeP, question("q1"))
	cs, duet := h.session(h.node), h.session(h.duet)
	mustOK(t, duet, "record_answer", obj{"item_id": q.ID, "revision": 1, "text": "use 8080", "human_ts": humanTS})
	got := mustOK(t, cs, "answers", obj{})
	eq(t, "event count", len(got["events"].([]any)), 1)
	has(t, "event", at(got, "events", 0).(obj), obj{"type": "answer_recorded", "item_id": q.ID, "handle": "q1", "revision": float64(1)})
	eq(t, "event record text", at(got, "events", 0, "record", "text"), "use 8080")
	again := mustOK(t, cs, "answers", obj{"since": got["next"]})
	eq(t, "answers after next", []any{again["events"], again["next"]}, []any{[]any{}, got["next"]})
	eq(t, "the decision on item", at(mustOK(t, cs, "item", obj{"id": q.ID}), "decision", "text"), "use 8080")
	mustFail(t, h.session(h.other), "item", obj{"id": q.ID}, "not_found") // another swarm's id
}

func TestAnswersWaitSecondsRange(t *testing.T) {
	h := newHarness(t)
	cs := h.session(h.node)
	for _, w := range []int{-1, 26, 1000} {
		mustFail(t, cs, "answers", obj{"wait_seconds": w}, "invalid")
	}
	mustOK(t, cs, "answers", obj{"wait_seconds": 0})
	mustFail(t, cs, "answers", obj{"since": -1}, "invalid")
}

// TestAnswersLongPollWakes: a waiting answers call returns as soon as Duet records an answer in
// its swarm, well before its wait ends.
func TestAnswersLongPollWakes(t *testing.T) {
	h := newHarness(t)
	q := h.seed(nodeP, question("q1"))
	cs, duet := h.session(h.node), h.session(h.duet)
	type reply struct {
		got     obj
		elapsed time.Duration
	}
	done := make(chan reply, 1)
	go func() {
		start := time.Now()
		res, err := call(cs, "answers", obj{"wait_seconds": 20})
		if err != nil {
			done <- reply{}
			return
		}
		m, _ := res.StructuredContent.(obj)
		done <- reply{m, time.Since(start)}
	}()
	time.Sleep(300 * time.Millisecond)
	mustOK(t, duet, "record_answer", obj{"item_id": q.ID, "revision": 1, "text": "use 8080", "human_ts": humanTS})
	select {
	case r := <-done:
		evs, _ := r.got["events"].([]any)
		if len(evs) != 1 || r.elapsed > 5*time.Second {
			t.Errorf("answers = %v after %v, want the answer within a second of the commit", r.got, r.elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting answers call was not woken by a commit")
	}
}

// TestAnswersWaitIsNotCutByToolDeadline: the call's own wait is added to the tool deadline, so a
// wait longer than the deadline still ends on its own timer, with an empty page.
func TestAnswersWaitIsNotCutByToolDeadline(t *testing.T) {
	h := newHarness(t, func(c *web.Config) { c.ToolDeadline = 300 * time.Millisecond })
	cs := h.session(h.node)
	start := time.Now()
	out := mustOK(t, cs, "answers", obj{"wait_seconds": 1})
	if el := time.Since(start); el < 900*time.Millisecond || el > 3*time.Second {
		t.Errorf("answers waited %v, want about 1s", el)
	}
	if evs, _ := out["events"].([]any); len(evs) != 0 {
		t.Errorf("events = %v, want none", evs)
	}
}

// blockingStore is a bus.Store whose every transaction blocks until the test ends, and ignores
// its context: the worst a stuck database does.
type blockingStore struct{ release chan struct{} }

func (s blockingStore) Tx(context.Context, func(bus.Tx) error) error {
	<-s.release
	return nil
}

func TestDeadlineBoundsToolCall(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	const deadline = 300 * time.Millisecond
	h := newHarnessOver(t, func(*db.DB) bus.Store { return blockingStore{release} }, func(c *web.Config) { c.ToolDeadline = deadline })
	cs := h.session(h.duet)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	res, err := cs.CallTool(ctx, callParams("item", obj{"id": "itm_x"}))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("the call did not return a result: %v (after %v)", err, elapsed)
	}
	if elapsed < deadline-50*time.Millisecond || elapsed > deadline+100*time.Millisecond {
		t.Errorf("the call returned after %v, want within the %v deadline plus 100ms", elapsed, deadline)
	}
	if e, _ := structured(t, res)["error"].(obj); !res.IsError || e["code"] != "internal" {
		t.Errorf("result = %v, want an internal tool error", res.StructuredContent)
	}
}
