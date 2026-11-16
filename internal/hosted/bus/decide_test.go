package bus_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

var (
	passkey = bus.Principal{Kind: bus.PrincipalPasskey, Name: "phone"}
	deskP   = bus.Principal{Kind: bus.PrincipalDesk, Name: "desk"}
	ctx     = context.Background()
)

func askQuestion(handle string) bus.Draft {
	return bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human", Handle: handle, Title: "Which port?",
		Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
}

func (e *env) approvalItem(t *testing.T) bus.Item {
	t.Helper()
	return e.post(t, node("ci-1", "crew"), mergeApproval("m7")).Item
}

func (e *env) questionItem(t *testing.T) bus.Item {
	t.Helper()
	return e.post(t, node("ci-1", "crew"), askQuestion("q1")).Item
}

func approveIn(it bus.Item) bus.ApprovalInput {
	return bus.ApprovalInput{ItemID: it.ID, Revision: it.Revision, Decision: bus.DecisionApprove, Text: "yes, merge it", HumanTS: t0}
}

func answerIn(it bus.Item) bus.AnswerInput {
	return bus.AnswerInput{ItemID: it.ID, Revision: it.Revision, Text: "use 8080", HumanTS: t0}
}

// snap is everything a refused call must leave alone: the row counts and the item itself.
type snap struct {
	c  db.Counts
	it bus.Item
}

func (e *env) snap(t *testing.T, id string) snap {
	t.Helper()
	c, err := e.d.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := e.stored(t, id)
	return snap{c, it}
}

func (e *env) unchanged(t *testing.T, what string, s snap) {
	t.Helper()
	same(t, what+": stored state", e.snap(t, s.it.ID), s)
}

func (e *env) effective(t *testing.T, id string, rev int) (r bus.Record, found bool) {
	t.Helper()
	err := e.d.Tx(ctx, func(x bus.Tx) (err error) {
		r, found, err = x.Decision(id, rev)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, found
}

func TestApprovalForOldRevisionIsRejected(t *testing.T) {
	e := newEnv(t)
	it := e.approvalItem(t)
	d := mergeApproval("m7")
	d.Title = "Merge PR 7 (rebased)"
	if r := e.post(t, node("ci-1", "crew"), d); r.Item.Revision != 2 {
		t.Fatalf("repost revision = %d", r.Item.Revision)
	}
	s := e.snap(t, it.ID)
	_, err := e.b.RecordApproval(ctx, passkey, approveIn(it)) // revision 1
	be := errWithCode(t, err, bus.CodeStaleRevision)
	if !strings.Contains(be.Valid, "2") {
		t.Errorf("Valid = %q, want the current revision, 2", be.Valid)
	}
	e.unchanged(t, "stale approval", s)
	q := e.questionItem(t)
	s = e.snap(t, q.ID)
	in := answerIn(q)
	in.Revision = 5
	_, err = e.b.RecordAnswer(ctx, duet, in)
	wantCode(t, err, bus.CodeStaleRevision)
	e.unchanged(t, "stale answer", s)
}

func TestNonPasskeyActionRejectedForEveryone(t *testing.T) {
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
		{"standing target", merge(policy.Standing), bus.CodeStanding},
		{"unlisted target", policy.Policy{V: 1}, bus.CodeExcludedAction},
		{"denied target", merge(policy.Deny), bus.CodeExcludedAction},
	} {
		l := e.l
		l.Policy = c.pol
		b := bus.New(e.d, e.now, l)
		s := e.snap(t, it.ID)
		for _, p := range []bus.Principal{duet, node("ci-1", "crew")} {
			_, err := b.RecordApproval(ctx, p, approveIn(it))
			wantCode(t, err, bus.CodeForbidden)
		}
		_, err := b.RecordApproval(ctx, passkey, approveIn(it))
		wantCode(t, err, c.want)
		e.unchanged(t, c.name, s)
	}
}

// An item the bus would never have accepted (a secrets or money action) is excluded for every
// principal that may decide, answer or approval alike.
func TestExcludedActionNeverDecidable(t *testing.T) {
	e := newEnv(t)
	for i, c := range []struct {
		kind   bus.Kind
		action bus.Action
	}{{bus.KindQuestion, bus.ActionMoney}, {bus.KindApproval, bus.ActionSecrets}} {
		d := askQuestion("raw")
		d.Kind, d.Action, d.Handle = c.kind, c.action, "raw"+string(rune('a'+i))
		it := bus.Item{Draft: d, ID: "itm_raw" + string(rune('a'+i)), Swarm: "crew", From: "ci-1", FromKind: bus.PrincipalNode, Revision: 1,
			State: bus.StateOpen, ContentHash: "v1:x", Created: t0, Updated: t0}
		if err := e.d.Tx(ctx, func(x bus.Tx) error { return x.InsertItem(it) }); err != nil {
			t.Fatal(err)
		}
		s := e.snap(t, it.ID)
		var err error
		if c.kind == bus.KindQuestion {
			_, err = e.b.RecordAnswer(ctx, duet, answerIn(it))
		} else {
			_, err = e.b.RecordApproval(ctx, passkey, approveIn(it))
		}
		wantCode(t, err, bus.CodeExcludedAction)
		e.unchanged(t, string(c.action), s)
	}
}

func TestApprovalOnQuestionIsNotApprovable(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	n := e.post(t, node("ci-1", "crew"), notice("n1")).Item
	for _, c := range []struct {
		name string
		it   bus.Item
		call func() error
	}{
		{"approval on a question", q, func() error { _, err := e.b.RecordApproval(ctx, passkey, approveIn(q)); return err }},
		{"answer on an approval", a, func() error { _, err := e.b.RecordAnswer(ctx, duet, answerIn(a)); return err }},
		{"answer on a notice", n, func() error { _, err := e.b.RecordAnswer(ctx, duet, answerIn(n)); return err }},
	} {
		s := e.snap(t, c.it.ID)
		wantCode(t, c.call(), bus.CodeNotApprovable)
		e.unchanged(t, c.name, s)
	}
}

func TestMissingItemIsNotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.b.RecordApproval(ctx, passkey, bus.ApprovalInput{ItemID: "itm_nope", Revision: 1, Decision: bus.DecisionApprove, Text: "x"})
	wantCode(t, err, bus.CodeNotFound)
	_, err = e.b.RecordAnswer(ctx, duet, bus.AnswerInput{ItemID: "itm_nope", Revision: 1, Text: "x"})
	wantCode(t, err, bus.CodeNotFound)
}

