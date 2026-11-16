package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

const humanTS = "2026-10-28T09:30:00Z"

func TestDuetRecordApprovalReturnsLinkNeverResolves(t *testing.T) {
	h := newHarness(t)
	it := h.seed(nodeP, mergeApproval("m7"))
	out := mustOK(t, h.session(h.duet), "record_approval", obj{"item_id": it.ID, "revision": it.Revision, "decision": "approve",
		"text": "yes, merge it", "human_ts": humanTS})
	eq(t, "result", out, obj{"v": float64(1), "resolved": false, "approval_url": "https://approve.example.com/approve/" + it.ID + "?rev=1"})
	view, err := h.b.Item(ctx, duetP, it.ID)
	eq(t, "item state, decision and error", []any{view.Item.State, view.Decision, err}, []any{bus.StateOpen, (*bus.Record)(nil), error(nil)})
	var relays []bus.Relay
	err = h.d.Tx(ctx, func(x bus.Tx) (err error) { relays, err = x.Relays(it.ID); return err })
	eq(t, "relays", err, error(nil))
	eq(t, "relay count", len(relays), 1)
	r := relays[0]
	eq(t, "relay", []any{r.Source, r.Text, r.Revision}, []any{"duet", "yes, merge it", 1})
	eq(t, "decisions stored", h.counts().Decisions, 0)
}

func secretsApproval(at time.Time) bus.Item {
	d := mergeApproval("raw")
	d.Action = bus.ActionSecrets
	return bus.Item{Draft: d, ID: "itm_raw", Swarm: "crew", From: "ci-1", FromKind: bus.PrincipalNode, Revision: 1,
		State: bus.StateOpen, ContentHash: "v1:x", Created: at, Updated: at}
}

// TestToolErrorShape: a bus refusal is a tool result with isError and the v 1 error object, and
// `winner` rides only on already_decided.
func TestToolErrorShape(t *testing.T) {
	h := newHarness(t)
	cs := h.session(h.duet)
	approval, q := h.seed(nodeP, mergeApproval("m7")), h.seed(nodeP, question("q1"))
	raw := secretsApproval(time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC))
	eq(t, "inserting a raw item", h.d.Tx(ctx, func(x bus.Tx) error { return x.InsertItem(raw) }), error(nil))
	approve := func(id string, rev int, decision string) obj {
		return obj{"item_id": id, "revision": rev, "decision": decision, "text": "yes", "human_ts": humanTS}
	}
	for _, c := range []struct {
		name, tool string
		args       obj
		code       string
	}{
		{"an excluded action", "record_approval", approve(raw.ID, 1, "approve"), "excluded_action"},
		{"a question", "record_approval", approve(q.ID, 1, "approve"), "not_approvable"},
		{"a stale revision", "record_approval", approve(approval.ID, 9, "approve"), "stale_revision"},
		{"an unknown decision", "record_approval", approve(approval.ID, 1, "maybe"), "invalid"},
		{"an unknown item", "item", obj{"id": "itm_nope"}, "not_found"},
		{"a bad swarm name", "swarm_status", obj{"swarm": "Not A Swarm"}, "invalid"},
		{"a bad time", "record_answer", obj{"item_id": q.ID, "revision": 1, "text": "8080", "human_ts": "yesterday"}, "invalid"},
	} {
		e := mustFail(t, cs, c.tool, c.args, c.code)
		_, hasWinner := e["winner"]
		eq(t, c.name+": winner is set", hasWinner, false)
		what := e["what"].(string)
		eq(t, c.name+": the error echoes the caller's value", strings.Contains(what, "yesterday") || strings.Contains(what, "Not A Swarm"), false)
	}
	mustOK(t, cs, "record_answer", obj{"item_id": q.ID, "revision": 1, "text": "use 8080", "human_ts": humanTS})
	e := mustFail(t, cs, "record_answer", obj{"item_id": q.ID, "revision": 1, "text": "use 9090", "human_ts": humanTS}, "already_decided")
	has(t, "winner", at(e, "winner").(obj), obj{"text": "use 8080", "actor_kind": "agent"})
}

