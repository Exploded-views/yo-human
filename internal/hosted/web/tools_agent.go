package web

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// nodeTools registers a node's six tools: post, answers, item, reply, status_push and relay. A node
// sees only its own swarm. escalate lands with Task 11b.
func (s *service) nodeTools(srv *mcp.Server, b *bus.Bus) {
	addTool(s, srv, "post", "Post an item to the owner, to human. Always set a handle: reposting the same handle with the same content changes nothing, and with new content revises the item and voids any earlier approval.",
		false, nil, func(ctx context.Context, p bus.Principal, in draftIn) (any, error) { return nodePost(ctx, b, p, in) })
	addTool(s, srv, "answers", "Follow your swarm's answers: the events after since, up to 100, in order, leaving out ones you caused. With wait_seconds it waits for news. Pass next back as since; the cursor moves only that way.",
		true, answersWait, func(ctx context.Context, p bus.Principal, in answersIn) (any, error) { return answers(ctx, b, p, in) })
	addTool(s, srv, "item", "Read one item of your swarm in full, with its thread and the decision for its current revision.",
		true, nil, func(ctx context.Context, p bus.Principal, in idIn) (any, error) { return itemView(ctx, b, p, in) })
	addTool(s, srv, "reply", "Reply in the thread of one of your swarm's items.",
		false, nil, func(ctx context.Context, p bus.Principal, in replyIn) (any, error) {
			return reply(ctx, b, p, in.ThreadID, in.Body, in.Severity, in.Urgency)
		})
	addTool(s, srv, "status_push", "Replace your swarm's status document.",
		false, nil, func(ctx context.Context, p bus.Principal, in statusIn) (any, error) { return statusPush(ctx, b, p, in) })
	addTool(s, srv, "relay", "Pass on the owner's words about an item of your swarm, at the revision he saw. A relay is stored as an agent's and never resolves anything.",
		false, nil, func(ctx context.Context, p bus.Principal, in relayIn) (any, error) { return relay(ctx, b, p, in) })
}

// answersWait is the part of an answers call's deadline that is its own wait, clamped to what the
// bus allows; an out-of-range wait is refused later, by answers.
func answersWait(in answersIn) time.Duration {
	return min(max(time.Duration(in.WaitSeconds)*time.Second, 0), bus.MaxWait)
}

// nodePost: a node writes only to the owner, so an omitted to is human.
func nodePost(ctx context.Context, b *bus.Bus, p bus.Principal, in draftIn) (any, error) {
	if in.To == "" {
		in.To = "human"
	}
	return postItem(ctx, b, p, in)
}

type answersOut struct {
	envelope
	Events []eventWire `json:"events"`
	Next   int64       `json:"next"`
}

func answers(ctx context.Context, b *bus.Bus, p bus.Principal, in answersIn) (any, error) {
	if in.Since < 0 {
		return nil, invalid("since", "0, or the next of a previous result")
	}
	if in.WaitSeconds < 0 || in.WaitSeconds > int(bus.MaxWait/time.Second) {
		return nil, invalid("wait_seconds", "0 to 25")
	}
	u, err := b.Answers(ctx, p, in.Since, time.Duration(in.WaitSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	return answersOut{v1(), eventsOf(u.Events), u.Next}, nil
}

func statusPush(ctx context.Context, b *bus.Bus, p bus.Principal, in statusIn) (any, error) {
	if in.Sessions == nil {
		in.Sessions = []sessionIn{}
	}
	if in.WaitingOnHuman == nil {
		in.WaitingOnHuman = []string{}
	}
	doc, _ := json.Marshal(in) // cannot fail: strings, a finite number and integers
	if err := b.PushStatus(ctx, p, doc); err != nil {
		return nil, err
	}
	return v1(), nil
}

type relayOut struct {
	envelope
	Relay relayWire `json:"relay"`
}

func relay(ctx context.Context, b *bus.Bus, p bus.Principal, in relayIn) (any, error) {
	at, err := parseTS("human_ts", in.HumanTS)
	if err != nil {
		return nil, err
	}
	r, err := b.RelayWords(ctx, p, bus.RelayInput{ItemID: in.ItemID, Revision: in.Revision, Text: in.Text, HumanTS: at})
	if err != nil {
		return nil, err
	}
	return relayOut{v1(), relayOf(r)}, nil
}