func TestNodeCannotRecordAnswerOrApproval(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	crew := node("ci-1", "crew")
	for _, id := range []string{q.ID, a.ID} {
		s := e.snap(t, id)
		_, err := e.b.RecordApproval(ctx, crew, bus.ApprovalInput{ItemID: id, Revision: 1, Decision: bus.DecisionApprove, Text: "go"})
		be := errWithCode(t, err, bus.CodeForbidden)
		if !strings.Contains(be.Fix, "passkey") {
			t.Errorf("approval Fix = %q, want the passkey instruction", be.Fix)
		}
		_, err = e.b.RecordAnswer(ctx, crew, bus.AnswerInput{ItemID: id, Revision: 1, Text: "go"})
		be = errWithCode(t, err, bus.CodeForbidden)
		if !strings.Contains(be.Fix, "relay") {
			t.Errorf("answer Fix = %q, want the relay instruction", be.Fix)
		}
		e.unchanged(t, "node decide", s)
		if got, _ := e.stored(t, id); got.State != bus.StateOpen {
			t.Errorf("state = %s, want open", got.State)
		}
	}
}

// Only a passkey approves and only Duet or the desk answers: every other credential, and a
// passkey without a name, is refused before the item is read.
func TestOnlyTheRightCredentialsDecide(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	s, sa := e.snap(t, q.ID), e.snap(t, a.ID)
	others := []bus.Principal{
		{Kind: bus.PrincipalExecutor, Name: "exec"}, {Kind: bus.PrincipalDeskStart, Name: "start"}, {Name: "x"},
		deskP, duet, node("ci-1", "crew"), {Kind: bus.PrincipalPasskey},
	}
	for _, p := range others {
		_, err := e.b.RecordApproval(ctx, p, approveIn(a))
		wantCode(t, err, bus.CodeForbidden)
	}
	for _, p := range []bus.Principal{passkey, {Kind: bus.PrincipalExecutor, Name: "exec"}, {Kind: bus.PrincipalDeskStart, Name: "start"}, {Name: "x"}} {
		_, err := e.b.RecordAnswer(ctx, p, answerIn(q))
		wantCode(t, err, bus.CodeForbidden)
	}
	e.unchanged(t, "question", s)
	e.unchanged(t, "approval", sa)
}

