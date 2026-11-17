package web

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// The input structs. A field without omitempty is required by the schema; every struct is closed
// (additionalProperties false), so an extra or forged argument is refused before any bus call.
// There is no actor_kind, source or by field anywhere: they come from the credential.

type idIn struct {
	ID string `json:"id" jsonschema:"the item's id"`
}

type sinceIn struct {
	Since int64 `json:"since,omitempty" jsonschema:"the cursor from a previous result's next, or 0 for the start"`
}

type swarmIn struct {
	Swarm string `json:"swarm,omitempty" jsonschema:"one swarm's name; empty for every swarm"`
}

type answerIn struct {
	ItemID        string `json:"item_id" jsonschema:"the question or blocker's id"`
	Revision      int    `json:"revision" jsonschema:"the revision the owner saw"`
	Text          string `json:"text" jsonschema:"the owner's words, 1 to 4096 bytes"`
	HumanTS       string `json:"human_ts" jsonschema:"when the owner said it, RFC 3339"`
	SourceMessage string `json:"source_message,omitempty" jsonschema:"the id of the owner's message, 1 to 64 characters from A-Z a-z 0-9 . _ : -"`
}

type approvalIn struct {
	ItemID   string `json:"item_id" jsonschema:"the approval's id"`
	Revision int    `json:"revision" jsonschema:"the revision the owner saw"`
	Decision string `json:"decision" jsonschema:"approve or reject, as the owner said it"`
	Text     string `json:"text" jsonschema:"the owner's own words, 1 to 4096 bytes"`
	HumanTS  string `json:"human_ts" jsonschema:"when the owner said it, RFC 3339"`
}

type ackIn struct {
	ItemID  string `json:"item_id" jsonschema:"the item's id"`
	SeenSeq int64  `json:"seen_seq" jsonschema:"the last_seq of the summary you read"`
}

type relayIn struct {
	ItemID   string `json:"item_id" jsonschema:"the item's id"`
	Revision int    `json:"revision" jsonschema:"the revision the owner saw"`
	Text     string `json:"text" jsonschema:"the owner's words, 1 to 4096 bytes"`
	HumanTS  string `json:"human_ts" jsonschema:"when the owner said it, RFC 3339"`
}

type replyIn struct {
	ThreadID string `json:"thread_id" jsonschema:"the item's id: its thread"`
	Body     string `json:"body" jsonschema:"the message, 1 to 65536 bytes"`
	Severity string `json:"severity" jsonschema:"low, normal, high or critical"`
	Urgency  string `json:"urgency" jsonschema:"page, interrupt-when-good or digest"`
}

type answersIn struct {
	Since       int64 `json:"since,omitempty" jsonschema:"the cursor from a previous result's next, or 0 for the start"`
	WaitSeconds int   `json:"wait_seconds,omitempty" jsonschema:"0 to 25: how long to wait for news when there is none"`
}

// draftIn is an item as a caller posts it. Severity and urgency are required, and urgency is
// never defaulted: only the poster knows which lane it needs.
type draftIn struct {
	Kind           string   `json:"kind,omitempty" jsonschema:"approval, question, blocker, finished, failed or notice"`
	Severity       string   `json:"severity" jsonschema:"low, normal, high or critical"`
	To             string   `json:"to,omitempty" jsonschema:"human, or a crew's swarm name for Duet; a node writes only to human"`
	Handle         string   `json:"handle,omitempty" jsonschema:"your own name for the item, up to 80 characters; reposting it revises the item"`
	Title          string   `json:"title,omitempty" jsonschema:"up to 200 characters"`
	Summary        string   `json:"summary,omitempty" jsonschema:"up to 1000 characters"`
	Detail         string   `json:"detail,omitempty" jsonschema:"up to 16384 bytes"`
	RequiredAction string   `json:"required_action,omitempty" jsonschema:"up to 1000 characters"`
	Repo           string   `json:"repo,omitempty" jsonschema:"owner/name"`
	Ref            string   `json:"ref,omitempty" jsonschema:"a PR number for a merge, or a tag"`
	BaseRef        string   `json:"base_ref,omitempty" jsonschema:"a branch name"`
	Target         string   `json:"target,omitempty" jsonschema:"up to 200 characters; a deploy's environment"`
	PinnedCommit   string   `json:"pinned_commit,omitempty" jsonschema:"a full 40-hex lowercase SHA"`
	PinnedTree     string   `json:"pinned_tree,omitempty" jsonschema:"a full 40-hex lowercase SHA"`
	Action         string   `json:"action,omitempty" jsonschema:"none, other or an action category; anything but none needs kind approval"`
	Link           string   `json:"link,omitempty" jsonschema:"one https URL on an allow-listed host, up to 2048 bytes"`
	RiskFlags      []string `json:"risk_flags,omitempty" jsonschema:"up to 8 flags matching a-z 0-9 -, up to 40 characters each"`
	Untrusted      bool     `json:"untrusted,omitempty" jsonschema:"true when the item quotes text you did not write"`
	Urgency        string   `json:"urgency" jsonschema:"page, interrupt-when-good or digest; Duet takes page or digest only"`
}

