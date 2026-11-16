package web

import (
	"context"
	"net/url"
	"strconv"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// duetTools registers Duet's seven tools. Duet sees every swarm; what it records is stored as an
// agent's, because the actor kind comes from the credential alone.
func (s *service) duetTools(srv *mcp.Server, b *bus.Bus) {
	addTool(s, srv, "inbox_for_claude", "Read what needs the owner: items grouped by kind (approval, question, blocker, finished, failed), the interrupt group Duet chooses when to raise, and threads with new replies. Pass next back as since.",
		true, nil, func(ctx context.Context, p bus.Principal, in sinceIn) (any, error) { return inbox(ctx, b, p, in) })
	addTool(s, srv, "item", "Read one item in full, with its thread and the decision for its current revision.",
		true, nil, func(ctx context.Context, p bus.Principal, in idIn) (any, error) { return itemView(ctx, b, p, in) })
	addTool(s, srv, "swarm_status", "Read the latest status document of one swarm, or of every swarm.",
		true, nil, func(ctx context.Context, p bus.Principal, in swarmIn) (any, error) { return swarmStatus(ctx, b, p, in) })
	addTool(s, srv, "record_answer", "Record the owner's answer to a question or blocker, in the owner's words, at the revision the owner saw. The first answer for a revision wins.",
		false, nil, func(ctx context.Context, p bus.Principal, in answerIn) (any, error) {
			return recordAnswer(ctx, b, p, in)
		})
	addTool(s, srv, "record_approval", "Record the owner's words about an approval. This never resolves it: send the returned approval_url to the owner, who approves on that page with his passkey.",
		false, nil, func(ctx context.Context, p bus.Principal, in approvalIn) (any, error) {
			return s.recordApproval(ctx, b, p, in)
		})
	addTool(s, srv, "post", "Post an item to the owner (to human) or to a crew's swarm, or, with thread_id, reply in an item's thread. Urgency is required: page or digest.",
		false, nil, func(ctx context.Context, p bus.Principal, in duetPostIn) (any, error) { return duetPost(ctx, b, p, in) })
	addTool(s, srv, "ack", "Hide an item from the inbox until it has news. Pass the last_seq of the summary you read as seen_seq.",
		false, nil, func(ctx context.Context, p bus.Principal, in ackIn) (any, error) { return ack(ctx, b, p, in) })
}

type inboxOut struct {
	envelope
	Groups    map[bus.Kind][]summaryWire `json:"groups"`
	Interrupt []summaryWire              `json:"interrupt"`
	Threads   []summaryWire              `json:"threads"`
	Next      int64                      `json:"next"`
}

func inbox(ctx context.Context, b *bus.Bus, p bus.Principal, in sinceIn) (any, error) {
	r, err := b.Inbox(ctx, p, in.Since)
	if err != nil {
		return nil, err
	}
	groups := map[bus.Kind][]summaryWire{}
	for k, g := range r.Groups {
		groups[k] = summariesOf(g)
	}
	return inboxOut{v1(), groups, summariesOf(r.Interrupt), summariesOf(r.Threads), r.Next}, nil
}

type itemViewOut struct {
	envelope
	Item     itemWire      `json:"item"`
	Thread   []messageWire `json:"thread"`
	Decision *recordWire   `json:"decision,omitempty"`
}

func itemView(ctx context.Context, b *bus.Bus, p bus.Principal, in idIn) (any, error) {
	v, err := b.Item(ctx, p, in.ID)
	if err != nil {
		return nil, err
	}
	out := itemViewOut{envelope: v1(), Item: itemOf(v.Item), Thread: messagesOf(v.Messages)}
	if v.Decision != nil {
		r := recordOf(*v.Decision)
		out.Decision = &r
	}
	return out, nil
}

type statusesOut struct {
	envelope
	Statuses []statusWire `json:"statuses"`
}

func swarmStatus(ctx context.Context, b *bus.Bus, p bus.Principal, in swarmIn) (any, error) {
	docs, err := b.SwarmStatus(ctx, p, in.Swarm)
	if err != nil {
		return nil, err
	}
	return statusesOut{v1(), statusesOf(docs)}, nil
}

type recordOut struct {
	envelope
	Record recordWire `json:"record"`
}

func recordAnswer(ctx context.Context, b *bus.Bus, p bus.Principal, in answerIn) (any, error) {
	at, err := parseTS("human_ts", in.HumanTS)
	if err != nil {
		return nil, err
	}
	r, err := b.RecordAnswer(ctx, p, bus.AnswerInput{ItemID: in.ItemID, Revision: in.Revision, Text: in.Text, HumanTS: at, SourceMessage: in.SourceMessage})
	if err != nil {
		return nil, err
	}
	return recordOut{v1(), recordOf(r)}, nil
}

type approvalOut struct {
	envelope
	Resolved    bool   `json:"resolved"`
	ApprovalURL string `json:"approval_url"`
}

// recordApproval stores the owner's words and hands back the approval page's link. It never
// resolves: nothing Duet sends changes the item's state.
func (s *service) recordApproval(ctx context.Context, b *bus.Bus, p bus.Principal, in approvalIn) (any, error) {
	at, err := parseTS("human_ts", in.HumanTS)
	if err != nil {
		return nil, err
	}
	r, err := b.RelayApproval(ctx, p, bus.ApprovalInput{ItemID: in.ItemID, Revision: in.Revision, Decision: bus.Decision(in.Decision), Text: in.Text, HumanTS: at})
	if err != nil {
		return nil, err
	}
	return approvalOut{v1(), false, s.cfg.ApproveOrigin + "/approve/" + url.PathEscape(r.ItemID) + "?rev=" + strconv.Itoa(r.Revision)}, nil
}

type postOut struct {
	envelope
	Item    itemWire    `json:"item"`
	Outcome bus.Outcome `json:"outcome"`
}

type messageOut struct {
	envelope
	Message messageWire `json:"message"`
}

func duetPost(ctx context.Context, b *bus.Bus, p bus.Principal, in duetPostIn) (any, error) {
	if in.ThreadID == "" {
		if in.Body != "" {
			return nil, invalid("body", "empty unless thread_id is set")
		}
		return postItem(ctx, b, p, in.draftIn)
	}
	if f := in.itemOnly(); f != "" {
		return nil, invalid(f, "empty on a reply: a reply is body, severity and urgency in an item's thread")
	}
	return reply(ctx, b, p, in.ThreadID, in.Body, in.Severity, in.Urgency)
}

func postItem(ctx context.Context, b *bus.Bus, p bus.Principal, in draftIn) (any, error) {
	r, err := b.Post(ctx, p, in.draft())
	if err != nil {
		return nil, err
	}
	return postOut{v1(), itemOf(r.Item), r.Outcome}, nil
}

func reply(ctx context.Context, b *bus.Bus, p bus.Principal, thread, body, severity, urgency string) (any, error) {
	m, err := b.Reply(ctx, p, thread, body, bus.Severity(severity), bus.Urgency(urgency))
	if err != nil {
		return nil, err
	}
	return messageOut{v1(), messageOf(m)}, nil
}

func ack(ctx context.Context, b *bus.Bus, p bus.Principal, in ackIn) (any, error) {
	if err := b.Ack(ctx, p, in.ItemID, in.SeenSeq); err != nil {
		return nil, err
	}
	return v1(), nil
}
