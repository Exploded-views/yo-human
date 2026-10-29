package bus_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"slices"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestPostCreatesWithCredentialSwarm(t *testing.T) {
	e := newEnv(t)
	r := e.post(t, node("ci-1", "crew"), notice("h1"))
	it := r.Item
	same(t, "outcome, swarm, from, kind, revision, state", []any{r.Outcome, it.Swarm, it.From, it.FromKind, it.Revision, it.State},
		[]any{bus.OutcomeCreated, "crew", "ci-1", bus.PrincipalNode, 1, bus.StateOpen})
	if it.ID == "" {
		t.Error("no id")
	}
	evs := e.events(t)
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	same(t, "event", []any{evs[0].Type, evs[0].Swarm, evs[0].ItemID}, []any{bus.EventItemPosted, "crew", it.ID})
	got, found := e.stored(t, it.ID)
	same(t, "stored", []any{found, got.LastSeq, it.LastSeq, got.ContentHash}, []any{true, evs[0].Seq, evs[0].Seq, it.ContentHash})
}

func TestFromComesFromCredential(t *testing.T) {
	for _, f := range reflect.VisibleFields(reflect.TypeFor[bus.Draft]()) {
		if f.Name == "From" || f.Name == "FromKind" || f.Name == "Swarm" {
			t.Errorf("Draft has a %s field: a client could set it", f.Name)
		}
	}
	e := newEnv(t)
	d := notice("h1")
	d.To = "crew"
	it := e.post(t, duet, d).Item
	if it.FromKind != bus.PrincipalDuet || it.From != "duet" || it.Swarm != "crew" {
		t.Errorf("duet item to crew = %q/%q in %q", it.From, it.FromKind, it.Swarm)
	}
	d.To, d.Handle = "human", "h2"
	if it := e.post(t, duet, d).Item; it.Swarm != "duet" || it.FromKind != bus.PrincipalDuet {
		t.Errorf("duet item to human went to swarm %q", it.Swarm)
	}
	d.To, d.Handle = "Not A Name", "h3"
	_, err := e.b.Post(context.Background(), duet, d)
	wantCode(t, err, bus.CodeInvalid)
}

func TestNodeCannotTargetAnotherSwarm(t *testing.T) {
	e := newEnv(t)
	d := notice("h1")
	d.To = "client"
	it := e.post(t, node("ci-1", "crew"), d).Item
	if it.Swarm != "crew" {
		t.Fatalf("item went to swarm %q, want the credential's", it.Swarm)
	}
	_, err := e.b.Reply(context.Background(), node("ci-2", "client"), it.ID, "hi", bus.SeverityNormal)
	wantCode(t, err, bus.CodeNotFound)
}

func TestRepostSameContentUnchanged(t *testing.T) {
	e := newEnv(t)
	first := e.post(t, node("ci-1", "crew"), notice("h1"))
	again := e.post(t, node("ci-1", "crew"), notice("h1"))
	if again.Outcome != bus.OutcomeUnchanged || again.Item.ID != first.Item.ID || again.Item.Revision != 1 {
		t.Fatalf("repost = %+v", again)
	}
	if n := len(e.events(t)); n != 1 {
		t.Errorf("%d events after an unchanged repost, want 1", n)
	}
	got, _ := e.stored(t, first.Item.ID)
	if !got.Updated.Equal(first.Item.Updated) || got.LastSeq != first.Item.LastSeq {
		t.Errorf("an unchanged repost touched the item: %+v", got)
	}
}

func TestRepostEmptyAndNilFlagsAreTheSameContent(t *testing.T) {
	e := newEnv(t)
	d := notice("h1")
	e.post(t, node("ci-1", "crew"), d)
	d.RiskFlags = []string{}
	if r := e.post(t, node("ci-1", "crew"), d); r.Outcome != bus.OutcomeUnchanged {
		t.Errorf("empty flags vs nil gave %s", r.Outcome)
	}
}

func approveRevision(t *testing.T, e *env, id string, rev int) {
	t.Helper()
	r := bus.Record{ItemID: id, Revision: rev, Kind: bus.RecordApproval, Decision: bus.DecisionApprove,
		ActorKind: bus.ActorHuman, Text: "yes", Source: "passkey:phone", By: "owner", HumanTS: t0, Recorded: t0}
	if err := e.d.Tx(context.Background(), func(x bus.Tx) error { return x.InsertDecision(r) }); err != nil {
		t.Fatal(err)
	}
}

