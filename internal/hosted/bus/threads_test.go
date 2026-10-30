package bus_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestCrossSwarmIsIndistinguishableFromMissing(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	before := len(e.events(t))
	other := node("ci-9", "client")
	_, errCross := e.b.Reply(context.Background(), other, it.ID, "hi", bus.SeverityNormal, bus.UrgencyDigest)
	_, errMissing := e.b.Reply(context.Background(), other, bus.NewID("itm"), "hi", bus.SeverityNormal, bus.UrgencyDigest)
	a, b := errWithCode(t, errCross, bus.CodeNotFound), errWithCode(t, errMissing, bus.CodeNotFound)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("cross-swarm error %+v differs from missing %+v", a, b)
	}
	if strings.Contains(a.What+a.Valid+a.Fix, it.ID) {
		t.Error("the error names the id")
	}
	if len(e.messages(t, it.ID)) != 0 || len(e.events(t)) != before {
		t.Error("a refused reply stored something")
	}
}

func TestReplyAppendsMessageAndEvent(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	m, err := e.b.Reply(context.Background(), node("ci-2", "crew"), it.ID, "on it", bus.SeverityHigh, bus.UrgencyDigest)
	if err != nil {
		t.Fatal(err)
	}
	same(t, "message", []any{m.ThreadID, m.From, m.FromKind, m.Body, m.Severity, m.ID != "", m.Created.IsZero()},
		[]any{it.ID, "ci-2", bus.PrincipalNode, "on it", bus.SeverityHigh, true, false})
	m2, err := e.b.Reply(context.Background(), duet, it.ID, "thanks", bus.SeverityNormal, bus.UrgencyDigest)
	if err != nil {
		t.Fatal(err)
	}
	same(t, "duet reply", []any{m2.FromKind, m2.From}, []any{bus.PrincipalDuet, "duet"})
	got := e.messages(t, it.ID)
	same(t, "thread", len(got), 2)
	if len(got) == 2 {
		same(t, "thread order", []string{got[0].ID, got[1].ID}, []string{m.ID, m2.ID})
	}
	evs := e.events(t)
	same(t, "events", types(evs), []bus.EventType{bus.EventItemPosted, bus.EventMessageAdded, bus.EventMessageAdded})
	if len(evs) != 3 {
		t.FailNow()
	}
	same(t, "message event", []string{evs[1].Swarm, evs[1].ItemID}, []string{"crew", it.ID})
	st, _ := e.stored(t, it.ID)
	same(t, "item after replies", []any{st.LastSeq, st.Revision, st.State}, []any{evs[2].Seq, 1, bus.StateOpen})
}

func TestReplyValidatesAndChecksWhoMayReply(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	n := node("ci-2", "crew")
	for name, c := range map[string]struct {
		body string
		sev  bus.Severity
	}{
		"empty": {"", bus.SeverityNormal}, "blank": {" \n\t ", bus.SeverityNormal},
		"too big":      {strings.Repeat("a", bus.MaxBody+1), bus.SeverityNormal},
		"bad severity": {"hi", "urgent"}, "bidi": {"a" + string(rune(0x202e)) + "b", bus.SeverityNormal},
	} {
		_, err := e.b.Reply(context.Background(), n, it.ID, c.body, c.sev, bus.UrgencyDigest)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What == "" {
			t.Errorf("%s: no What", name)
		}
	}
	for _, p := range []bus.Principal{
		{Kind: bus.PrincipalDesk, Name: "desk"}, {Kind: bus.PrincipalPasskey, Name: "phone"},
		{Kind: bus.PrincipalExecutor, Name: "exec"}, {Kind: bus.PrincipalDeskStart, Name: "start"}, {},
	} {
		_, err := e.b.Reply(context.Background(), p, it.ID, "hi", bus.SeverityNormal, bus.UrgencyDigest)
		wantCode(t, err, bus.CodeForbidden)
	}
	if len(e.messages(t, it.ID)) != 0 {
		t.Error("a refused reply stored a message")
	}
}
