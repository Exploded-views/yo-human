package web

import (
	"encoding/json"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// The wire forms of the bus types. Every text field is data written by another agent or a person,
// kept verbatim. Times are RFC 3339 in UTC.

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

type itemWire struct {
	ID             string       `json:"id"`
	Handle         string       `json:"handle"`
	Kind           bus.Kind     `json:"kind"`
	Severity       bus.Severity `json:"severity"`
	To             string       `json:"to"`
	Swarm          string       `json:"swarm"`
	From           string       `json:"from"`
	FromKind       string       `json:"from_kind"`
	Revision       int          `json:"revision"`
	State          bus.State    `json:"state"`
	Title          string       `json:"title"`
	Summary        string       `json:"summary"`
	Detail         string       `json:"detail"`
	RequiredAction string       `json:"required_action"`
	Repo           string       `json:"repo"`
	Ref            string       `json:"ref"`
	BaseRef        string       `json:"base_ref"`
	Target         string       `json:"target"`
	PinnedCommit   string       `json:"pinned_commit"`
	PinnedTree     string       `json:"pinned_tree"`
	Action         bus.Action   `json:"action"`
	Link           string       `json:"link"`
	RiskFlags      []string     `json:"risk_flags"`
	Untrusted      bool         `json:"untrusted"`
	Urgency        bus.Urgency  `json:"urgency"`
	LastSeq        int64        `json:"last_seq"`
	Created        string       `json:"created"`
	Updated        string       `json:"updated"`
}

func itemOf(it bus.Item) itemWire {
	flags := it.RiskFlags
	if flags == nil {
		flags = []string{}
	}
	return itemWire{ID: it.ID, Handle: it.Handle, Kind: it.Kind, Severity: it.Severity, To: it.To, Swarm: it.Swarm, From: it.From,
		FromKind: string(it.FromKind), Revision: it.Revision, State: it.State, Title: it.Title, Summary: it.Summary, Detail: it.Detail,
		RequiredAction: it.RequiredAction, Repo: it.Repo, Ref: it.Ref, BaseRef: it.BaseRef, Target: it.Target, PinnedCommit: it.PinnedCommit,
		PinnedTree: it.PinnedTree, Action: it.Action, Link: it.Link, RiskFlags: flags, Untrusted: it.Untrusted, Urgency: it.Urgency,
		LastSeq: it.LastSeq, Created: ts(it.Created), Updated: ts(it.Updated)}
}

// summaryWire is one line of the inbox. LastSeq is the value ack takes back as seen_seq.
type summaryWire struct {
	ID        string       `json:"id"`
	Handle    string       `json:"handle"`
	From      string       `json:"from"`
	Swarm     string       `json:"swarm"`
	Severity  bus.Severity `json:"severity"`
	Title     string       `json:"title"`
	Summary   string       `json:"summary"`
	Revision  int          `json:"revision"`
	Created   string       `json:"created"`
	Link      string       `json:"link"`
	Untrusted bool         `json:"untrusted"`
	Kind      bus.Kind     `json:"kind"`
	Urgency   bus.Urgency  `json:"urgency"`
	LastSeq   int64        `json:"last_seq"`
}

func summariesOf(in []bus.Summary) []summaryWire {
	out := make([]summaryWire, 0, len(in))
	for _, s := range in {
		out = append(out, summaryWire{ID: s.ID, Handle: s.Handle, From: s.From, Swarm: s.Swarm, Severity: s.Severity, Title: s.Title,
			Summary: s.Summary, Revision: s.Revision, Created: ts(s.Created), Link: s.Link, Untrusted: s.Untrusted, Kind: s.Kind,
			Urgency: s.Urgency, LastSeq: s.LastSeq})
	}
	return out
}

type messageWire struct {
	ID       string       `json:"id"`
	ThreadID string       `json:"thread_id"`
	From     string       `json:"from"`
	FromKind string       `json:"from_kind"`
	Body     string       `json:"body"`
	Severity bus.Severity `json:"severity"`
	Urgency  bus.Urgency  `json:"urgency"`
	Created  string       `json:"created"`
}

func messageOf(m bus.Message) messageWire {
	return messageWire{ID: m.ID, ThreadID: m.ThreadID, From: m.From, FromKind: string(m.FromKind), Body: m.Body, Severity: m.Severity,
		Urgency: m.Urgency, Created: ts(m.Created)}
}

func messagesOf(in []bus.Message) []messageWire {
	out := make([]messageWire, 0, len(in))
	for _, m := range in {
		out = append(out, messageOf(m))
	}
	return out
}

type recordWire struct {
	ItemID        string         `json:"item_id"`
	Revision      int            `json:"revision"`
	Kind          bus.RecordKind `json:"kind"`
	Decision      bus.Decision   `json:"decision"`
	ActorKind     bus.ActorKind  `json:"actor_kind"`
	Text          string         `json:"text"`
	Source        string         `json:"source"`
	By            string         `json:"by"`
	SourceMessage string         `json:"source_message"`
	HumanTS       string         `json:"human_ts"`
	Recorded      string         `json:"recorded"`
}

func recordOf(r bus.Record) recordWire {
	return recordWire{ItemID: r.ItemID, Revision: r.Revision, Kind: r.Kind, Decision: r.Decision, ActorKind: r.ActorKind, Text: r.Text,
		Source: r.Source, By: r.By, SourceMessage: r.SourceMessage, HumanTS: ts(r.HumanTS), Recorded: ts(r.Recorded)}
}

type relayWire struct {
	ID       string `json:"id"`
	ItemID   string `json:"item_id"`
	Revision int    `json:"revision"`
	Text     string `json:"text"`
	Source   string `json:"source"`
	By       string `json:"by"`
	HumanTS  string `json:"human_ts"`
	Recorded string `json:"recorded"`
}

func relayOf(r bus.Relay) relayWire {
	return relayWire{ID: r.ID, ItemID: r.ItemID, Revision: r.Revision, Text: r.Text, Source: r.Source, By: r.By,
		HumanTS: ts(r.HumanTS), Recorded: ts(r.Recorded)}
}

type eventWire struct {
	Seq      int64         `json:"seq"`
	Type     bus.EventType `json:"type"`
	ItemID   string        `json:"item_id"`
	Handle   string        `json:"handle"`
	Revision int           `json:"revision"`
	Record   *recordWire   `json:"record,omitempty"`
	Message  *messageWire  `json:"message,omitempty"`
}

func eventsOf(in []bus.Update) []eventWire {
	out := make([]eventWire, 0, len(in))
	for _, u := range in {
		e := eventWire{Seq: u.Seq, Type: u.Type, ItemID: u.ItemID, Handle: u.Handle, Revision: u.Revision}
		if u.Record != nil {
			r := recordOf(*u.Record)
			e.Record = &r
		}
		if u.Message != nil {
			m := messageOf(*u.Message)
			e.Message = &m
		}
		out = append(out, e)
	}
	return out
}

type statusWire struct {
	Swarm   string          `json:"swarm"`
	Doc     json.RawMessage `json:"doc"`
	Updated string          `json:"updated"`
}

func statusesOf(in []bus.StatusDoc) []statusWire {
	out := make([]statusWire, 0, len(in))
	for _, d := range in {
		out = append(out, statusWire{Swarm: d.Swarm, Doc: d.Doc, Updated: ts(d.Updated)})
	}
	return out
}

// deliveryWire says the owner's phone may not be hearing from the bus. It is present only then.
type deliveryWire struct {
	Broken  bool   `json:"broken"`
	Since   string `json:"since"`
	Status  int    `json:"status"`
	Failed  int    `json:"failed"`
	Pending int    `json:"pending"`
}

func deliveryOf(d *bus.DeliveryState) *deliveryWire { return nil }