func TestRepostChangedBumpsRevisionAndVoids(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	first := e.post(t, p, mergeApproval("merge-7")).Item
	approveRevision(t, e, first.ID, 1)
	if got, _ := e.stored(t, first.ID); got.State != bus.StateDecided {
		t.Fatalf("state before repost = %s, want decided", got.State)
	}
	d := mergeApproval("merge-7")
	d.Title = "Merge PR 7 (rebased)"
	r := e.post(t, p, d)
	same(t, "revised", []any{r.Outcome, r.Item.Revision, r.Item.State, r.Item.Title},
		[]any{bus.OutcomeRevised, 2, bus.StateOpen, d.Title})
	evs := e.events(t)
	want := []bus.EventType{bus.EventItemPosted, bus.EventItemRevised, bus.EventApprovalVoided}
	if !slices.Equal(types(evs), want) {
		t.Fatalf("events = %v, want %v", types(evs), want)
	}
	got, _ := e.stored(t, first.ID)
	same(t, "stored after revise", []any{got.Revision, got.State, got.LastSeq, got.Title, got.From, got.Created},
		[]any{2, bus.StateOpen, evs[2].Seq, d.Title, "ci-1", first.Created})
	if got.ContentHash == first.ContentHash {
		t.Error("the content hash did not change")
	}
}

func TestReviseWithoutApprovalDoesNotVoid(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	e.post(t, p, notice("h1"))
	d := notice("h1")
	d.Title = "Build done again"
	e.post(t, p, d)
	if got := types(e.events(t)); !slices.Equal(got, []bus.EventType{bus.EventItemPosted, bus.EventItemRevised}) {
		t.Errorf("events = %v", got)
	}
}

func TestReviseOfAnsweredQuestionDoesNotVoid(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	q := notice("q1")
	q.Kind = bus.KindQuestion
	it := e.post(t, p, q).Item
	ans := bus.Record{ItemID: it.ID, Revision: 1, Kind: bus.RecordAnswer, ActorKind: bus.ActorAgent,
		Text: "port 80", Source: "duet", By: "duet", HumanTS: t0, Recorded: t0}
	if err := e.d.Tx(context.Background(), func(x bus.Tx) error { return x.InsertDecision(ans) }); err != nil {
		t.Fatal(err)
	}
	q.Title = "Which port, really?"
	r := e.post(t, p, q)
	same(t, "revised", []any{r.Outcome, r.Item.State}, []any{bus.OutcomeRevised, bus.StateOpen})
	if got := types(e.events(t)); !slices.Equal(got, []bus.EventType{bus.EventItemPosted, bus.EventItemRevised}) {
		t.Errorf("events = %v: an answer is not an approval, nothing to void", got)
	}
}

func TestReviseChangingKindBumpsRevision(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	e.post(t, p, notice("h1"))
	d := notice("h1")
	d.Kind = bus.KindQuestion
	r := e.post(t, p, d)
	if r.Outcome != bus.OutcomeRevised || r.Item.Kind != bus.KindQuestion || r.Item.Revision != 2 {
		t.Fatalf("kind change = %+v", r)
	}
}

func TestRepostByAnotherAuthorRefused(t *testing.T) {
	e := newEnv(t)
	first := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	d := notice("h1")
	d.Title = "Rewritten by someone else"
	for _, p := range []bus.Principal{node("ci-2", "crew"), duet} {
		d.To = "human"
		if p.Kind == bus.PrincipalDuet {
			d.To = "crew"
		}
		_, err := e.b.Post(context.Background(), p, d)
		wantCode(t, err, bus.CodeForbidden)
	}
	if got, _ := e.stored(t, first.ID); got.Title != first.Title || got.Revision != 1 {
		t.Errorf("item changed: %+v", got)
	}
}