func TestWalledAnswerOnlyGoOrHold(t *testing.T) {
	e := newEnvWalled(t, "client")
	d := typed("12")
	d.Kind = bus.KindQuestion
	it := e.post(t, walledNode, d).Item
	s := e.snap(t, it.ID)
	for _, text := range []string{"yes, go ahead", "go", "go sl:", "go sl:12 now", "Go sl:12", "stop sl:12", "go sl:12\nhold sl:13", " go sl:12"} {
		in := answerIn(it)
		in.Text = text
		_, err := e.b.RecordAnswer(ctx, duet, in)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "text" {
			t.Errorf("answer %q: What = %q", text, be.What)
		}
	}
	e.unchanged(t, "walled prose", s)
	in := answerIn(it)
	in.Text = "go sl:12"
	r, err := e.b.RecordAnswer(ctx, duet, in)
	if err != nil || r.Text != "go sl:12" || r.ActorKind != bus.ActorAgent {
		t.Errorf("go sl:12 = %+v, %v", r, err)
	}
}

func TestDuetCannotApprove(t *testing.T) {
	e := newEnv(t)
	a, q := e.approvalItem(t), e.questionItem(t)
	s := e.snap(t, a.ID)
	_, err := e.b.RecordApproval(ctx, duet, approveIn(a))
	be := errWithCode(t, err, bus.CodeForbidden)
	if !strings.Contains(be.Fix, "approval page") || !strings.Contains(be.Fix, "passkey") {
		t.Errorf("Fix = %q", be.Fix)
	}
	e.unchanged(t, "duet approval", s)
	if got, _ := e.stored(t, a.ID); got.State != bus.StateOpen {
		t.Errorf("state = %s", got.State)
	}
	r, err := e.b.RecordAnswer(ctx, duet, answerIn(q))
	if err != nil || r.ActorKind != bus.ActorAgent {
		t.Fatalf("duet answer = %+v, %v", r, err)
	}
	if got, _ := e.stored(t, q.ID); got.State != bus.StateDecided {
		t.Errorf("question state = %s, want decided", got.State)
	}
}

func TestActorKindComesOnlyFromCredential(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[bus.AnswerInput](), reflect.TypeFor[bus.ApprovalInput](), reflect.TypeFor[bus.RelayInput]()} {
		for _, f := range reflect.VisibleFields(typ) {
			if strings.Contains(f.Name, "Actor") || f.Name == "By" || f.Name == "Kind" {
				t.Errorf("%s has a %s field: a caller could set it", typ.Name(), f.Name)
			}
		}
	}
}

func TestActorKindFollowsCredential(t *testing.T) {
	e := newEnv(t)
	a, q := e.approvalItem(t), e.questionItem(t)
	r, err := e.b.RecordApproval(ctx, passkey, approveIn(a))
	if err != nil || r.ActorKind != bus.ActorHuman || r.By != "phone" {
		t.Errorf("passkey approval = %+v, %v", r, err)
	}
	r, err = e.b.RecordAnswer(ctx, duet, answerIn(q))
	if err != nil || r.ActorKind != bus.ActorAgent || r.By != "duet" {
		t.Errorf("duet answer = %+v, %v", r, err)
	}
	r, err = e.b.RecordAnswer(ctx, deskP, answerIn(q))
	if err != nil || r.ActorKind != bus.ActorHuman || r.By != "desk" {
		t.Errorf("desk answer = %+v, %v", r, err)
	}
}