func (d draftIn) draft() bus.Draft {
	return bus.Draft{Kind: bus.Kind(d.Kind), Severity: bus.Severity(d.Severity), To: d.To, Handle: d.Handle, Title: d.Title, Summary: d.Summary,
		Detail: d.Detail, RequiredAction: d.RequiredAction, Repo: d.Repo, Ref: d.Ref, BaseRef: d.BaseRef, Target: d.Target,
		PinnedCommit: d.PinnedCommit, PinnedTree: d.PinnedTree, Action: bus.Action(d.Action), Link: d.Link, RiskFlags: d.RiskFlags,
		Untrusted: d.Untrusted, Urgency: bus.Urgency(d.Urgency)}
}

// itemOnly names the first field that only an item has: a reply carries none of them.
func (d draftIn) itemOnly() string {
	for _, f := range []struct {
		name string
		set  bool
	}{{"kind", d.Kind != ""}, {"to", d.To != ""}, {"handle", d.Handle != ""}, {"title", d.Title != ""}, {"summary", d.Summary != ""},
		{"detail", d.Detail != ""}, {"required_action", d.RequiredAction != ""}, {"repo", d.Repo != ""}, {"ref", d.Ref != ""},
		{"base_ref", d.BaseRef != ""}, {"target", d.Target != ""}, {"pinned_commit", d.PinnedCommit != ""}, {"pinned_tree", d.PinnedTree != ""},
		{"action", d.Action != ""}, {"link", d.Link != ""}, {"risk_flags", len(d.RiskFlags) > 0}, {"untrusted", d.Untrusted}} {
		if f.set {
			return f.name
		}
	}
	return ""
}

type duetPostIn struct {
	draftIn
	ThreadID string `json:"thread_id,omitempty" jsonschema:"set to reply in that item's thread: then only body, severity and urgency are used"`
	Body     string `json:"body,omitempty" jsonschema:"the reply, with thread_id; 1 to 65536 bytes"`
}

type statusIn struct {
	Swarm          string      `json:"swarm" jsonschema:"your own swarm's name"`
	Sessions       []sessionIn `json:"sessions" jsonschema:"at most 64 sessions"`
	WaitingOnHuman []string    `json:"waiting_on_human" jsonschema:"the handles waiting on the owner, at most 64"`
	MainHealth     string      `json:"main_health" jsonschema:"ok, degraded, failing or unknown"`
	Spend          float64     `json:"spend" jsonschema:"zero or more"`
	TS             int64       `json:"ts" jsonschema:"seconds since the epoch"`
}

type sessionIn struct {
	Name  string `json:"name" jsonschema:"the session's name"`
	State string `json:"state" jsonschema:"working, blocked, idle or done"`
}

// parseTS reads a time the caller sent. The error names the field, never the value.
func parseTS(field, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, &bus.Error{Code: bus.CodeInvalid, What: field, Valid: "an RFC 3339 time such as 2026-10-28T09:30:00Z", Fix: "correct " + field + " and call again"}
	}
	return t, nil
}

func invalid(what, valid string) *bus.Error {
	return &bus.Error{Code: bus.CodeInvalid, What: what, Valid: valid, Fix: "correct " + what + " and call again"}
}
