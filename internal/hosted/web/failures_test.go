package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/web"
)

const causeMarker = "SQLITE-CAUSE-FAKE-7"

// failingStore is a bus.Store whose transactions fail with a cause that must never reach a caller,
// or panic when panics is set.
type failingStore struct{ panics bool }

func (s failingStore) Tx(context.Context, func(bus.Tx) error) error {
	if s.panics {
		panic("store panic " + causeMarker)
	}
	return errors.New("disk I/O " + causeMarker)
}

// everyToolArgs is a valid call of every tool, for both principals.
func everyToolArgs() (duet, node map[string]obj) {
	draft := obj{"kind": "notice", "severity": "low", "to": "crew", "handle": "n1", "title": "t", "action": "none", "urgency": "digest"}
	duet = map[string]obj{
		"inbox_for_claude": {}, "item": {"id": "itm_x"}, "swarm_status": {}, "post": draft, "ack": {"item_id": "itm_x", "seen_seq": 1},
		"record_answer":   {"item_id": "itm_x", "revision": 1, "text": "a", "human_ts": humanTS},
		"record_approval": {"item_id": "itm_x", "revision": 1, "decision": "approve", "text": "a", "human_ts": humanTS},
	}
	node = map[string]obj{
		"post": forge(draft, obj{"to": "human"}), "answers": {}, "item": {"id": "itm_x"},
		"reply":       {"thread_id": "itm_x", "body": "b", "severity": "low", "urgency": "digest"},
		"relay":       {"item_id": "itm_x", "revision": 1, "text": "a", "human_ts": humanTS},
		"status_push": {"swarm": "crew", "sessions": []obj{}, "waiting_on_human": []string{}, "main_health": "ok", "spend": 0, "ts": 1},
	}
	return duet, node
}

// TestInternalErrorCarriesNoCause: a store failure, or a panic under a tool, is an internal error
// that says nothing of why; the cause goes to the operator's log only.
func TestInternalErrorCarriesNoCause(t *testing.T) {
	for _, panics := range []bool{false, true} {
		var logs bytes.Buffer
		h := newHarnessOver(t, func(*db.DB) bus.Store { return failingStore{panics} },
			func(c *web.Config) { c.Log = slog.New(slog.NewTextHandler(&logs, nil)) })
		duet, node := everyToolArgs()
		for token, tools := range map[string]map[string]obj{h.duet: duet, h.node: node} {
			cs := h.session(token)
			for tool, args := range tools {
				e := mustFail(t, cs, tool, args, "internal")
				raw, _ := json.Marshal(e)
				eq(t, tool+" error body carries the cause", strings.Contains(string(raw), causeMarker), false)
				eq(t, tool+" what", e["what"], "the bus")
			}
		}
		eq(t, "the log carries the cause", strings.Contains(logs.String(), causeMarker), true)
	}
}

// TestVerifierFailureIsA500ThatNamesNothing: a failing credential lookup is not a 401 (the token
// may be good) and its error text stays out of the response.
func TestVerifierFailureIsA500ThatNamesNothing(t *testing.T) {
	h := newHarness(t)
	h.tokens.err = errors.New("lookup " + causeMarker)
	for _, tok := range []string{h.node, h.duet} {
		resp, body := h.do(h.h.MCP, mcpPost(tok))
		eq(t, "status", resp.StatusCode, http.StatusInternalServerError)
		eq(t, "the body names the cause", strings.Contains(body, causeMarker), false)
	}
}