// An approval inserted straight through the Tx with an agent fails the database check.
func TestAgentApprovalFailsDatabaseCheck(t *testing.T) {
	e := newEnv(t)
	again := e.post(t, node("ci-1", "crew"), func() bus.Draft { d := mergeApproval("m8"); d.Title = "Merge PR 8"; return d }()).Item
	s := e.snap(t, again.ID)
	rec := bus.Record{ItemID: again.ID, Revision: 1, Kind: bus.RecordApproval, Decision: bus.DecisionApprove, ActorKind: bus.ActorAgent,
		Text: "x", Source: "duet", By: "duet", HumanTS: t0, Recorded: t0}
	if err := e.d.Tx(ctx, func(x bus.Tx) error { return x.InsertDecision(rec) }); err == nil {
		t.Error("the database took an agent approval")
	}
	e.unchanged(t, "agent approval through the Tx", s)
}

func TestSourceComesFromCredential(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	in := answerIn(q)
	in.Source, in.SourceMessage = "passkey:forged", "msg-9"
	r, err := e.b.RecordAnswer(ctx, duet, in)
	same(t, "duet answer", []any{err, r.Source, r.SourceMessage, r.HumanTS}, []any{nil, "duet", "msg-9", t0})
	in.Text = "use 9090"
	r, err = e.b.RecordAnswer(ctx, deskP, in)
	same(t, "desk answer", []any{err, r.Source}, []any{nil, "desk"})
	ap := approveIn(a)
	ap.Source = "desk"
	r, err = e.b.RecordApproval(ctx, bus.Principal{Kind: bus.PrincipalPasskey, Name: "tablet"}, ap)
	same(t, "passkey approval", []any{err, r.Source, r.Kind, r.Decision}, []any{nil, "passkey:tablet", bus.RecordApproval, bus.DecisionApprove})
}

func TestDecisionInputRules(t *testing.T) {
	e := newEnv(t)
	q, a := e.questionItem(t), e.approvalItem(t)
	s, sa := e.snap(t, q.ID), e.snap(t, a.ID)
	for _, text := range []string{"", "  \n\t ", strings.Repeat("a", bus.MaxAnswer+1), "bad \xff utf8", "nul\x00byte", "bidi \u202e text"} {
		in := answerIn(q)
		in.Text = text
		_, err := e.b.RecordAnswer(ctx, duet, in)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "text" {
			t.Errorf("answer text %q: What = %q", text, be.What)
		}
		ap := approveIn(a)
		ap.Text = text
		_, err = e.b.RecordApproval(ctx, passkey, ap)
		wantCode(t, err, bus.CodeInvalid)
	}
	for _, dec := range []bus.Decision{"", "maybe", "APPROVE"} {
		ap := approveIn(a)
		ap.Decision = dec
		_, err := e.b.RecordApproval(ctx, passkey, ap)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "decision" {
			t.Errorf("decision %q: What = %q", dec, be.What)
		}
	}
	e.unchanged(t, "question", s)
	e.unchanged(t, "approval", sa)
	ap := approveIn(a)
	ap.Decision = bus.DecisionReject
	if r, err := e.b.RecordApproval(ctx, passkey, ap); err != nil || r.Decision != bus.DecisionReject {
		t.Errorf("reject = %+v, %v", r, err)
	}
}

func reflectEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

// A question that carries a passkey-able action (the bus never posts one, so it is stored
// directly) is still not an approval: the kind check, not the policy, refuses it.
func TestApprovalOnQuestionWithActionIsNotApprovable(t *testing.T) {
	e := newEnv(t)
	d := askQuestion("qm")
	d.Action, d.Repo, d.Ref, d.BaseRef, d.Target = bus.ActionMerge, "o/r", "7", "main", "o/r#7"
	it := bus.Item{Draft: d, ID: "itm_rawq", Swarm: "crew", From: "ci-1", FromKind: bus.PrincipalNode, Revision: 1,
		State: bus.StateOpen, ContentHash: "v1:x", Created: t0, Updated: t0}
	if err := e.d.Tx(ctx, func(x bus.Tx) error { return x.InsertItem(it) }); err != nil {
		t.Fatal(err)
	}
	s := e.snap(t, it.ID)
	_, err := e.b.RecordApproval(ctx, passkey, approveIn(it))
	wantCode(t, err, bus.CodeNotApprovable)
	e.unchanged(t, "approval on a question", s)
}
