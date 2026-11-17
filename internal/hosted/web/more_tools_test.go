package web_test

import (
	"testing"
)

// TestToolInputsTheSchemaCannotCheck: what the schema lets through and a tool still refuses.
func TestToolInputsTheSchemaCannotCheck(t *testing.T) {
	h := newHarness(t)
	q := h.seed(nodeP, question("q1"))
	duet, node := h.session(h.duet), h.session(h.node)
	mustFail(t, duet, "ack", obj{"item_id": "itm_nope", "seen_seq": 1}, "not_found")
	mustFail(t, duet, "record_approval", obj{"item_id": q.ID, "revision": 1, "decision": "approve", "text": "a", "human_ts": "yesterday"}, "invalid")
	mustFail(t, node, "relay", obj{"item_id": q.ID, "revision": 1, "text": "a", "human_ts": "yesterday"}, "invalid")
	mustFail(t, duet, "inbox_for_claude", obj{"since": "zero"}, "invalid")
	mustFail(t, node, "answers", obj{"since": -1}, "invalid")
	// A body with no thread_id is refused, naming body, even beside an otherwise good item.
	e := mustFail(t, duet, "post", obj{"kind": "notice", "severity": "low", "to": "crew", "handle": "b1", "title": "t", "action": "none", "urgency": "digest", "body": "x"}, "invalid")
	eq(t, "what", e["what"], "body")
}

// TestStatusPushAcceptsNullLists: a null list is an empty one.
func TestStatusPushAcceptsNullLists(t *testing.T) {
	h := newHarness(t)
	mustOK(t, h.session(h.node), "status_push", obj{"swarm": "crew", "sessions": nil, "waiting_on_human": nil, "main_health": "unknown", "spend": 0, "ts": 5})
	st := mustOK(t, h.session(h.duet), "swarm_status", obj{"swarm": "crew"})
	eq(t, "stored doc", []any{at(st, "statuses", 0, "doc", "sessions"), at(st, "statuses", 0, "doc", "waiting_on_human")}, []any{[]any{}, []any{}})
}

// TestThreadsAndMessagesOnTheWire: a reply shows in the item's thread, in the node's answers as a
// message_added event, and, from the node, in Duet's inbox threads.
func TestThreadsAndMessagesOnTheWire(t *testing.T) {
	h := newHarness(t)
	duet, node := h.session(h.duet), h.session(h.node)
	posted := mustOK(t, duet, "post", obj{"kind": "question", "severity": "normal", "to": "crew", "handle": "d1", "title": "Ready?", "action": "none", "urgency": "digest"})
	id := at(posted, "item", "id")
	mustOK(t, node, "reply", obj{"thread_id": id, "body": "yes", "severity": "low", "urgency": "digest"})
	view := mustOK(t, duet, "item", obj{"id": id})
	has(t, "thread message", at(view, "thread", 0).(obj), obj{"body": "yes", "from": "ci-1", "from_kind": "node"})
	eq(t, "inbox threads", len(at(mustOK(t, duet, "inbox_for_claude", obj{}), "threads").([]any)), 1)
	mustOK(t, duet, "post", obj{"thread_id": id, "body": "thanks", "severity": "low", "urgency": "digest"})
	got := mustOK(t, node, "answers", obj{})
	has(t, "message event", at(got, "events", 1).(obj), obj{"type": "message_added", "item_id": id})
	eq(t, "message event body", at(got, "events", 1, "message", "body"), "thanks")
}