func TestHandleScopedPerSwarm(t *testing.T) {
	e := newEnv(t)
	a := e.post(t, node("ci-1", "crew"), notice("same"))
	b := e.post(t, node("ci-9", "client"), notice("same"))
	if a.Outcome != bus.OutcomeCreated || b.Outcome != bus.OutcomeCreated || a.Item.ID == b.Item.ID ||
		a.Item.Swarm != "crew" || b.Item.Swarm != "client" {
		t.Fatalf("a = %+v, b = %+v", a, b)
	}
}

func TestPostWithoutHandleAlwaysCreates(t *testing.T) {
	e := newEnv(t)
	q := bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human", Title: "Which port?", Action: bus.ActionNone}
	a, b := e.post(t, node("ci-1", "crew"), q), e.post(t, node("ci-1", "crew"), q)
	if a.Outcome != bus.OutcomeCreated || b.Outcome != bus.OutcomeCreated || a.Item.ID == b.Item.ID || a.Item.Handle == b.Item.Handle {
		t.Fatalf("a = %+v, b = %+v", a.Item, b.Item)
	}
}

func TestPostRefusesNonPostingPrincipals(t *testing.T) {
	e := newEnv(t)
	for _, p := range []bus.Principal{
		{Kind: bus.PrincipalDesk, Name: "desk"}, {Kind: bus.PrincipalPasskey, Name: "phone"},
		{Kind: bus.PrincipalExecutor, Name: "exec"}, {Kind: bus.PrincipalDeskStart, Name: "start"}, {},
		{Kind: bus.PrincipalNode, Name: "ci-1"}, // a node with no swarm
	} {
		_, err := e.b.Post(context.Background(), p, notice("h1"))
		wantCode(t, err, bus.CodeForbidden)
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused posts", n)
	}
}

func TestPostValidatesBeforeStoring(t *testing.T) {
	e := newEnv(t)
	d := notice("h1")
	d.Severity = "urgent"
	_, err := e.b.Post(context.Background(), node("ci-1", "crew"), d)
	wantCode(t, err, bus.CodeInvalid)
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after an invalid post", n)
	}
}

func TestPostRefusesExcludedActions(t *testing.T) {
	e := newEnv(t)
	for _, a := range []bus.Action{bus.ActionSecrets, bus.ActionMoney} {
		d := mergeApproval("h-" + string(a))
		d.Action, d.Repo, d.Ref, d.BaseRef, d.PinnedCommit = a, "", "", "", ""
		_, err := e.b.Post(context.Background(), node("ci-1", "crew"), d)
		wantCode(t, err, bus.CodeExcludedAction)
		// Outright: not only when the item is an approval the policy would refuse.
		q := notice("q-" + string(a))
		q.Action = a
		_, err = e.b.Post(context.Background(), duet, q)
		wantCode(t, err, bus.CodeExcludedAction)
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after excluded posts", n)
	}
}

func TestPostChecksApprovability(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	d := mergeApproval("h1")
	d.Repo = "o/unlisted"
	_, err := e.b.Post(context.Background(), p, d)
	wantCode(t, err, bus.CodeExcludedAction)
	d = mergeApproval("h2")
	d.Action, d.Repo, d.Ref, d.BaseRef, d.PinnedCommit = bus.ActionNone, "", "", "", ""
	_, err = e.b.Post(context.Background(), p, d)
	wantCode(t, err, bus.CodeNotApprovable)
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused approvals", n)
	}
	e.post(t, p, mergeApproval("h3"))
}

func TestContentHashIsSHA256OfCanonicalJSON(t *testing.T) {
	e := newEnv(t)
	d := notice("h")
	d.Title = "T"
	got := e.post(t, node("ci-1", "crew"), d).Item.ContentHash
	const canon = `{"Kind":"notice","Severity":"normal","To":"human","Handle":"h","Title":"T","Summary":"","Detail":"",` +
		`"RequiredAction":"","Repo":"","Ref":"","BaseRef":"","Target":"","PinnedCommit":"","PinnedTree":"",` +
		`"Action":"none","Link":"","RiskFlags":[],"Untrusted":false}`
	sum := sha256.Sum256([]byte(canon))
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("hash = %s, want %s", got, want)
	}
	d.Untrusted = true
	if other := e.post(t, node("ci-1", "crew"), d).Item.ContentHash; other == got {
		t.Error("changing Untrusted left the hash unchanged")
	}
}