func TestDuetReadTools(t *testing.T) {
	h := newHarness(t)
	q := h.seed(nodeP, question("q1"))
	mustOK(t, h.session(h.node), "status_push", obj{"swarm": "crew", "sessions": []obj{{"name": "ci", "state": "working"}}, "waiting_on_human": []string{"q1"},
		"main_health": "ok", "spend": 1.5, "ts": 1790000000})
	cs := h.session(h.duet)

	inbox := mustOK(t, cs, "inbox_for_claude", obj{})
	eq(t, "question group length", len(at(inbox, "groups", "question").([]any)), 1)
	sum := at(inbox, "groups", "question", 0).(obj)
	has(t, "summary", sum, obj{"id": q.ID, "handle": "q1", "from": "ci-1", "swarm": "crew", "severity": "normal", "title": "Which port?",
		"revision": float64(1), "untrusted": false, "kind": "question", "urgency": "digest"})
	for _, k := range []string{"summary", "created", "link", "last_seq"} {
		eq(t, "summary has "+k, sum[k] != nil, true)
	}
	eq(t, "inbox has interrupt, threads and next", []bool{inbox["interrupt"] != nil, inbox["threads"] != nil, inbox["next"] != nil}, []bool{true, true, true})
	for _, g := range []string{"approval", "blocker", "finished", "failed"} {
		eq(t, "empty group "+g, at(inbox, "groups", g), []any{})
	}
	mustOK(t, cs, "ack", obj{"item_id": q.ID, "seen_seq": sum["last_seq"]})
	eq(t, "question group after ack", at(mustOK(t, cs, "inbox_for_claude", obj{}), "groups", "question"), []any{})

	item := mustOK(t, cs, "item", obj{"id": q.ID})
	has(t, "item", at(item, "item").(obj), obj{"id": q.ID, "handle": "q1", "state": "open", "to": "human"})
	eq(t, "thread", item["thread"], []any{})
	st := mustOK(t, cs, "swarm_status", obj{})
	eq(t, "status", []any{at(st, "statuses", 0, "swarm"), at(st, "statuses", 0, "doc", "main_health")}, []any{"crew", "ok"})
	eq(t, "status for another swarm", mustOK(t, cs, "swarm_status", obj{"swarm": "other"})["statuses"], []any{})
}

func TestDuetWriteTools(t *testing.T) {
	h := newHarness(t)
	q := h.seed(nodeP, question("q1"))
	cs := h.session(h.duet)
	ans := mustOK(t, cs, "record_answer", obj{"item_id": q.ID, "revision": 1, "text": "use 8080", "human_ts": humanTS, "source_message": "msg-1"})
	has(t, "record", at(ans, "record").(obj), obj{"kind": "answer", "actor_kind": "agent", "source": "duet", "text": "use 8080", "source_message": "msg-1"})

	draft := obj{"kind": "notice", "severity": "normal", "to": "crew", "handle": "d1", "title": "Heads up", "action": "none", "urgency": "digest"}
	posted := mustOK(t, cs, "post", draft)
	eq(t, "outcome", posted["outcome"], "created")
	has(t, "item", at(posted, "item").(obj), obj{"swarm": "crew", "from": "duet", "to": "crew"})
	eq(t, "a repeat post", mustOK(t, cs, "post", draft)["outcome"], "unchanged")

	id := at(posted, "item", "id")
	reply := mustOK(t, cs, "post", obj{"thread_id": id, "body": "thanks", "severity": "low", "urgency": "digest"})
	has(t, "message", at(reply, "message").(obj), obj{"body": "thanks", "from": "duet", "thread_id": id, "severity": "low", "urgency": "digest"})
	mustFail(t, cs, "post", obj{"thread_id": id, "body": "x", "severity": "low", "urgency": "interrupt-when-good"}, "invalid")
	mustFail(t, cs, "post", obj{"thread_id": id, "severity": "low", "urgency": "digest"}, "invalid")                            // a reply needs a body
	mustFail(t, cs, "post", obj{"thread_id": id, "body": "x", "title": "t", "severity": "low", "urgency": "digest"}, "invalid") // and no item fields
}

// TestToolsRefuseMissingRequiredArguments: each tool with a required argument dropped is refused by
// the schema, in the v 1 error shape, before any bus call: nothing is stored.
func TestToolsRefuseMissingRequiredArguments(t *testing.T) {
	h := newHarness(t)
	q := h.seed(nodeP, question("q1"))
	before := h.counts()
	duetArgs := map[string]obj{
		"item":            {"id": q.ID},
		"record_answer":   {"item_id": q.ID, "revision": 1, "text": "a", "human_ts": humanTS},
		"record_approval": {"item_id": q.ID, "revision": 1, "decision": "approve", "text": "a", "human_ts": humanTS},
		"ack":             {"item_id": q.ID, "seen_seq": 1},
		"post":            {"kind": "notice", "severity": "low", "to": "crew", "title": "t", "action": "none", "urgency": "digest"},
	}
	nodeArgs := map[string]obj{
		"item":        {"id": q.ID},
		"post":        {"kind": "notice", "severity": "low", "title": "t", "action": "none", "urgency": "digest"},
		"reply":       {"thread_id": q.ID, "body": "b", "severity": "low", "urgency": "digest"},
		"relay":       {"item_id": q.ID, "revision": 1, "text": "a", "human_ts": humanTS},
		"status_push": {"swarm": "crew", "sessions": []obj{}, "waiting_on_human": []string{}, "main_health": "ok", "spend": 0, "ts": 1},
	}
	for token, tools := range map[string]map[string]obj{h.duet: duetArgs, h.node: nodeArgs} {
		cs := h.session(token)
		for tool, args := range tools {
			for missing := range args {
				if tool == "post" && missing != "severity" && missing != "urgency" {
					continue // optional in the schema; the bus names what it needs
				}
				partial := obj{}
				for k, v := range args {
					if k != missing {
						partial[k] = v
					}
				}
				mustFail(t, cs, tool, partial, "invalid")
			}
		}
	}
	eq(t, "rows after refused calls", h.counts(), before)
}
