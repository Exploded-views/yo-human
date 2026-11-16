package bus_test

import (
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

// TestRelayApprovalStoresWordsNeverResolves: Duet's record_approval stores the owner's quoted
// words as an agent relay and nothing else. The item stays open at its revision, no decision is
// stored, and the owner's passkey approval still succeeds afterwards.
func TestRelayApprovalStoresWordsNeverResolves(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	before := len(e.events(t))
	r, err := e.b.RelayApproval(ctx, duet, approveIn(it))
	if err != nil {
		t.Fatalf("RelayApproval: %v", err)
	}
	same(t, "relay", [6]any{r.ItemID, r.Revision, r.Text, r.Source, r.By, r.HumanTS.Equal(t0)},
		[6]any{it.ID, it.Revision, "yes, merge it", "duet", "duet", true})
	same(t, "stored relays", e.relays(t, it.ID), []bus.Relay{r})
	evs := e.events(t)
	same(t, "events", types(evs[before:]), []bus.EventType{bus.EventWordsRelayed})
	_, found := e.effective(t, it.ID, it.Revision)
	same(t, "a decision was stored", found, false)
	got, _ := e.stored(t, it.ID)
	same(t, "item state and revision", [2]any{got.State, got.Revision}, [2]any{bus.StateOpen, it.Revision})
	if _, err := e.b.RecordApproval(ctx, passkey, approveIn(it)); err != nil {
		t.Errorf("a passkey approval after the relay: %v", err)
	}
}

func TestRelayApprovalRefusals(t *testing.T) {
	e := newEnv(t)
	a, q := e.approvalItem(t), e.questionItem(t)
	n := e.post(t, node("ci-1", "crew"), notice("n1")).Item
	at := func(it bus.Item, f func(*bus.ApprovalInput)) bus.ApprovalInput {
		in := approveIn(it)
		f(&in)
		return in
	}
	for _, c := range []struct {
		name string
		p    bus.Principal
		in   bus.ApprovalInput
		want bus.Code
	}{
		{"a node", node("ci-1", "crew"), approveIn(a), bus.CodeForbidden},
		{"a passkey", passkey, approveIn(a), bus.CodeForbidden},
		{"the desk", deskP, approveIn(a), bus.CodeForbidden},
		{"an unknown item", duet, at(a, func(in *bus.ApprovalInput) { in.ItemID = "itm_nope" }), bus.CodeNotFound},
		{"a question", duet, approveIn(q), bus.CodeNotApprovable},
		{"a notice", duet, approveIn(n), bus.CodeNotApprovable},
		{"a stale revision", duet, at(a, func(in *bus.ApprovalInput) { in.Revision = 9 }), bus.CodeStaleRevision},
		{"no decision", duet, at(a, func(in *bus.ApprovalInput) { in.Decision = "" }), bus.CodeInvalid},
		{"an unknown decision", duet, at(a, func(in *bus.ApprovalInput) { in.Decision = "maybe" }), bus.CodeInvalid},
		{"empty text", duet, at(a, func(in *bus.ApprovalInput) { in.Text = "  " }), bus.CodeInvalid},
		{"text over 4 KiB", duet, at(a, func(in *bus.ApprovalInput) { in.Text = strings.Repeat("x", 4097) }), bus.CodeInvalid},
	} {
		s := e.snap(t, a.ID)
		_, err := e.b.RelayApproval(ctx, c.p, c.in)
		wantCode(t, err, c.want)
		e.unchanged(t, c.name, s)
	}
	if got := len(e.relays(t, a.ID)); got != 0 {
		t.Errorf("relays stored after refusals = %d, want 0", got)
	}
}

// TestRelayApprovalPolicy: an item the policy no longer puts behind a passkey (standing, denied or
// unlisted) is refused with the code Limits.Approvable gives, and nothing is stored.
func TestRelayApprovalPolicy(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	merge := func(m policy.Mode) policy.Policy {
		return policy.Policy{V: 1, Rules: []policy.Rule{{Action: "merge", Repo: "o/r", Ref: "main", Mode: m}}}
	}
	for _, c := range []struct {
		name string
		pol  policy.Policy
		want bus.Code
	}{
		{"standing", merge(policy.Standing), bus.CodeStanding},
		{"denied", merge(policy.Deny), bus.CodeExcludedAction},
		{"unlisted", policy.Policy{V: 1}, bus.CodeExcludedAction},
	} {
		l := e.l
		l.Policy = c.pol
		s := e.snap(t, it.ID)
		_, err := bus.New(e.d, e.now, l).RelayApproval(ctx, duet, approveIn(it))
		wantCode(t, err, c.want)
		e.unchanged(t, c.name, s)
	}
}

// TestRelayApprovalExcludedAction: an approval row for secrets (which the bus would never have
// accepted) is excluded whoever relays.
func TestRelayApprovalExcludedAction(t *testing.T) {
	e := newEnv(t)
	d := mergeApproval("raw")
	d.Action = bus.ActionSecrets
	it := bus.Item{Draft: d, ID: "itm_raw", Swarm: "crew", From: "ci-1", FromKind: bus.PrincipalNode, Revision: 1,
		State: bus.StateOpen, ContentHash: "v1:x", Created: t0, Updated: t0}
	if err := e.d.Tx(ctx, func(x bus.Tx) error { return x.InsertItem(it) }); err != nil {
		t.Fatal(err)
	}
	s := e.snap(t, it.ID)
	_, err := e.b.RelayApproval(ctx, duet, approveIn(it))
	wantCode(t, err, bus.CodeExcludedAction)
	e.unchanged(t, "secrets", s)
}

// TestRelayApprovalWalledSwarmIsGoOrHold: in a walled swarm the text is exactly go sl:<n> for
// approve and hold sl:<n> for reject, so the words cannot contradict the decision.
func TestRelayApprovalWalledSwarmIsGoOrHold(t *testing.T) {
	e := newEnvWalled(t, "client")
	d := mergeApproval("ob-7")
	d.Repo, d.Ref, d.Target = "cn:alpha", "pr:7", "cn:alpha"
	it := bus.Item{Draft: d, ID: "itm_wall", Swarm: "client", From: "ci-1", FromKind: bus.PrincipalNode, Revision: 1,
		State: bus.StateOpen, ContentHash: "v1:x", Created: t0, Updated: t0}
	if err := e.d.Tx(ctx, func(x bus.Tx) error { return x.InsertItem(it) }); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		decision bus.Decision
		text     string
		ok       bool
	}{
		{bus.DecisionApprove, "go sl:12", true},
		{bus.DecisionReject, "hold sl:12", true},
		{bus.DecisionApprove, "hold sl:12", false},
		{bus.DecisionReject, "go sl:12", false},
		{bus.DecisionApprove, "yes, merge it", false},
		{bus.DecisionApprove, "go sl:012", false},
	} {
		in := approveIn(it)
		in.Decision, in.Text = c.decision, c.text
		_, err := e.b.RelayApproval(ctx, duet, in)
		if c.ok && err != nil {
			t.Errorf("%s %q: %v, want it stored", c.decision, c.text, err)
		}
		if !c.ok {
			wantCode(t, err, bus.CodeInvalid)
		}
	}
	if got := len(e.relays(t, it.ID)); got != 2 {
		t.Errorf("relays stored = %d, want 2", got)
	}
}
